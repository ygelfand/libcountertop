package homealarmview

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/apps"
	"github.com/ygelfand/libcountertop/pkg/display/navigation"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/homealarm"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const (
	sendWait  = 15 * time.Second
	lingerFor = 3 * time.Minute
	logMost   = 8
	codeMost  = 12
	frame     = 50 * time.Millisecond
)

type Options struct {
	UI       *widgets.UI
	Shell    *shell.Shell
	Alarm    *homealarm.Alarm
	Settings storage.Section[schema.HomeAlarm]
	Ring     func(schema.Chime)
	Chime    func(schema.Chime)
	Quiet    func()
	Wake     func()
	Clock24  func() bool
	Backdrop func(owner shell.View) Backdrop
}

type Backdrop interface {
	Keep(width, height int)
	Close()
}

type App struct{}

func (*App) Covers() bool { return true }

func (*App) Timeout() time.Duration { return lingerFor }

type Popup struct{}

func (*Popup) Covers() bool { return true }

func (*Popup) Wakes() bool { return true }

func (*Popup) Timeout() time.Duration { return lingerFor }

type entry struct {
	at     time.Time
	status homealarm.Status
	by     string
}

type View struct {
	o         Options
	app       *App
	popup     *Popup
	pad       *widgets.Keypad
	animating atomic.Bool
	zonePage  atomic.Int32

	mu        sync.Mutex
	hold      *shell.Hold
	brief     *time.Timer
	note      string
	asking    func(code string)
	log       []entry
	rehearsed *rehearsal
	backdrops map[shell.View]Backdrop
}

func New(o Options) *View {
	v := &View{o: o, app: &App{}, popup: &Popup{}}
	v.pad = &widgets.Keypad{ID: "homealarm", Redraw: o.Shell.Redraw}
	o.Alarm.Changed.Listen(v.moved)
	o.Alarm.Updated.Listen(func(struct{}) { o.Shell.Redraw() })
	o.Shell.Changed.Listen(func(shell.Change) { v.follow() })
	return v
}

func (v *View) App() apps.App {
	return apps.App{
		ID:   schema.AppHomeAlarm,
		Name: func() string { return say.T("homealarm.app") },
		Icon: "mdi:shield-home",
		Open: func() { v.o.Shell.Push(v.app) },
	}
}

func (v *View) Screen(s shell.View) *navigation.Screen {
	switch s {
	case v.app, v.popup:
		return &navigation.Screen{View: s, Clear: true, Fixed: s == v.popup, Build: v.build}
	}
	return nil
}

func (v *View) follow() {
	v.mu.Lock()
	h := v.hold
	gone := h != nil && !h.Held()
	if gone {
		v.hold = nil
	}
	v.mu.Unlock()
	if gone {
		v.clearCode()
	}
}

func (v *View) moved(c homealarm.Change) {
	v.o.Shell.Redraw()
	if c.From.Status == c.To.Status || v.rehearsal() != nil {
		return
	}
	v.remember(c.To)
	v.react(c.From, c.To, true)
}

func (v *View) react(from, to homealarm.State, sound bool) {
	cfg := v.o.Settings.Read()
	ring, chime := v.o.Ring, v.o.Chime
	if !sound {
		ring, chime = func(schema.Chime) {}, func(schema.Chime) {}
	}
	switch s := to.Status; {
	case s.Urgent():
		cue := cfg.Pending
		if s == homealarm.Triggered {
			cue = cfg.Triggered
		}
		v.o.Quiet()
		ring(cue.Sound)
		v.raise(cue.Show, 0)
	case s == homealarm.Arming:
		v.o.Quiet()
		chime(cfg.Arming.Sound)
		v.raise(cfg.Arming.Show, 0)
	case s.Settled() && from.Known():
		v.o.Quiet()
		chime(cfg.Settled.Sound)
		v.raise(cfg.Settled.Show, cfg.SettledFor.After())
	default:
		v.o.Quiet()
		v.lower()
	}
}

