package webreceiver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
)

var errNoPage = errors.New("webreceiver: no cast page is open")

func (r *Receiver) Trace(on bool) { r.tracing.Store(on) }

func (r *Receiver) Tracing() bool { return r.tracing.Load() }

func (r *Receiver) trace(msg string, args ...any) {
	if r.tracing.Load() {
		slog.Info(msg, args...)
		return
	}
	slog.Debug(msg, args...)
}

const elementState = `JSON.stringify([...document.querySelectorAll("audio,video")].map(v => ({
	tag: v.tagName, src: (v.currentSrc || "").slice(-60), ready: v.readyState, paused: v.paused,
	ended: v.ended, t: v.currentTime, duration: v.duration, error: v.error && v.error.code,
	network: v.networkState})))`

var snapshotsAfter = []time.Duration{time.Second, 5 * time.Second}

func (r *Receiver) snapshotElements(op string) {
	if !r.tracing.Load() {
		return
	}
	safe.Go("cast trace snapshot", func() {
		began := time.Now()
		for _, after := range snapshotsAfter {
			time.Sleep(after - time.Since(began))
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			state, err := r.Eval(ctx, elementState)
			cancel()
			r.trace("the cast page's media elements", "after", op, "since", after, "state", state,
				"err", err)
		}
	})
}

func (r *Receiver) Eval(ctx context.Context, expr string) (string, error) {
	r.mu.Lock()
	tools := r.tools
	r.mu.Unlock()
	if tools == nil {
		return "", errNoPage
	}
	raw, err := tools.request(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expr,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return "", err
	}
	var out struct {
		Result struct {
			Type        string          `json:"type"`
			Value       json.RawMessage `json:"value"`
			Description string          `json:"description"`
		} `json:"result"`
		Exception *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("webreceiver: devtools: %w", err)
	}
	if e := out.Exception; e != nil {
		return "", fmt.Errorf("the page threw: %s", firstOf(e.Exception.Description, e.Text))
	}
	var text string
	if json.Unmarshal(out.Result.Value, &text) == nil {
		return text, nil
	}
	if len(out.Result.Value) > 0 {
		return string(out.Result.Value), nil
	}
	return firstOf(out.Result.Description, out.Result.Type), nil
}

func (r *Receiver) Describe() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tools == nil {
		return "no cast page is open"
	}
	var b strings.Builder
	line := func(k, v string) { fmt.Fprintf(&b, "%-9s %s\n", k, v) }
	line("app", fmt.Sprintf("%s (%s)", r.app.DisplayName, r.app.AppID))
	line("page", firstOf(r.url, "-"))
	line("state", firstOf(r.state, "-"))
	line("session", fmt.Sprint(r.mediaSession))
	s := r.shown
	line("at", fmt.Sprintf("%s of %s, playing %t, live %t",
		s.At.Round(time.Second), s.Length.Round(time.Second), s.Playing, s.Live))
	if br := s.Break; br != nil {
		line("break", fmt.Sprintf("ad %d of %d, %s of %s",
			br.Clip, br.Clips, br.At.Round(time.Second), br.Length.Round(time.Second)))
	}
	if len(s.Breaks) > 0 {
		line("breaks", fmt.Sprint(s.Breaks))
	}
	line("trace", map[bool]string{true: "on", false: "off"}[r.tracing.Load()])
	return b.String()
}
