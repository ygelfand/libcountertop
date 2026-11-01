package idleview

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/clockview"
	"github.com/ygelfand/libcountertop/pkg/display/geometry"
	"github.com/ygelfand/libcountertop/pkg/display/gpu"
	"github.com/ygelfand/libcountertop/pkg/display/photoframe"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/visual"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	frameEvery = time.Second / 20
	moveEvery  = 30 * time.Second
	forever    = 100 * 365 * 24 * time.Hour
)

type View struct {
	options Dependencies
	mu      sync.Mutex
	vis     [2]visual.Visual
	kinds   [2]string
	running bool
	stop    context.CancelFunc
	done    chan struct{}
	shown   string
	spot    Spot
	moved   time.Time
	night   config.Night

	gmu    sync.Mutex
	gl     [2]*shading
	broken [2]visual.Visual

	frames [2]*photoframe.Frame
	stages [2]*photoframe.Stage
}

func New(options Dependencies) *View { return &View{options: options} }

func (v *View) Covers() bool           { return true }
func (v *View) Asleep() bool           { return true }
func (v *View) Timeout() time.Duration { return forever }

func (v *View) Night() config.Night {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.night
}

func (v *View) setNight(n config.Night) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.night = n
}

func Dark(idle config.Idle, clock config.Face, n config.Night) config.Idle {
	if !n.Quiet() {
		return idle
	}
	idle.First, idle.Second = config.IdleVisual{}, config.IdleVisual{}
	if n == config.NightOff {
		idle.Face = config.FaceNone
		return idle
	}
	if idle.Face == config.FaceNone {
		idle.Face = clock
	}
	idle.Placement = config.Placement{
		Position: config.PositionCenter,
		Align:    config.AlignCenter,
		Size:     config.SizeLarge,
	}
	return idle
}

func (v *View) read() Settings {
	cfg := v.options.Read()
	night := v.Night()
	if cfg.Idle.ScreenOff() {
		night = config.NightOff
	}
	cfg.Idle = Dark(cfg.Idle, cfg.Clock.Face, night)
	return cfg
}

func (v *View) Keep(w, h int) []config.IdleVisual {
	cfg := v.read()
	slots := Chosen(cfg.Idle)

	v.mu.Lock()
	if !v.running {
		v.running = true
		ctx, cancel := context.WithCancel(context.Background())
		v.stop, v.done = cancel, make(chan struct{})
		go v.run(ctx)
	}
	vis := make([]visual.Visual, len(slots))
	for n, slot := range slots {
		if slot.Photos() {
			v.vis[n], v.kinds[n] = nil, slot.Kind
			continue
		}
		if v.vis[n] == nil || v.kinds[n] != slot.Kind {
			v.vis[n], v.kinds[n] = visual.New(visual.Kind(slot.Kind)), slot.Kind
		}
		vis[n] = v.vis[n]
	}
	v.shown = key(cfg, time.Now())
	v.mu.Unlock()

	for n := len(slots); n < len(v.stages); n++ {
		v.unstage(n)
	}
	for n, area := range Areas(w, h, len(slots)) {
		if slots[n].Photos() {
			v.unshadeSlot(n)
			continue
		}
		v.unstage(n)
		v.shaded(n, vis[n], area, slots[n].Source)
	}
	return slots
}

func (v *View) Photo(n int, area ui.Rect, look photoframe.Look) bool {
	f := v.Frame(n)
	if f == nil {
		return false
	}
	v.mu.Lock()
	s := v.stages[n]
	if s == nil {
		s = photoframe.NewStage(f, photoframe.StageOptions{
			GPU:     v.options.GPU,
			Display: v.options.Display,
			Visible: func() bool { return v.options.Shell.Visible(v) },
		})
		v.stages[n] = s
	}
	v.mu.Unlock()
	return s.Keep(area, look)
}

func (v *View) unstage(n int) {
	v.mu.Lock()
	s := v.stages[n]
	v.mu.Unlock()
	if s != nil {
		s.Close()
	}
}

func (v *View) Frame(n int) *photoframe.Frame {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.frames[n] == nil && v.options.Photos != nil {
		f := photoframe.New(fmt.Sprintf("saver%d", n+1), v.options.Photos)
		f.Changed.Listen(func(int) { v.options.Shell.Redraw() })
		v.frames[n] = f
	}
	return v.frames[n]
}

func (v *View) Page(n int, forward bool) {
	f := v.Frame(n)
	if f == nil {
		return
	}
	if forward {
		f.Next(time.Now())
		return
	}
	f.Previous(time.Now())
}

func (v *View) turnPhotos(cfg Settings, now time.Time) {
	every := cfg.Idle.PhotoEvery.After()
	for n, slot := range Chosen(cfg.Idle) {
		if !slot.Photos() {
			continue
		}
		if f := v.Frame(n); f != nil && f.Due(now, every) {
			f.Next(now)
		}
	}
}

func visuals(slots []config.IdleVisual) int {
	n := 0
	for _, s := range slots {
		if !s.Photos() {
			n++
		}
	}
	return n
}

func Chosen(c config.Idle) []config.IdleVisual {
	var out []config.IdleVisual
	for _, v := range []config.IdleVisual{c.First, c.Second} {
		if v.On() {
			out = append(out, v)
		}
	}
	return out
}

