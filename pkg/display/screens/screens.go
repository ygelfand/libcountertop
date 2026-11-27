package screens

import (
	"context"
	_ "embed"
	"fmt"
	"image"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/gpu"
	"github.com/ygelfand/libcountertop/pkg/display/photoframe"

	"github.com/ygelfand/libcountertop/pkg/display/weatherview"

	"github.com/ygelfand/libcountertop/pkg/display/timerview"

	"github.com/ygelfand/libcountertop/pkg/display/alarmview"
	"github.com/ygelfand/libcountertop/pkg/display/alertview"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/assistant/timer"
	"github.com/ygelfand/libcountertop/pkg/display/appearance"
	"github.com/ygelfand/libcountertop/pkg/display/assistant"
	"github.com/ygelfand/libcountertop/pkg/display/dashboard"
	"github.com/ygelfand/libcountertop/pkg/display/drawer"
	"github.com/ygelfand/libcountertop/pkg/display/geometry"
	backend "github.com/ygelfand/libcountertop/pkg/display/gogui"
	"github.com/ygelfand/libcountertop/pkg/display/homeview"
	"github.com/ygelfand/libcountertop/pkg/display/message"
	"github.com/ygelfand/libcountertop/pkg/display/navigation"
	"github.com/ygelfand/libcountertop/pkg/display/panel"
	"github.com/ygelfand/libcountertop/pkg/display/playerview"
	"github.com/ygelfand/libcountertop/pkg/display/poster"
	"github.com/ygelfand/libcountertop/pkg/display/ringsview"
	"github.com/ygelfand/libcountertop/pkg/display/screen"
	"github.com/ygelfand/libcountertop/pkg/display/settings"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/surface"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/calendar"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/homecontrol"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/lovelaceview"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/nws"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/todo"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/weather"
	"github.com/ygelfand/libcountertop/pkg/hook"
	"github.com/ygelfand/libcountertop/pkg/input/textentry"
	"github.com/ygelfand/libcountertop/pkg/input/touch"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
	"github.com/ygelfand/libcountertop/pkg/tessera/firmwareview"
	tesseraview "github.com/ygelfand/libcountertop/pkg/tessera/widgets"
)

//go:embed esphome.svg
var esphomeLogo string

const (
	layerID    = 60
	layerZ     = 5
	helperWait = 200 * time.Millisecond
)

type Screen = navigation.Screen

type Watch func(changed func()) (stop func())

func On[T any](h *hook.Hook[T]) Watch {
	return func(changed func()) func() { return h.Listen(func(T) { changed() }) }
}

func All(watches ...Watch) Watch {
	return func(changed func()) func() {
		stops := make([]func(), 0, len(watches))
		for _, w := range watches {
			stops = append(stops, w(changed))
		}
		return func() {
			for _, stop := range stops {
				stop()
			}
		}
	}
}

type Settings struct {
	Screen  schema.Screen
	Clock   schema.Clock
	Idle    schema.Idle
	Weather schema.Weather
	Media   schema.Media
}

type Placer interface {
	Above(ui.Rect, assistant.Showing)
	Below(ui.Rect, assistant.Showing)
	Unstage()
}

type Staging interface {
	Staged() bool
	StagedFull() bool
}

type Stage interface {
	Placer
	Staging
	Look(assistant.Showing) (custom, full bool)
}

type Weather interface {
	Now() (weather.Reading, bool)
}

type Mixer interface {
	Picked() (schema.Stream, bool)
	Level(schema.Stream) int
	Set(schema.Stream, int)
	Muted(schema.Stream) bool
	Mute(schema.Stream, bool)
}

type Popup interface {
	Show(schema.Stream)
	Linger()
	Expand()
	Dismiss()
}

type Volume interface {
	Mixer
	Popup
	Card() shell.View
}

type Marks struct {
	Mic    bool
	Camera bool
}

