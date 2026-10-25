package photos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/hook"
	"github.com/ygelfand/libcountertop/pkg/media/googlephotos"
	"github.com/ygelfand/libcountertop/pkg/media/immich"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const (
	tick       = 5 * time.Second
	retryAfter = time.Minute
	deckSize   = 250
	index      = "index.json"
)

const (
	immichName = "immich"
	googleName = "google"
)

type Status int

const (
	Unset Status = iota
	Connecting
	Ready
	BadKey
	TooOld
	Unreachable
	Unknown
	Empty
	Link
)

type State struct {
	Status  Status
	Source  string
	Version string
	Pending int
	Missing string
}

type Picture struct {
	ID     string
	Data   []byte
	Taken  time.Time
	Place  string
	People []string
	Year   int
}

type Options struct {
	Settings storage.Section[schema.Photos]
	Dir      string
	Keep     int
	Now      func() time.Time
	DeviceID uint32
}

type entry struct {
	ID     string   `json:"id"`
	URL    string   `json:"url,omitempty"`
	Taken  string   `json:"taken,omitempty"`
	Place  string   `json:"place,omitempty"`
	People []string `json:"people,omitempty"`
	Year   int      `json:"year,omitempty"`
}

type Library struct {
	Changed hook.Hook[State]

	o      Options
	knobs  *Controls
	status *esphome.TextSensor

	mu      sync.Mutex
	state   State
	applied schema.Photos
	src     source
	deck    []entry
	ready   []entry
	next    int
	failed  time.Time
}