func Areas(w, h, n int) []ui.Rect {
	switch {
	case n <= 0:
		return nil
	case n == 1:
		return []ui.Rect{{W: w, H: h}}
	case w > h:
		return []ui.Rect{{W: w / 2, H: h}, {X: w / 2, W: w - w/2, H: h}}
	}
	return []ui.Rect{{W: w, H: h / 2}, {Y: h / 2, W: w, H: h - h/2}}
}

func Reading(cfg Settings, at time.Time) clockview.Reading {
	r := clockview.Read(at, cfg.Screen.Hours == config.TwentyFourHour)
	if !cfg.Clock.Date {
		r = r.Undated()
	}
	return r
}

func key(cfg Settings, at time.Time) string {
	r := Reading(cfg, at)
	k := fmt.Sprintf("%v %+v ink=%s", r, cfg.Idle, cfg.Clock.Ink)
	if clockview.Ticks(cfg.Idle.Face) {
		k = fmt.Sprintf("%s second=%d", k, r.Second)
	}
	return k
}

type Piece struct {
	Clip    ui.Rect
	Palette theme.Theme
}

func Pieces(palette theme.Theme, tones []visual.Traits, areas []ui.Rect, box ui.Rect) []Piece {
	var out []Piece
	for n, area := range areas {
		if n >= len(tones) {
			break
		}
		if clip := intersect(area, box); clip.W > 0 {
			out = append(out, Piece{Clip: clip, Palette: tones[n].Under(palette)})
		}
	}
	if len(out) == 0 {
		return []Piece{{Palette: palette}}
	}
	if len(out) == 2 && out[0].Palette == out[1].Palette {
		return []Piece{{Palette: out[0].Palette}}
	}
	return out
}

func intersect(a, b ui.Rect) ui.Rect {
	x0, y0 := max(a.X, b.X), max(a.Y, b.Y)
	x1, y1 := min(a.X+a.W, b.X+b.W), min(a.Y+a.H, b.Y+b.H)
	if x1 <= x0 || y1 <= y0 {
		return ui.Rect{}
	}
	return ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

func (v *View) run(ctx context.Context) {
	var release func()
	defer func() {
		if release != nil {
			release()
		}
		v.unshadeAll()
		v.mu.Lock()
		v.running = false
		close(v.done)
		v.mu.Unlock()
	}()

	t := time.NewTicker(shadeEvery)
	defer t.Stop()
	var next, shaded time.Time
	for {
		var now time.Time
		select {
		case <-ctx.Done():
			return
		case now = <-t.C:
		}
		if !v.options.Shell.Visible(v) {
			return
		}
		if fps := v.options.Read().Visual.MaxFPS; fps <= 0 ||
			now.Sub(shaded) >= time.Second/time.Duration(fps)-2*time.Millisecond {
			shaded = now
			v.shade()
		}
		if now.Before(next) {
			continue
		}
		next = now.Add(frameEvery)

		cfg := v.read()
		v.turnPhotos(cfg, now)
		if slots := Chosen(cfg.Idle); visuals(slots) > 0 {
			if release == nil {
				release = v.options.Visuals.Hold()
			}
			if v.painting(slots) {
				v.options.Shell.Redraw()
				continue
			}
		} else {
			v.unshadeAll()
			if release != nil {
				release()
				release = nil
			}
		}

		v.mu.Lock()
		stale := v.shown != key(cfg, time.Now()) ||
			moving(cfg.Idle) && now.Sub(v.moved) >= moveEvery
		v.mu.Unlock()
		if stale {
			v.options.Shell.Redraw()
		}
	}
}

type Spot struct {
	At    config.Position
	Align config.Align
}

func moving(c config.Idle) bool {
	return c.Position == config.PositionRandom || c.Align == config.AlignRandom
}

func (v *View) Spot(c config.Idle, now time.Time) Spot {
	if !moving(c) {
		return Spot{c.Position, c.Align}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.moved.IsZero() || now.Sub(v.moved) >= moveEvery || !fits(v.spot, c) {
		v.spot, v.moved = next(v.spot, c), now
	}
	return v.spot
}

func fits(s Spot, c config.Idle) bool {
	return (c.Position == config.PositionRandom || s.At == c.Position) &&
		(c.Align == config.AlignRandom || s.Align == c.Align)
}

func next(from Spot, c config.Idle) Spot {
	var spots []Spot
	for _, at := range config.Positions() {
		for _, align := range config.Aligns() {
			if s := (Spot{at, align}); fits(s, c) && s != from {
				spots = append(spots, s)
			}
		}
	}
	return spots[rand.IntN(len(spots))]
}

type Settings struct {
	Screen config.Screen
	Clock  config.Clock
	Idle   config.Idle
	Visual config.Visual
}
type Visuals interface {
	Hold() func()
	Input() visual.Input
}
type Display interface{ Orientation() geometry.Orientation }
type Dependencies struct {
	Read    func() Settings
	Visuals Visuals
	GPU     *gpu.Renderer
	Display Display
	Shell   *shell.Shell
	Photos  photoframe.Source
}

func (v *View) Close() {
	v.mu.Lock()
	stop, done := v.stop, v.done
	v.mu.Unlock()
	if stop != nil {
		stop()
		<-done
	}
	for n := range v.stages {
		v.unstage(n)
	}
}