type Board struct {
	Glyph  func(dashboard.Tab) string
	Draw   func(*gogui.Window, ui.Rect, dashboard.Tab, theme.Theme) gogui.View
	Ground func(dashboard.Tab, ui.Rect) bool
}

type Options struct {
	Name       string
	UISize     string
	DeviceName func() string
	Settings   func() Settings
	UI         *widgets.UI
	Weather    storage.Section[schema.Weather]

	Brand        func() theme.Theme
	Logo         func() image.Image
	Night        func() image.Image
	Offering     func() string
	Input        *textentry.Controller
	KeyboardPair func(string) (string, time.Time, error)

	Display   *panel.Driver
	GPU       *gpu.Renderer
	Touch     *touch.Screen
	Turned    *hook.Hook[geometry.Orientation]
	Shell     *shell.Shell
	Screen    *screen.Screen
	Messages  *message.Messages
	Assistant *assistant.Assistant
	Stage     Stage
	Badge     func(id, name string, size float32) gogui.View
	Timers    *timer.Timers
	// TimerAct calls a Home Assistant action with a timer_id (the Split timer keys).
	TimerAct  func(action, timerID string) error
	Poster    *poster.Poster
	Drawer    *drawer.Rail
	Tabs      *dashboard.Tabs
	Home      *homecontrol.Engine
	HomeHA    homeview.Client
	Adopted   func() bool
	Saver     homeview.SaverMode
	Visuals   homeview.Visuals
	Tessera   *tesseraview.View
	Firmware  *firmwareview.View
	Lovelace  *lovelaceview.Feature
	Forecast  Weather
	Warnings  weatherview.Alerts
	Alarms    alarmview.Alarms
	OpenRadar func(*nws.Alert)

	WeatherEntities  func() shell.View
	Todo             *todo.Todo
	Calendar         *calendar.Calendar
	CamerasPage      func() shell.View
	DoorbellPage     func() shell.View
	HomeAlarmPage    func() shell.View
	ThermostatPage   func() shell.View
	YouTubeTVPage    func() shell.View
	Player           playerview.Player
	PlayerPage       func() shell.View
	BluetoothSources func() shell.View
	Lyrics           playerview.Lyrics
	Volume           Volume
	Streams          []schema.Stream

	Upgrade   func() widgets.Upgrade
	Upgrading Watch
	Marks     func() Marks
	Redraws   []Watch

	Boards map[string]Board
	Views  []func(shell.View) *Screen
}

type App struct {
	o       Options
	ui      *widgets.UI
	nav     *navigation.Nav
	boards  map[string]Board
	preview *widgets.LivePreview
	player  *playerview.Renderer
	home    *homeview.Renderer
	alerts  alertview.View
	timers  timerview.View
	weather weatherview.View
	alarms  alarmview.View
	rings   *ringsview.View

	ticking atomic.Bool
	touched touches

	wallpaper *photoframe.Stage
	papered   atomic.Bool
	papering  bool
	homeDrawn bool

	moves mover

	mu sync.Mutex
	w  *gogui.Window
	r  *backend.Renderer
}

