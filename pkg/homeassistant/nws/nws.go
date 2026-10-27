package nws

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/fetch"
	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/runtime/remember"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const (
	endpoint = "https://api.weather.gov/alerts/active"
	every    = 5 * time.Minute
	wait     = 20 * time.Second
)

type Severity int

const (
	Unknown Severity = iota
	Minor
	Moderate
	Severe
	Extreme
)

func severity(s string) Severity {
	switch s {
	case "Extreme":
		return Extreme
	case "Severe":
		return Severe
	case "Moderate":
		return Moderate
	case "Minor":
		return Minor
	}
	return Unknown
}

type Alert struct {
	ID          string
	Refers      []string
	Event       string
	Headline    string
	Area        string
	Description string
	Instruction string
	Severity    Severity
	Expires     time.Time
	Outline     [][2]float64
}

func (a Alert) known(ids []string) bool {
	return slices.Contains(ids, a.ID) || slices.ContainsFunc(a.Refers, func(r string) bool {
		return slices.Contains(ids, r)
	})
}

type Options struct {
	Controller *homeassistant.Controller
	Client     *http.Client
	Agent      string
	Settings   storage.Section[schema.Weather]
	Dismissed  remember.Slot[[]string]
	Redraw     func()
}

type NWS struct {
	o Options

	mu      sync.Mutex
	home    [2]float64
	located bool
	alerts  []Alert
	problem error
	loaded  bool
}

func New(o Options) *NWS {
	if o.Client == nil {
		o.Client = fetch.Client(wait)
	}
	return &NWS{o: o}
}

func (n *NWS) Name() string { return "nws" }

func (n *NWS) Run(ctx context.Context) error {
	t := time.NewTimer(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		if n.o.Settings.Read().Alerts {
			n.poll(ctx)
		}
		t.Reset(every)
	}
}

func (n *NWS) Current() (Alert, bool) {
	if !n.o.Settings.Read().Alerts {
		return Alert{}, false
	}
	gone := n.o.Dismissed.Get()
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, a := range n.alerts {
		if a.Severity >= Severe && !a.known(gone) {
			return a, true
		}
	}
	return Alert{}, false
}

func (n *NWS) Dismiss(a Alert) {
	gone := n.o.Dismissed.Get()
	if !slices.Contains(gone, a.ID) {
		gone = append(gone, a.ID)
	}
	if err := n.o.Dismissed.Set(gone); err != nil {
		slog.Warn("saving a dismissed weather alert", "err", err)
	}
	n.redraw()
}

func (n *NWS) Home(ctx context.Context) (float64, float64, error) { return n.locate(ctx) }

func (n *NWS) Load(body io.Reader) error {
	alerts, err := parse(body)
	if err != nil {
		return err
	}
	n.mu.Lock()
	n.loaded, n.problem, n.alerts = true, nil, alerts
	n.mu.Unlock()
	n.redraw()
	return nil
}

func (n *NWS) redraw() {
	if n.o.Redraw != nil {
		n.o.Redraw()
	}
}

func (n *NWS) poll(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	lat, lon, err := n.locate(ctx)
	var alerts []Alert
	if err == nil {
		alerts, err = n.fetch(ctx, lat, lon)
	}
	if err != nil {
		slog.Warn("reading weather alerts", "err", err)
	}
	n.mu.Lock()
	n.loaded, n.problem = true, err
	if err == nil {
		n.alerts = alerts
	}
	n.mu.Unlock()
	if err == nil {
		n.forget(alerts)
	}
	n.redraw()
}

func (n *NWS) forget(alerts []Alert) {
	gone := n.o.Dismissed.Get()
	kept := slices.DeleteFunc(slices.Clone(gone), func(id string) bool {
		return !slices.ContainsFunc(alerts, func(a Alert) bool { return a.known([]string{id}) })
	})
	if len(kept) == len(gone) {
		return
	}
	if err := n.o.Dismissed.Set(kept); err != nil {
		slog.Warn("forgetting expired weather alerts", "err", err)
	}
}

func (n *NWS) locate(ctx context.Context) (float64, float64, error) {
	n.mu.Lock()
	home, located := n.home, n.located
	n.mu.Unlock()
	if located {
		return home[0], home[1], nil
	}
	lat, lon, err := n.o.Controller.Home(ctx)
	if err != nil {
		return 0, 0, err
	}
	n.mu.Lock()
	n.home, n.located = [2]float64{lat, lon}, true
	n.mu.Unlock()
	return lat, lon, nil
}

func (n *NWS) fetch(ctx context.Context, lat, lon float64) ([]Alert, error) {
	url := fmt.Sprintf("%s?point=%.4f,%.4f", endpoint, lat, lon)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", n.o.Agent)
	req.Header.Set("Accept", "application/geo+json")
	resp, err := n.o.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nws: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nws: %s", resp.Status)
	}
	return parse(http.MaxBytesReader(nil, resp.Body, 4<<20))
}

func outline(g *struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}) [][2]float64 {
	if g == nil || g.Type != "Polygon" {
		return nil
	}
	var rings [][][2]float64
	if err := json.Unmarshal(g.Coordinates, &rings); err != nil || len(rings) == 0 {
		return nil
	}
	out := make([][2]float64, 0, len(rings[0]))
	for _, p := range rings[0] {
		out = append(out, [2]float64{p[1], p[0]})
	}
	return out
}

func parse(body io.Reader) ([]Alert, error) {
	var doc struct {
		Features []struct {
			Geometry *struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
			Properties struct {
				ID          string `json:"id"`
				Event       string `json:"event"`
				Headline    string `json:"headline"`
				Area        string `json:"areaDesc"`
				Description string `json:"description"`
				Instruction string `json:"instruction"`
				Severity    string `json:"severity"`
				Status      string `json:"status"`
				Expires     string `json:"expires"`
				Ends        string `json:"ends"`
				References  []struct {
					ID string `json:"identifier"`
				} `json:"references"`
			} `json:"properties"`
		} `json:"features"`
	}
	if err := json.NewDecoder(body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("nws: the alerts: %w", err)
	}
	var out []Alert
	for _, f := range doc.Features {
		p := f.Properties
		if p.Status != "Actual" || p.ID == "" {
			continue
		}
		end := p.Ends
		if end == "" {
			end = p.Expires
		}
		expires, _ := time.Parse(time.RFC3339, end)
		refs := make([]string, 0, len(p.References))
		for _, r := range p.References {
			refs = append(refs, r.ID)
		}
		out = append(out, Alert{
			Refers:      refs,
			ID:          p.ID,
			Event:       p.Event,
			Headline:    p.Headline,
			Area:        p.Area,
			Description: p.Description,
			Instruction: p.Instruction,
			Severity:    severity(p.Severity),
			Expires:     expires,
			Outline:     outline(f.Geometry),
		})
	}
	slices.SortStableFunc(out, func(a, b Alert) int { return cmp.Compare(b.Severity, a.Severity) })
	return out, nil
}