func (v *View) remember(s homealarm.State) {
	if !s.Known() {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.log = append(v.log, entry{at: time.Now(), status: s.Status, by: s.ChangedBy})
	if len(v.log) > logMost {
		v.log = slices.Delete(v.log, 0, len(v.log)-logMost)
	}
}

func (v *View) history() []entry {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.Clone(v.log)
}

func (v *View) raise(show bool, hideAfter time.Duration) {
	if !show {
		v.lower()
		return
	}
	if v.o.Wake != nil {
		v.o.Wake()
	}
	v.mu.Lock()
	if v.brief != nil {
		v.brief.Stop()
		v.brief = nil
	}
	held := v.hold != nil
	v.mu.Unlock()
	var h *shell.Hold
	if !held {
		h = v.o.Shell.Hold(v.popup)
	}
	v.mu.Lock()
	if v.hold == nil {
		v.hold = h
	}
	if hideAfter > 0 {
		v.brief = time.AfterFunc(hideAfter, v.lower)
	}
	v.mu.Unlock()
}

func (v *View) lower() {
	v.mu.Lock()
	if v.brief != nil {
		v.brief.Stop()
		v.brief = nil
	}
	h := v.hold
	v.hold = nil
	v.mu.Unlock()
	if h != nil || v.o.Shell.Holding(v.popup) {
		v.o.Shell.Remove(v.popup)
	}
	v.clearCode()
}

func (v *View) close(s shell.View) {
	if s == v.popup {
		v.o.Quiet()
		v.lower()
		return
	}
	v.o.Shell.Remove(s)
}

func (v *View) look() schema.HomeAlarmLook { return v.o.Settings.Read().Look }

func (v *View) arm(to homealarm.Status) func() {
	return func() {
		v.request(true, func(ctx context.Context, code string) error {
			return v.o.Alarm.Arm(ctx, to, code)
		})
	}
}

func (v *View) disarm() {
	v.request(false, v.o.Alarm.Disarm)
}

func (v *View) press(entity string) func() {
	return func() {
		v.send(
			func(ctx context.Context, _ string) error { return v.o.Alarm.Press(ctx, entity) },
			"",
		)
	}
}

func (v *View) toggle(entity string) func() {
	return func() {
		v.send(
			func(ctx context.Context, _ string) error { return v.o.Alarm.Toggle(ctx, entity) },
			"",
		)
	}
}

func (v *View) trigger() {
	v.request(false, v.o.Alarm.Trigger)
}

func (v *View) request(arming bool, send func(context.Context, string) error) {
	code := v.pad.Typed()
	if v.o.Alarm.State().NeedsCode(arming) && code == "" {
		if inline[v.look()] {
			v.tell(say.T("homealarm.code.first"))
			return
		}
		v.mu.Lock()
		v.asking = func(code string) { v.send(send, code) }
		v.note = ""
		v.mu.Unlock()
		v.pad.Reset()
		v.o.Shell.Redraw()
		return
	}
	v.send(send, code)
}

func (v *View) send(send func(context.Context, string) error, code string) {
	if v.rehearsal() != nil {
		v.mu.Lock()
		v.asking = nil
		v.mu.Unlock()
		v.pad.Reset()
		v.tell(say.T("homealarm.rehearsal"))
		return
	}
	v.mu.Lock()
	v.asking = nil
	v.note = say.T("homealarm.sending")
	v.mu.Unlock()
	v.pad.Reset()
	v.o.Shell.Redraw()
	safe.Go("home alarm action", func() {
		ctx, cancel := context.WithTimeout(context.Background(), sendWait)
		defer cancel()
		err := send(ctx, code)
		note := ""
		if err != nil {
			note = refusal(err)
		}
		v.tell(note)
	})
}

func refusal(err error) string {
	var failed *homeassistant.Failed
	if errors.As(err, &failed) && failed.Message != "" {
		return failed.Message
	}
	if errors.Is(err, homealarm.ErrNoEntity) {
		return say.T("homealarm.none")
	}
	return say.T("homealarm.refused")
}

func (v *View) tell(note string) {
	v.mu.Lock()
	v.note = note
	v.mu.Unlock()
	v.o.Shell.Redraw()
}

func (v *View) submit() {
	v.mu.Lock()
	ask := v.asking
	v.mu.Unlock()
	if ask != nil {
		ask(v.pad.Typed())
	}
}

func (v *View) cancel() { v.clearCode() }

func (v *View) clearCode() {
	v.mu.Lock()
	v.asking = nil
	v.mu.Unlock()
	v.pad.Reset()
	v.o.Shell.Redraw()
}

func (v *View) prompt() (string, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.note, v.asking != nil
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