func New(o Options) *Library {
	if o.Keep <= 0 {
		o.Keep = 24
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	l := &Library{o: o}
	l.knobs = &Controls{
		Table:  NewTable(),
		Device: o.DeviceID,
		Read:   o.Settings.Read,
		Save:   func(s Setting, v string) error { return setting.Store(o.Settings, s, v) },
	}
	l.status = &esphome.TextSensor{Base: esphome.Base{
		ObjectID: "photos_status", Name: "Photos status", Icon: "mdi:image-multiple",
		Category: esphome.CategoryDiagnostic, DeviceID: o.DeviceID,
	}}
	l.load()
	return l
}

func (l *Library) Name() string { return "photos" }

func (l *Library) Entities() []esphome.Entity { return append(l.knobs.Entities(), l.status) }

func (l *Library) Settings() *Controls { return l.knobs }

func (l *Library) Restore(c schema.Photos) {
	l.knobs.PublishFrom(c)
	l.status.Set(l.State().Says())
}

func (l *Library) Run(ctx context.Context) error {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		l.step(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

func (l *Library) State() State {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.state
}

func (l *Library) Next() (Picture, bool) {
	l.mu.Lock()
	if len(l.ready) == 0 {
		l.mu.Unlock()
		return Picture{}, false
	}
	e := l.ready[l.next%len(l.ready)]
	l.next++
	l.mu.Unlock()

	data, err := os.ReadFile(l.file(e.ID))
	if err != nil {
		slog.Warn("reading a cached photo failed", "asset", e.ID, "err", err)
		l.forget(e.ID)
		return Picture{}, false
	}
	taken, _ := time.Parse(time.RFC3339, e.Taken)
	return Picture{
		ID:     e.ID,
		Data:   data,
		Taken:  taken,
		Place:  e.Place,
		People: e.People,
		Year:   e.Year,
	}, true
}

func (l *Library) step(ctx context.Context) {
	cfg := l.o.Settings.Read()
	if !configured(cfg) {
		l.reset(cfg)
		l.set(State{Status: Unset})
		return
	}
	if !same(cfg, l.applied) {
		l.reset(cfg)
		l.set(State{Status: Connecting})
	}
	l.mu.Lock()
	src, failed := l.src, l.failed
	l.mu.Unlock()
	if src == nil {
		return
	}
	if !failed.IsZero() && l.o.Now().Sub(failed) < retryAfter {
		return
	}
	if err := src.Connect(ctx); err != nil {
		l.fail(src, err)
		return
	}
	if err := l.refill(ctx, src); err != nil {
		l.fail(src, err)
		return
	}
	if err := l.prefetch(ctx, src); err != nil {
		l.fail(src, err)
		return
	}
	l.mu.Lock()
	st := State{Status: Ready, Source: src.Name(), Version: src.Describe(), Pending: len(l.deck)}
	if len(l.ready) == 0 && len(l.deck) == 0 {
		st.Status = Empty
	}
	l.failed = time.Time{}
	l.mu.Unlock()
	l.set(st)
}

func (l *Library) refill(ctx context.Context, src source) error {
	l.mu.Lock()
	want := 0
	if len(l.deck) < l.o.Keep {
		want = deckSize
	}
	l.mu.Unlock()

	fresh, err := src.Draw(ctx, want)
	if err != nil {
		return err
	}
	if len(fresh) == 0 {
		return nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	seen := make(map[string]bool, len(l.ready)+len(l.deck))
	for _, e := range l.ready {
		seen[e.ID] = true
	}
	for _, e := range l.deck {
		seen[e.ID] = true
	}
	var add []entry
	for _, e := range fresh {
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		add = append(add, e)
	}
	l.deck = append(add, l.deck...)
	return nil
}

func (l *Library) prefetch(ctx context.Context, src source) error {
	for {
		l.mu.Lock()
		if len(l.ready)-l.next >= l.o.Keep || len(l.deck) == 0 {
			l.mu.Unlock()
			return nil
		}
		e := l.deck[0]
		l.mu.Unlock()

		data, err := src.Fetch(ctx, e)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(l.o.Dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(l.file(e.ID), data, 0o644); err != nil {
			return err
		}

		l.mu.Lock()
		l.deck = l.deck[1:]
		l.ready = append(l.ready, e)
		first := len(l.ready) == 1
		l.trim()
		l.mu.Unlock()
		l.save()
		if first {
			l.Changed.Emit(l.State())
		}
	}
}

func (l *Library) trim() {
	most := 2 * l.o.Keep
	if l.next > len(l.ready) {
		l.next = len(l.ready)
	}
	for len(l.ready) > most && l.next > 0 {
		_ = os.Remove(l.file(l.ready[0].ID))
		l.ready = l.ready[1:]
		l.next--
	}
}

func (l *Library) reset(cfg schema.Photos) {
	l.mu.Lock()
	changed := !same(cfg, l.applied)
	had := changed && len(l.ready) > 0 && configured(l.applied)
	l.applied = cfg
	l.src = newSource(cfg, l.o.Now)
	l.deck, l.failed = nil, time.Time{}
	if had {
		for _, e := range l.ready {
			_ = os.Remove(l.file(e.ID))
		}
		l.ready, l.next = nil, 0
	}
	l.mu.Unlock()
	if had {
		l.save()
	}
}

func (l *Library) fail(src source, err error) {
	st, missing := src.Classify(err)
	slog.Warn("photos", "err", err)
	if st == TooOld {
		l.set(State{Status: st, Source: src.Name(), Version: src.Describe()})
		return
	}
	l.mu.Lock()
	l.failed = l.o.Now()
	l.mu.Unlock()
	l.set(State{Status: st, Source: src.Name(), Version: src.Describe(), Missing: missing})
}

func (l *Library) set(st State) {
	l.mu.Lock()
	changed := l.state != st
	l.state = st
	l.mu.Unlock()
	if changed {
		l.status.Set(st.Says())
		l.Changed.Emit(st)
	}
}

func (st State) Says() string {
	google := st.Source == googleName
	switch st.Status {
	case Connecting:
		return say.T("photos.status.connecting")
	case Ready:
		if google {
			return say.T("photos.status.google")
		}
		return say.F("photos.status.ready", map[string]any{"Version": st.Version})
	case BadKey:
		return say.T("photos.status.key")
	case TooOld:
		return say.F("photos.status.old", map[string]any{"Version": st.Version})
	case Unreachable:
		if google {
			return say.T("photos.status.google.unreachable")
		}
		return say.T("photos.status.unreachable")
	case Unknown:
		return say.F("photos.status.unknown", map[string]any{"Name": st.Missing})
	case Empty:
		return say.T("photos.status.empty")
	case Link:
		return say.T("photos.status.google.link")
	}
	return say.T("photos.status.unset")
}

func (l *Library) forget(id string) {
	l.mu.Lock()
	if i := slices.IndexFunc(l.ready, func(e entry) bool { return e.ID == id }); i >= 0 {
		l.ready = slices.Delete(l.ready, i, i+1)
		if i < l.next {
			l.next--
		}
	}
	l.mu.Unlock()
	l.save()
}

func (l *Library) file(id string) string {
	return filepath.Join(l.o.Dir, filepath.Base(id)+".jpg")
}

func (l *Library) load() {
	data, err := os.ReadFile(filepath.Join(l.o.Dir, index))
	if err != nil {
		return
	}
	var saved []entry
	if err := json.Unmarshal(data, &saved); err != nil {
		return
	}
	for _, e := range saved {
		if _, err := os.Stat(l.file(e.ID)); err == nil {
			l.ready = append(l.ready, e)
		}
	}
}

func (l *Library) save() {
	l.mu.Lock()
	data, err := json.Marshal(l.ready)
	l.mu.Unlock()
	if err != nil {
		return
	}
	if err := os.MkdirAll(l.o.Dir, 0o755); err != nil {
		return
	}
	tmp := filepath.Join(l.o.Dir, index+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, filepath.Join(l.o.Dir, index))
}

var errTooOld = errors.New("photos: immich is older than 3.2")

// source is one place pictures come from. Immich and public Google Photos
// albums both plug in here; everything about the deck, the cache and the
// status is shared.
type source interface {
	// Connect resolves the configuration once. It is retried after a failure.
	Connect(ctx context.Context) error
	// Draw returns new entries, most wanted first.
	Draw(ctx context.Context, want int) ([]entry, error)
	// Fetch downloads one picture.
	Fetch(ctx context.Context, e entry) ([]byte, error)
	// Name is the source kind, used to pick status wording.
	Name() string
	// Describe is the connected detail, such as the Immich version.
	Describe() string
	// Classify turns a failure into a status and an optional missing name.
	Classify(err error) (Status, string)
}

func newSource(cfg schema.Photos, now func() time.Time) source {
	if links := googlephotos.SplitLinks(cfg.Google); len(links) > 0 {
		return &googleSource{client: googlephotos.New(), links: links, now: now}
	}
	if cfg.Server == "" {
		return nil
	}
	return &immichSource{client: immich.New(cfg.Server, cfg.Key), cfg: cfg, now: now}
}

func configured(c schema.Photos) bool {
	return c.Server != "" || len(googlephotos.SplitLinks(c.Google)) > 0
}

type immichSource struct {
	client *immich.Client
	cfg    schema.Photos
	now    func() time.Time

	connected bool
	version   string
	sel       immich.Selection
	memories  string
}

func (s *immichSource) Name() string { return immichName }

func (s *immichSource) Describe() string { return s.version }

func (s *immichSource) Connect(ctx context.Context) error {
	if s.connected {
		return nil
	}
	about, err := s.client.About(ctx)
	if err != nil {
		return err
	}
	s.version = about.Version
	if !immich.Supported(about.Version) {
		return errTooOld
	}
	sel, err := resolve(ctx, s.client, s.cfg)
	if err != nil {
		return err
	}
	s.sel, s.connected = sel, true
	return nil
}

func (s *immichSource) Draw(ctx context.Context, want int) ([]entry, error) {
	var out []entry
	today := s.now().Format(time.DateOnly)
	memories := s.cfg.Memories && s.memories != today
	if memories {
		found, err := s.client.Memories(ctx, s.now())
		if err != nil {
			return nil, err
		}
		for _, m := range found {
			for _, a := range m.Assets {
				if a.Type == "" || a.Type == "IMAGE" {
					out = append(out, describe(a, m.Data.Year))
				}
			}
		}
	}
	if want > 0 {
		found, err := s.client.Random(ctx, s.sel, want)
		if err != nil {
			return nil, err
		}
		for _, a := range found {
			out = append(out, describe(a, 0))
		}
	}
	if memories {
		s.memories = today
	}
	return out, nil
}

func (s *immichSource) Fetch(ctx context.Context, e entry) ([]byte, error) {
	return s.client.Preview(ctx, e.ID)
}

func (s *immichSource) Classify(err error) (Status, string) {
	var missing *missingError
	switch {
	case errors.Is(err, errTooOld):
		return TooOld, ""
	case errors.Is(err, immich.ErrKey):
		return BadKey, ""
	case errors.As(err, &missing):
		return Unknown, missing.name
	default:
		return Unreachable, ""
	}
}

type googleSource struct {
	client *googlephotos.Client
	links  []string
	now    func() time.Time

	connected bool
	albums    []googlephotos.Album
	pending   []googlephotos.Photo
}

func (s *googleSource) Name() string { return googleName }

func (s *googleSource) Describe() string { return "" }

func (s *googleSource) Connect(ctx context.Context) error {
	if s.connected {
		return nil
	}
	albums := make([]googlephotos.Album, 0, len(s.links))
	for _, link := range s.links {
		album, err := s.client.Album(ctx, link)
		if err != nil {
			return err
		}
		albums = append(albums, album)
	}
	s.albums, s.connected = albums, true
	return nil
}

func (s *googleSource) Draw(_ context.Context, want int) ([]entry, error) {
	if want <= 0 {
		return nil, nil
	}
	if len(s.pending) == 0 {
		s.pending = s.shuffle()
	}
	n := min(want, len(s.pending))
	out := make([]entry, 0, n)
	for _, p := range s.pending[:n] {
		e := entry{ID: p.ID, URL: p.URL}
		if !p.Taken.IsZero() {
			e.Taken = p.Taken.UTC().Format(time.RFC3339)
			e.Year = p.Taken.Year()
		}
		out = append(out, e)
	}
	s.pending = s.pending[n:]
	return out, nil
}

func (s *googleSource) shuffle() []googlephotos.Photo {
	var all []googlephotos.Photo
	for _, a := range s.albums {
		all = append(all, a.Photos...)
	}
	rand.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
	return all
}

func (s *googleSource) Fetch(ctx context.Context, e entry) ([]byte, error) {
	if e.URL == "" {
		return nil, fmt.Errorf("photos: no link for %s", e.ID)
	}
	return s.client.Picture(ctx, e.URL)
}

func (s *googleSource) Classify(err error) (Status, string) {
	if errors.Is(err, googlephotos.ErrLink) {
		return Link, ""
	}
	return Unreachable, ""
}

type missingError struct{ name string }

func (e *missingError) Error() string { return fmt.Sprintf("photos: nothing named %q", e.name) }

func resolve(ctx context.Context, c *immich.Client, cfg schema.Photos) (immich.Selection, error) {
	sel := immich.Selection{Favorites: cfg.Favorites}
	if names := schema.Names(cfg.Albums); len(names) > 0 {
		all, err := c.Albums(ctx)
		if err != nil {
			return sel, err
		}
		if sel.Albums, err = match(names, all, func(a immich.Album) (string, []string) {
			return a.ID, []string{a.Name}
		}); err != nil {
			return sel, err
		}
	}
	if names := schema.Names(cfg.Tags); len(names) > 0 {
		all, err := c.Tags(ctx)
		if err != nil {
			return sel, err
		}
		if sel.Tags, err = match(names, all, func(t immich.Tag) (string, []string) {
			return t.ID, []string{t.Value, t.Name}
		}); err != nil {
			return sel, err
		}
	}
	if names := schema.Names(cfg.People); len(names) > 0 {
		all, err := c.People(ctx)
		if err != nil {
			return sel, err
		}
		if sel.People, err = match(names, all, func(p immich.Person) (string, []string) {
			return p.ID, []string{p.Name}
		}); err != nil {
			return sel, err
		}
	}
	return sel, nil
}

func match[T any](wants []string, all []T, of func(T) (string, []string)) ([]string, error) {
	var ids []string
	for _, want := range wants {
		found := ""
		for _, item := range all {
			id, names := of(item)
			if id == want || slices.ContainsFunc(names, func(n string) bool {
				return strings.EqualFold(n, want)
			}) {
				found = id
				break
			}
		}
		if found == "" {
			return nil, &missingError{name: want}
		}
		ids = append(ids, found)
	}
	return ids, nil
}

func describe(a immich.Asset, year int) entry {
	e := entry{ID: a.ID, Year: year, Taken: a.Local}
	if a.Exif != nil {
		if a.Exif.Taken != "" {
			e.Taken = a.Exif.Taken
		}
		e.Place = place(a.Exif)
	}
	for _, p := range a.People {
		if p.Name != "" && !p.Hidden {
			e.People = append(e.People, p.Name)
		}
	}
	return e
}

func place(x *immich.Exif) string {
	var parts []string
	for _, s := range []string{x.City, x.Country} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

func same(a, b schema.Photos) bool {
	return a.Server == b.Server && a.Key == b.Key && a.Google == b.Google &&
		a.Albums == b.Albums && a.Tags == b.Tags && a.People == b.People &&
		a.Favorites == b.Favorites && a.Memories == b.Memories
}
