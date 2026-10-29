// Package thermostatview draws up to four Home Assistant thermostats as an app and a dashboard tab, in one of several looks.
package thermostatview

import (
	"context"
	"errors"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/apps"
	"github.com/ygelfand/libcountertop/pkg/display/navigation"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/visual"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/thermostat"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const (
	sendAfter = 700 * time.Millisecond
	sendWait  = 15 * time.Second
	keepFor   = 10 * time.Second
	lingerFor = 3 * time.Minute
	frame     = 50 * time.Millisecond
)

type Options struct {
	UI          *widgets.UI
	Shell       *shell.Shell
	Thermostats *thermostat.Thermostats
	Settings    storage.Section[schema.Thermostat]
	Clock24     func() bool
	Outside     func() (float64, bool)
	Backdrop    func(kind visual.Kind, showing func() bool) Backdrop
	Tabbed      func() bool
}

type Backdrop interface {
	KeepAt(area ui.Rect)
	Close()
}

type App struct{}

func (*App) Covers() bool { return true }

func (*App) Timeout() time.Duration { return lingerFor }

type End int

const (
	Single End = iota
	Low
	High
)

type pending struct {
	target, low, high *float64
	mode              thermostat.Mode
	at                time.Time
	send              *time.Timer
}

type View struct {
	o         Options
	app       *App
	animating atomic.Bool

	mu        sync.Mutex
	pending   map[string]*pending
	ends      map[string]End
	notes     map[string]string
	shuffles  map[string]shuffle
	rehearsed []thermostat.State
	screen    slot
	tab       slot
}

func New(o Options) *View {
	v := &View{
		o:        o,
		app:      &App{},
		pending:  map[string]*pending{},
		ends:     map[string]End{},
		notes:    map[string]string{},
		shuffles: map[string]shuffle{},
	}
	v.screen.showing = func() bool { return o.Shell.Visible(v.app) }
	v.tab.showing = o.Tabbed
	o.Thermostats.Updated.Listen(func(struct{}) { o.Shell.Redraw() })
	o.Shell.Changed.Listen(func(shell.Change) {
		if !o.Shell.Visible(v.app) {
			v.drop(&v.screen)
		}
	})
	return v
}

func (v *View) App() apps.App {
	return apps.App{
		ID:   schema.AppThermostat,
		Name: func() string { return say.T("thermostat.app") },
		Icon: "mdi:thermostat",
		Open: v.Open,
	}
}

func (v *View) Open() { v.o.Shell.Push(v.app) }

func (v *View) Screen(s shell.View) *navigation.Screen {
	if s != v.app {
		return nil
	}
	return &navigation.Screen{View: s, Clear: true, Build: v.build}
}

func (v *View) look() schema.ThermostatLook { return v.o.Settings.Read().Look }

func (v *View) shown() []thermostat.State {
	v.mu.Lock()
	rehearsed := slices.Clone(v.rehearsed)
	v.mu.Unlock()
	list := rehearsed
	if list == nil {
		list = v.o.Thermostats.States()
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	now := time.Now()
	for i, s := range list {
		p, ok := v.pending[s.Entity]
		if !ok {
			continue
		}
		if p.send == nil && now.Sub(p.at) > keepFor {
			delete(v.pending, s.Entity)
			continue
		}
		if p.send == nil && p.settled(s) {
			delete(v.pending, s.Entity)
			continue
		}
		list[i] = p.over(s)
	}
	return list
}

func (p *pending) over(s thermostat.State) thermostat.State {
	if p.target != nil {
		s.Target = p.target
	}
	if p.low != nil {
		s.Low, s.High = p.low, p.high
	}
	if p.mode != "" {
		s.Mode = p.mode
	}
	return s
}

func (p *pending) settled(s thermostat.State) bool {
	same := func(want, got *float64) bool {
		return want == nil || got != nil && math.Abs(*want-*got) < 0.01
	}
	return same(p.target, s.Target) && same(p.low, s.Low) && same(p.high, s.High) &&
		(p.mode == "" || p.mode == s.Mode)
}

func (v *View) find(entity string) (thermostat.State, bool) {
	for _, s := range v.shown() {
		if s.Entity == entity {
			return s, true
		}
	}
	return thermostat.State{}, false
}

func (v *View) end(entity string) End {
	v.mu.Lock()
	defer v.mu.Unlock()
	if e, ok := v.ends[entity]; ok {
		return e
	}
	return Low
}

func (v *View) pick(entity string, e End) {
	v.mu.Lock()
	v.ends[entity] = e
	v.mu.Unlock()
	v.o.Shell.Redraw()
}

func (v *View) nudge(entity string, steps int) {
	s, ok := v.find(entity)
	if !ok || !s.Settable() {
		return
	}
	move := func(at float64) *float64 {
		n := s.Clamp(math.Round((at+float64(steps)*s.Step)/s.Step) * s.Step)
		return &n
	}
	v.mu.Lock()
	p, ok := v.pending[entity]
	if !ok {
		p = &pending{}
		v.pending[entity] = p
	}
	if s.Ranged() {
		low, high := *s.Low, *s.High
		if v.endLocked(entity) == High {
			high = max(*move(high), low+s.Step)
		} else {
			low = min(*move(low), high-s.Step)
		}
		p.low, p.high = &low, &high
	} else {
		p.target = move(*s.Target)
	}
	p.at = time.Now()
	if p.send != nil {
		p.send.Stop()
	}
	p.send = time.AfterFunc(sendAfter, func() { v.flush(entity) })
	delete(v.notes, entity)
	v.mu.Unlock()
	v.o.Shell.Redraw()
}

func (v *View) endLocked(entity string) End {
	if e, ok := v.ends[entity]; ok {
		return e
	}
	return Low
}

func (v *View) flush(entity string) {
	v.mu.Lock()
	p, ok := v.pending[entity]
	if !ok {
		v.mu.Unlock()
		return
	}
	p.send, p.at = nil, time.Now()
	target, low, high := p.target, p.low, p.high
	rehearsing := v.rehearsed != nil
	if rehearsing {
		for i, s := range v.rehearsed {
			if s.Entity == entity {
				v.rehearsed[i] = p.over(s)
			}
		}
		delete(v.pending, entity)
	}
	v.mu.Unlock()
	if rehearsing {
		v.o.Shell.Redraw()
		return
	}
	v.send(entity, func(ctx context.Context) error {
		if low != nil {
			return v.o.Thermostats.SetRange(ctx, entity, *low, *high)
		}
		return v.o.Thermostats.SetTemperature(ctx, entity, *target)
	})
}

func (v *View) setMode(entity string, m thermostat.Mode) {
	v.mu.Lock()
	if v.rehearsed != nil {
		for i, s := range v.rehearsed {
			if s.Entity == entity {
				v.rehearsed[i].Mode = m
				v.rehearsed[i].Action = rehearsedAction(m)
			}
		}
		v.mu.Unlock()
		v.o.Shell.Redraw()
		return
	}
	p, ok := v.pending[entity]
	if !ok {
		p = &pending{}
		v.pending[entity] = p
	}
	p.mode, p.at = m, time.Now()
	delete(v.notes, entity)
	v.mu.Unlock()
	v.o.Shell.Redraw()
	v.send(entity, func(ctx context.Context) error {
		return v.o.Thermostats.SetMode(ctx, entity, m)
	})
}

func (v *View) send(entity string, do func(context.Context) error) {
	safe.Go("thermostat action", func() {
		ctx, cancel := context.WithTimeout(context.Background(), sendWait)
		defer cancel()
		err := do(ctx)
		if err == nil {
			return
		}
		v.mu.Lock()
		delete(v.pending, entity)
		v.notes[entity] = refusal(err)
		v.mu.Unlock()
		v.o.Shell.Redraw()
	})
}

func refusal(err error) string {
	var failed *homeassistant.Failed
	if errors.As(err, &failed) && failed.Message != "" {
		return failed.Message
	}
	return say.T("thermostat.refused")
}

func (v *View) note(entity string) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.notes[entity]
}

func (v *View) again(w *gogui.Window) {
	if v.animating.Swap(true) {
		return
	}
	time.AfterFunc(frame, func() {
		v.animating.Store(false)
		w.QueueCommand(func(w *gogui.Window) { w.InvalidateLayout() })
	})
}

type slot struct {
	shown   Backdrop
	kind    visual.Kind
	showing func() bool
}

func (v *View) keep(s *slot, kind visual.Kind, at ui.Rect) bool {
	if v.o.Backdrop == nil || s.showing == nil {
		return false
	}
	v.mu.Lock()
	old := s.shown
	if old != nil && s.kind != kind {
		s.shown = nil
	} else {
		old = nil
	}
	if s.shown == nil {
		s.shown, s.kind = v.o.Backdrop(kind, s.showing), kind
	}
	b := s.shown
	v.mu.Unlock()
	if old != nil {
		old.Close()
	}
	b.KeepAt(at)
	return true
}

func (v *View) drop(s *slot) {
	v.mu.Lock()
	b := s.shown
	s.shown = nil
	v.mu.Unlock()
	if b != nil {
		b.Close()
	}
}