func New(o Options) *App {
	o.UI.Open = o.Shell.Push
	a := &App{
		o:       o,
		ui:      o.UI,
		preview: &widgets.LivePreview{},
	}
	if o.Poster != nil {
		a.wallpaper = photoframe.NewStage(o.Poster.Frame(), photoframe.StageOptions{
			GPU:     o.GPU,
			Display: o.Display,
			Visible: a.papered.Load,
		})
	}
	if o.Home != nil {
		a.rings = ringsview.New(ringsview.Options{UI: o.UI, Home: o.Home, Clock: a.ringsClock})
	}
	a.alerts = alertview.View{UI: o.UI, Messages: o.Messages, Again: a.again}
	a.timers = timerview.View{UI: o.UI, Timers: o.Timers, Again: a.again, Act: o.TimerAct}
	a.alarms = alarmview.View{
		UI:      o.UI,
		Alarms:  o.Alarms,
		Again:   a.again,
		Clock24: func() bool { return o.Settings().Screen.Hours == schema.TwentyFourHour },
	}
	a.weather = weatherview.View{
		UI:      o.UI,
		Again:   a.again,
		Start:   time.Now(),
		Alerts:  o.Warnings,
		Radar:   o.OpenRadar,
		Clock24: func() bool { return o.Settings().Screen.Hours == schema.TwentyFourHour },
	}
	a.boards = map[string]Board{homecontrol.TabKind: {Glyph: homeGlyph, Draw: a.homeBoard}}
	for k, b := range o.Boards {
		a.boards[k] = b
	}
	a.nav = &navigation.Nav{For: a.screenFor}
	a.player = playerview.New(playerview.Dependencies{
		Player:           o.Player,
		Lyrics:           o.Lyrics,
		Shell:            o.Shell,
		UI:               o.UI,
		Dock:             func() schema.Edge { return o.Settings().Screen.Drawer },
		Loud:             func() { o.Volume.Show(schema.StreamMedia) },
		Muted:            func() bool { return o.Volume.Muted(schema.StreamMedia) },
		BluetoothSources: o.BluetoothSources,
		Mini:             func() bool { return o.Settings().Media.Mini },
	})
	a.home = homeview.New(homeview.Dependencies{
		Engine:          o.Home,
		Client:          o.HomeHA,
		Adopted:         o.Adopted,
		Name:            o.DeviceName,
		Weather:         o.Weather,
		WeatherEntities: o.WeatherEntities,
		Todo:            o.Todo,
		Calendar:        o.Calendar,
		Cameras:         o.CamerasPage,
		Doorbell:        o.DoorbellPage,
		HomeAlarm:       o.HomeAlarmPage,
		Thermostat:      o.ThermostatPage,
		YouTubeTV:       o.YouTubeTVPage,
		Shell:           o.Shell,
		Logo:            esphomeLogo,
		Marks:           settings.ModeMarks{Tessera: tesseraview.Mark, Native: a.logo},
		Saver:           o.Saver,
		Standby:         standby(o.Firmware),
		Visuals:         o.Visuals,
		Lovelace:        lovelaceRows(o.Lovelace),
		Reload:          lovelaceReload(o.Lovelace),
		UI:              o.UI,
	})
	if o.Tessera != nil {
		a.home.Listen(o.Tessera)
	}
	return a
}

func (a *App) Name() string { return "gui" }

func (a *App) Gate(w *gogui.Window) gogui.View { return a.home.Gate(w) }

func (a *App) CalendarPage() *shell.Page { return a.home.CalendarPage() }

func (a *App) TodoLists() *shell.Page { return a.home.TodoLists() }

func (a *App) sizeSetting() string {
	if s := a.o.Settings().Screen.Size; s != "" {
		return s
	}
	if a.o.UISize != "" {
		return a.o.UISize
	}
	return appearance.SizeLarge
}

func (a *App) current() gogui.Theme {
	s := a.o.Settings().Screen
	return appearance.Look(s.Style, s.Theme, a.sizeSetting(), s.Font, s.Custom)
}

func (a *App) Restyle() {
	a.mu.Lock()
	w := a.w
	a.mu.Unlock()
	if w != nil {
		w.QueueCommand(func(w *gogui.Window) { w.SetTheme(a.current()) })
	}
}

var phases = map[touch.Phase]backend.Phase{
	touch.Down: backend.Began,
	touch.Move: backend.Moved,
	touch.Up:   backend.Ended,
}

