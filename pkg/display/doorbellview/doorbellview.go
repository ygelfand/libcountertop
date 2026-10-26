package doorbellview

import (
	"context"
	"fmt"
	"image"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/camera/doorbell"
	"github.com/ygelfand/libcountertop/pkg/display/apps"
	"github.com/ygelfand/libcountertop/pkg/display/callvideo"
	"github.com/ygelfand/libcountertop/pkg/display/interaction"
	"github.com/ygelfand/libcountertop/pkg/display/message"
	"github.com/ygelfand/libcountertop/pkg/display/navigation"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const (
	ringFor          = 2 * time.Minute
	lingerFor        = 3 * time.Minute
	cardFor          = 20 * time.Second
	snapWait         = 15 * time.Second
	eventPictureWait = 3 * time.Second
	cameraWait       = 10 * time.Second
	thumbShare       = 2
)

type Speaker interface {
	Play(pcm []int16)
	Queued() int
}

type Cameras interface {
	Watch(entity string) bool
	Picture(ctx context.Context, entity string, w, h int) (*image.RGBA, error)
}

type Options struct {
	Shell    *shell.Shell
	Bell     *doorbell.Doorbell
	Display  callvideo.Display
	Speaker  Speaker
	Mic      func() (<-chan []int16, func())
	Messages *message.Messages
	Chime    func(schema.Chime)
	Cameras  Cameras
	Settings storage.Section[schema.Doorbell]
	Clock24  func() bool
	Wake     func()
}

type App struct{}

func (*App) Covers() bool { return true }

func (*App) Timeout() time.Duration { return lingerFor }

type Live struct{ rang bool }

func (*Live) Covers() bool { return true }

func (l *Live) Timeout() time.Duration {
	if l.rang {
		return ringFor
	}
	return lingerFor
}

type View struct {
	o        Options
	app      *App
	presses  *interaction.Presses
	shots    pictures
	spinning atomic.Bool

	mu   sync.Mutex
	live *session
}

func New(o Options) *View {
	v := &View{o: o, app: &App{}, presses: interaction.New()}
	o.Bell.Seen.Listen(v.seen)
	o.Bell.Changed.Listen(func(doorbell.State) { o.Shell.Redraw() })
	o.Shell.Changed.Listen(func(shell.Change) { v.follow() })
	return v
}

func (v *View) App() apps.App {
	return apps.App{
		ID:   schema.AppDoorbell,
		Name: func() string { return say.T("doorbell.app") },
		Icon: "mdi:doorbell-video",
		Open: func() {
			v.o.Shell.Push(v.app)
			safe.Go("doorbell pictures", v.freshen)
		},
	}
}

func (v *View) Screen(s shell.View) *navigation.Screen {
	if s == v.app {
		return &navigation.Screen{View: s, Clear: true, Build: v.buildApp}
	}
	v.mu.Lock()
	l := v.live
	v.mu.Unlock()
	if l != nil && s == l.view {
		return &navigation.Screen{View: s, Clear: true, Fixed: true, Build: l.build}
	}
	return nil
}

func (v *View) follow() {
	v.mu.Lock()
	l := v.live
	v.mu.Unlock()
	if l != nil && !v.o.Shell.Visible(l.view) {
		l.end()
		v.mu.Lock()
		if v.live == l {
			v.live = nil
		}
		v.mu.Unlock()
	}
}

func (v *View) seen(e doorbell.Event) {
	if len(e.Snapshot) > 0 {
		v.shots.keep(eventKey(e), e.Snapshot, v.screenMost()/thumbShare)
		v.shots.keep(doorKey(e.Slot), e.Snapshot, v.screenMost()/thumbShare)
	} else {
		v.assistantPicture(e)
	}
	keep := map[string]bool{}
	for _, r := range v.o.Bell.Recent() {
		keep[eventKey(r)] = true
	}
	v.shots.prune(eventPrefix, keep)
	if v.o.Wake != nil {
		v.o.Wake()
	}
	cfg := v.o.Settings.Read()
	style, chime, repeat, hold := cfg.Style, cfg.Notify, time.Duration(0), cardFor
	if e.Kind == doorbell.Visitor {
		style, chime = cfg.Visitor, cfg.Chime
		if style == schema.AlertAnswer {
			if v.o.Chime != nil {
				v.o.Chime(chime)
			}
			v.Watch(e.Slot, true)
			return
		}
		repeat = cfg.RingFor.After()
		hold = max(hold, repeat)
	}
	if style == schema.AlertNone || v.o.Messages == nil {
		return
	}
	pic, _ := v.shots.get(eventKey(e))
	slot := e.Slot
	v.o.Messages.Show(message.Message{
		Title:   e.Door,
		Body:    e.Says(),
		Tone:    schema.ToneInfo,
		Style:   style,
		Chime:   chime,
		Repeat:  repeat,
		Icon:    kindIcon(e.Kind),
		Picture: pic,
		Keys:    []message.Key{{Text: say.T("doorbell.watch")}},
		Ended: func(end message.End) {
			if end.Why == message.EndKey && end.Key == 0 {
				v.Watch(slot, false)
			}
		},
	}, hold)
}

func (v *View) Watch(slot int, rang bool) {
	v.mu.Lock()
	old := v.live
	l := &session{v: v, slot: slot, view: &Live{rang: rang}, began: time.Now()}
	v.live = l
	v.mu.Unlock()
	if old != nil {
		old.end()
		v.o.Shell.Remove(old.view)
	}
	v.o.Shell.Push(l.view)
	l.start()
}

func (v *View) freshen() {
	for _, d := range v.o.Bell.Doors() {
		v.doorPicture(d.Slot, v.screenMost()/thumbShare)
	}
}

func (v *View) doorPicture(slot, most int) {
	ctx, cancel := context.WithTimeout(context.Background(), snapWait)
	defer cancel()
	pic, err := v.o.Bell.Snapshot(ctx, slot)
	if err == nil {
		v.shots.keep(doorKey(slot), pic, most)
		v.o.Shell.Redraw()
		return
	}
	if v.o.Cameras == nil {
		slog.Debug("doorbell picture", "slot", slot, "err", err)
		return
	}
	entity, ok := v.o.Bell.Camera(ctx, slot)
	if !ok {
		return
	}
	img, err := v.o.Cameras.Picture(ctx, entity, most, most)
	if err != nil {
		slog.Debug("doorbell picture", "slot", slot, "camera", entity, "err", err)
		return
	}
	v.shots.keepImage(doorKey(slot), img)
	v.o.Shell.Redraw()
}

func (v *View) assistantPicture(e doorbell.Event) {
	if v.o.Cameras == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), eventPictureWait)
	defer cancel()
	entity, ok := v.o.Bell.Camera(ctx, e.Slot)
	if !ok {
		return
	}
	most := v.screenMost() / thumbShare
	img, err := v.o.Cameras.Picture(ctx, entity, most, most)
	if err != nil {
		slog.Warn("doorbell event picture from Home Assistant failed",
			"door", e.Door, "camera", entity, "err", err)
		return
	}
	v.shots.keepImage(eventKey(e), img)
	v.shots.keepImage(doorKey(e.Slot), img)
}

func (v *View) screenMost() int {
	if v.o.Display == nil {
		return 0
	}
	w, h := v.o.Display.Native()
	return max(w, h)
}

func (v *View) spinAgain(w *gogui.Window) {
	if v.spinning.Swap(true) {
		return
	}
	time.AfterFunc(spinStep, func() {
		v.spinning.Store(false)
		w.QueueCommand(func(w *gogui.Window) { w.InvalidateLayout() })
	})
}

const eventPrefix = "event/"

func eventKey(e doorbell.Event) string { return fmt.Sprintf("%s%d", eventPrefix, e.At.UnixNano()) }

func doorKey(slot int) string { return fmt.Sprintf("door/%d", slot) }

func kindIcon(k doorbell.Kind) string {
	switch k {
	case doorbell.Person:
		return "mdi:account"
	case doorbell.Package:
		return "mdi:package-variant-closed"
	case doorbell.Vehicle:
		return "mdi:car"
	case doorbell.Animal:
		return "mdi:paw"
	}
	return "mdi:doorbell"
}
