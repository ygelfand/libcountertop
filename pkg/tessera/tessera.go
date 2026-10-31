package tessera

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/hook"
	"github.com/ygelfand/libcountertop/pkg/runtime/component"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	Firmware = "0.55.0"
	protocol = 2
	ready    = "Ready for tile configuration"
)

type Facing int

const (
	Dashboard Facing = iota
	Landscape
	Portrait
)

func (f Facing) word() string {
	switch f {
	case Landscape:
		return "landscape"
	case Portrait:
		return "portrait"
	}
	return "dashboard"
}

type Options struct {
	Facing   Facing
	Device   uint32
	Board    func() string
	Size     func() (w, h int)
	Grid     func() schema.TesseraGrid
	Node     func() string
	Name     func() string
	Speaking func() bool
	Keep     string
	Alert    func(Alert)
	Unalert  func()
	Front    func()
}

type Screen struct {
	Changed hook.Hook[struct{}]
	Told    hook.Hook[component.Event]

	options Options
	inbox   *esphome.Text
	shape   *esphome.TextSensor
	fixed   []*esphome.TextSensor

	mu       sync.Mutex
	session  session
	layout   Layout
	memory   bool
	restored bool
	heard    bool
	dirty    bool
	fed      time.Time
	asked    question
	history  *History
	names    *Names
	wanted   PageAsk
	pictures map[string]*picture
}

func New(o Options) *Screen {
	w := o.Facing.word()
	s := &Screen{
		options: o,
		inbox: &esphome.Text{
			Base: esphome.Base{
				ObjectID: "tessera_" + w + "_tile_settings",
				Name:     "Tile settings",
				Icon:     "mdi:view-grid",
				DeviceID: o.Device,
			},
			MaxLength: 255,
			Mode:      esphome.TextPlain,
		},
		shape: diagnostic("tessera_"+w+"_screen_layout", "Screen layout", o.Device),
	}
	s.inbox.OnCommand = func(string) { s.inbox.Set("Error: incomplete message") }
	s.inbox.Set(ready)
	for _, f := range []struct {
		id, name string
		value    func() string
	}{
		{"screen_firmware", "Screen firmware", func() string { return Firmware }},
		{"screen_board", "Screen board", o.Board},
		{"device_name", "Device name", s.node},
		{"screen_language", "Screen language", say.Chosen},
		{"screen_features", "Screen features", func() string { return "dimmable standby" }},
		{"screen_flash", "Screen flash", func() string { return "other" }},
	} {
		t := diagnostic("tessera_"+w+"_"+f.id, f.name, o.Device)
		t.Set(f.value())
		s.fixed = append(s.fixed, t)
	}
	s.Regrid()
	s.restore()
	return s
}

func (s *Screen) node() string { return s.options.Node() + "-" + s.options.Facing.word() }

func (s *Screen) Facing() Facing { return s.options.Facing }

func (s *Screen) Enabled() bool { return s.options.Speaking() }

func (s *Screen) size() (w, h int) {
	w, h = s.options.Size()
	switch s.options.Facing {
	case Landscape:
		if h > w {
			w, h = h, w
		}
	case Portrait:
		if w > h {
			w, h = h, w
		}
	}
	return w, h
}

func diagnostic(id, name string, device uint32) *esphome.TextSensor {
	return &esphome.TextSensor{Base: esphome.Base{
		ObjectID: id,
		Name:     name,
		Category: esphome.CategoryDiagnostic,
		DeviceID: device,
	}}
}

func (s *Screen) Name() string { return "tessera " + s.options.Facing.word() }

func (s *Screen) Regrid() {
	w, h := s.size()
	if w <= 0 || h <= 0 {
		return
	}
	g := s.grid(w, h)
	shape := fmt.Sprintf("%dx%d %dx%d", w, h, g.Columns, g.Rows)
	if shape == s.shape.Get() {
		return
	}
	s.shape.Set(shape)
	slog.Info("tessera shape", "layout", shape)
}

func (s *Screen) Entities() []esphome.Entity {
	if !s.options.Speaking() {
		return nil
	}
	out := []esphome.Entity{s.inbox, s.shape}
	for _, t := range s.fixed {
		out = append(out, t)
	}
	return out
}

func (s *Screen) Actions() []*esphome.Action {
	if !s.options.Speaking() {
		return nil
	}
	return append([]*esphome.Action{{
		Name:        s.options.Facing.word() + "_screen_message",
		Description: "Tessera's screen protocol: one message from the Tessera integration.",
		Args:        []esphome.Arg{{Name: "message", Type: esphome.ArgString}},
		Answers:     true,
		Run: func(c esphome.Call) (any, error) {
			answer := s.Receive(c.String("message"))
			s.inbox.Set(answer["status"].(string))
			return answer, nil
		},
	}}, s.screenActions()...)
}

func (s *Screen) Layout() (Layout, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.layout.clone(), s.session.active || s.restored
}

func random() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		slog.Error("tessera: no randomness for a session", "err", err)
	}
	return binary.BigEndian.Uint64(b[:])
}

func (s *Screen) grid(w, h int) schema.TesseraGrid {
	g := s.options.Grid()
	if h > w {
		g.Columns, g.Rows = g.Rows, g.Columns
	}
	return g
}

func (s *Screen) Grid() schema.TesseraGrid { return s.grid(s.size()) }

func (s *Screen) Canvas() (w, h int) { return s.size() }

func (s *Screen) restore() {
	if s.options.Keep == "" {
		return
	}
	b, err := os.ReadFile(s.options.Keep)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("tessera: the kept layout could not be read", "err", err)
		}
		return
	}
	var l Layout
	if err := json.Unmarshal(b, &l); err != nil {
		slog.Warn("tessera: the kept layout is unreadable", "err", err)
		return
	}
	s.layout, s.restored = l, true
}

func (s *Screen) keep(l Layout) {
	if s.options.Keep == "" {
		return
	}
	b, err := json.Marshal(l)
	if err == nil {
		err = safe.WriteFile(s.options.Keep, b, 0o600)
	}
	if err != nil {
		slog.Warn("tessera: the layout could not be kept", "err", err)
	}
}

type Stage int

const (
	Unheard Stage = iota
	Empty
	Shown
)

func (s *Screen) Stage() Stage {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.session.active || s.restored:
		return Shown
	case s.heard:
		return Empty
	}
	return Unheard
}

func (s *Screen) Fresh(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	lapse := time.Duration(s.layout.Keepalive)*2*time.Second + time.Minute
	return s.session.active && now.Sub(s.fed) < lapse
}

func (s *Screen) Tell(event string, data map[string]string) {
	s.mu.Lock()
	inbox, rev := s.layout.Inbox, s.layout.Revision
	session := keyText(s.session.lease)
	s.mu.Unlock()
	if inbox == "" {
		return
	}
	fields := map[string]string{"inbox": inbox, "session": session, "rev": rev}
	for k, v := range data {
		fields[k] = v
	}
	s.Told.Emit(component.Event{Name: "esphome." + event, Data: fields})
}

func (s *Screen) Title() string {
	if s.options.Name == nil {
		return ""
	}
	return s.options.Name() + " " + s.options.Facing.word()
}