func (a *App) afterBoot(ctx context.Context) bool {
	d := a.o.Display
	done := make(chan struct{}, 1)
	stop := d.Released.Listen(func(p panel.Priority) {
		if p == panel.PriorityBoot {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})
	defer stop()
	for d.Held(panel.PriorityBoot) {
		select {
		case <-ctx.Done():
			return false
		case <-done:
		}
	}
	return true
}

func (a *App) Run(ctx context.Context) error {
	defer a.player.Close()
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	d := a.o.Display
	var c *surface.Client
	for c = d.Helper(); c == nil || c.Err() != nil; c = d.Helper() {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(helperWait):
		}
	}
	if !a.afterBoot(ctx) {
		return nil
	}
	nw, nh := d.Native()
	if err := c.UIOpen(layerID, nw, nh, layerZ); err != nil {
		return fmt.Errorf("gui: opening the UI layer: %w", err)
	}
	defer func() { stop(); _ = c.VideoClose(layerID) }()

	rot := d.Orientation()
	vw, vh := rot.Size(nw, nh)
	gogui.SetTheme(a.current())
	a.ui.Reset()
	a.preview = &widgets.LivePreview{}
	w := gogui.SimpleWindow(a.o.Name, vw, vh, a, func(w *gogui.Window) { w.SetView(a.root) })
	r, err := backend.New(surface.UILayer{C: c, ID: layerID}, w)
	if err != nil {
		return err
	}
	r.SetRotation(int(rot) / 90)
	if a.o.Input != nil {
		detach := a.o.Input.Attach(
			func(f func()) { w.QueueCommand(func(*gogui.Window) { f() }) },
			func(request textentry.Request) (textentry.View, error) {
				return a.ui.Editor.Remote(w, request)
			},
		)
		defer detach()
	}

	a.mu.Lock()
	a.w, a.r = w, r
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.w, a.r = nil, nil
		a.mu.Unlock()
	}()

	var width atomic.Int32
	width.Store(int32(vw))
	type start struct {
		at   [2]int
		open bool
	}
	starts := map[int]start{}
	sh := a.o.Shell

	stopTouch := a.o.Touch.Contacts.Listen(func(t touch.Contact) {
		sh.Touch()
		a.touched.contact(t)
		if marks := a.o.Settings().Screen.Touches; marks.Shown() {
			w.QueueCommand(func(w *gogui.Window) { w.InvalidateLayout() })
			if marks == schema.TouchesDots && t.Phase == touch.Up {
				time.AfterFunc(touchLinger, func() {
					w.QueueCommand(func(w *gogui.Window) { w.InvalidateLayout() })
				})
			}
		}
		if _, held := a.wakeBy(); held {
			return
		}
		at := image.Pt(t.X, t.Y)
		switch t.Phase {
		case touch.Down:
			a.o.Messages.Dismiss(t.ID, at)
			a.o.Messages.Grab(t.ID, at)
			a.o.Assistant.Cancel(t.ID)
		case touch.Move:
			a.o.Messages.Drag(t.ID, at)
		case touch.Up:
			a.o.Messages.Release(t.ID, at)
		}
		if a.o.Messages.Took(t.ID) || a.o.Assistant.Took(t.ID) {
			return
		}
		p, ok := phases[t.Phase]
		if !ok {
			return
		}
		switch t.Phase {
		case touch.Down:
			starts[t.ID] = start{at: [2]int{t.X, t.Y}, open: sh.Open()}
		case touch.Up:
			from, seen := starts[t.ID]
			delete(starts, t.ID)
			if seen && from.open && navigation.BackSweep(from.at, t.X, t.Y, int(width.Load())) {
				r.Touch(backend.Cancelled, uint64(t.ID), float32(t.X), float32(t.Y))
				w.QueueCommand(func(w *gogui.Window) {
					sh.Pop()
					w.InvalidateLayout()
				})
				return
			}
		}
		r.Touch(p, uint64(t.ID), float32(t.X), float32(t.Y))
		if t.Phase == touch.Up && len(starts) == 0 {
			w.QueueCommand(func(w *gogui.Window) {
				a.ui.Gestures.Lift()
				a.ui.Presses.Lift(w)
			})
		}
	})
	defer stopTouch()
	stopGesture := a.o.Touch.Gestures.Listen(func(g touch.Gesture) {
		a.touched.gesture(gestureName(g))
		if want, held := a.wakeBy(); held && gestureSetting(g) == want {
			w.QueueCommand(func(w *gogui.Window) {
				if top := sh.Top(); resting(top) {
					sh.Remove(top)
				}
				w.InvalidateLayout()
			})
		}
	})
	defer stopGesture()

	var look atomic.Value
	look.Store(a.current().Cfg.Name)
	restyle := func() {
		if now := a.current().Cfg.Name; now != look.Load() {
			look.Store(now)
			a.Restyle()
		}
	}
	stopStyle := a.o.Screen.Restyled.Listen(func(schema.Screen) { restyle() })
	defer stopStyle()
	stopTheme := a.o.Screen.Themed.Listen(func(theme.Theme) { restyle() })
	defer stopTheme()

	if a.o.Turned != nil {
		stopTurn := a.o.Turned.Listen(func(o geometry.Orientation) {
			w.QueueCommand(func(w *gogui.Window) {
				r.SetRotation(int(o) / 90)
				ww, hh := o.Size(nw, nh)
				width.Store(int32(ww))
				w.EventFn(&gogui.Event{Type: gogui.EventResized, WindowWidth: ww, WindowHeight: hh})
			})
		})
		defer stopTurn()
	}

	redraw := func() { w.QueueCommand(func(w *gogui.Window) { w.InvalidateLayout() }) }

	stopShell := sh.Redrawn.Listen(func(struct{}) { redraw() })
	defer stopShell()
	stopMoved := sh.Changed.Listen(func(c shell.Change) {
		a.changed(c)
		w.QueueCommand(func(w *gogui.Window) {
			a.ui.Editor.Shift, a.ui.Editor.Symbols = false, false
			w.ClearFocus()
		})
	})
	defer stopMoved()
	for _, o := range []*navigation.Overlay{
		{Priority: priorityAlarm, Build: a.alarm, Covers: a.alarmRinging},
		{Priority: priorityAlert, Build: a.alert, Covers: a.alertCovers},
		{Priority: priorityNotice, Build: a.timer, Covers: a.timerCovers},
		{Priority: priorityNotice + 1, Build: a.assistantPanel, Covered: a.o.Stage.Unstage},
		{Priority: playerview.PriorityMini, Build: a.player.Mini},
		{Priority: priorityMarks, Build: a.privacyMarks},
		{Priority: priorityTouches, Build: a.touchMarks},
		{Priority: widgets.UpgradePriority, Build: a.upgradeCard, Covers: a.upgradeCovers},
	} {
		a.nav.Show(o)
		defer a.nav.Hide(o)
	}
	stopMessage := a.o.Messages.Changed.Listen(func(bool) { redraw() })
	defer stopMessage()
	var spinning atomic.Bool
	stopUpgrade := a.o.Upgrading(func() {
		redraw()
		if !a.o.Upgrade().Active || !spinning.CompareAndSwap(false, true) {
			return
		}
		go func() {
			defer spinning.Store(false)
			for a.o.Upgrade().Active {
				redraw()
				select {
				case <-ctx.Done():
					return
				case <-time.After(widgets.UpgradeFrame):
				}
			}
			redraw()
		}()
	})
	defer stopUpgrade()
	stopTimer := a.o.Timers.Changed.Listen(func(timer.Card) { redraw() })
	defer stopTimer()
	stopAssistant := a.o.Assistant.Frame.Listen(func(assistant.Showing) { redraw() })
	defer stopAssistant()
	for _, watch := range a.o.Redraws {
		defer watch(redraw)()
	}

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				redraw()
			}
		}
	}()

	slog.Info("gui: running", "size", fmt.Sprintf("%dx%d", vw, vh), "theme", a.current().Cfg.Name)
	return r.Run(ctx, w)
}
