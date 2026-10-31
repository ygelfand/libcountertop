package firmwareview

import (
	"context"
	"fmt"
	"image"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"
	esphome "github.com/ygelfand/go-esphome-device"

	backend "github.com/ygelfand/libcountertop/pkg/display/gogui"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/tessera/firmware"
	"github.com/ygelfand/libcountertop/pkg/tessera/firmwarerelay"
)

type Options struct {
	Helper   string
	Screen   func(upright bool) Screen
	Panel    func() (width, height int)
	Diagonal float64
	Language func() string
	Grid     func() (columns, rows int)
	Theme    func() theme.Theme
	Themed   func() bool
	Turn     func() schema.TesseraTurn
	Bottom   func() schema.TesseraBottom
	Follow   func() bool
	Product  string
	Board    string
	Redraw   func()
}

type Screen struct {
	Dir   string
	Relay *firmwarerelay.Relay
}

type look struct {
	upright  bool
	language string
	columns  int
	rows     int
}

type View struct {
	opts   Options
	live   *backend.Live
	motion *motion

	mu        sync.Mutex
	runs      []*run
	front     *run
	preferred string
	turning   bool
	down      bool

	wake atomic.Pointer[func()]
}

type run struct {
	helper *firmware.Helper
	screen Screen
	look   look
	feed   *feed
	stop   context.CancelFunc
	done   chan struct{}
}

func New(o Options) *View {
	return &View{opts: o, live: backend.NewLive("tessera-firmware"), motion: newMotion()}
}

func (v *View) On() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.front != nil
}

func (v *View) Rests() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.front != nil && v.front.screen.Relay != nil && v.front.screen.Relay.Standby()
}

func (v *View) relays() []*firmwarerelay.Relay {
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []*firmwarerelay.Relay
	for _, r := range v.runs {
		if r.screen.Relay != nil {
			out = append(out, r.screen.Relay)
		}
	}
	return out
}

func (v *View) Set(on bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if on == (v.front != nil) {
		return
	}
	if !on {
		v.halt()
		return
	}
	w, h := v.opts.Panel()
	v.start(v.look(h > w))
}

func (v *View) look(upright bool) look {
	l := look{upright: upright, language: v.language()}
	if v.opts.Grid != nil {
		l.columns, l.rows = v.opts.Grid()
	}
	return l
}

func (v *View) prefs() string {
	if v.opts.Theme == nil {
		return ""
	}
	bottom := schema.BottomDock
	if v.opts.Bottom != nil {
		bottom = v.opts.Bottom()
	}
	return string(prefs(v.opts.Theme(), v.opts.Themed == nil || v.opts.Themed(), bottom))
}

func (v *View) Name() string { return "tessera firmware" }

func (v *View) Entities() []esphome.Entity {
	var out []esphome.Entity
	for _, r := range v.relays() {
		out = append(out, r.Entities()...)
	}
	return out
}

func (v *View) Actions() []*esphome.Action {
	var out []*esphome.Action
	for _, r := range v.relays() {
		out = append(out, r.Actions()...)
	}
	return out
}

func (v *View) language() string {
	if v.opts.Language == nil {
		return ""
	}
	return v.opts.Language()
}

func (v *View) halt() {
	for _, r := range v.runs {
		r.stop()
		<-r.done
		if err := r.helper.Close(); err != nil {
			slog.Warn("tessera firmware close", "err", err)
		}
	}
	v.runs, v.front = nil, nil
}

func (v *View) turn(l look) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.turning = false
	if v.front == nil || v.front.look == l {
		return
	}
	if r := v.ready(l); r != nil {
		v.show(r)
		return
	}
	v.halt()
	v.start(l)
}

func (v *View) ready(l look) *run {
	for _, r := range v.runs {
		if r.look == l {
			return r
		}
	}
	return nil
}

func (v *View) show(r *run) {
	if v.front != nil && v.front != r {
		v.front.feed.hide()
		v.front.helper.Frame().Rest(false)
	}
	v.front = r
	r.feed.reveal()
	v.motion.requests(r.helper.Frame().Want, v.opts.Follow)
}

func (v *View) start(l look) {
	v.preferred = v.prefs()
	other := l
	other.upright = !l.upright
	looks := []look{l}
	if v.opts.Screen(other.upright) != v.opts.Screen(l.upright) {
		looks = append(looks, other)
	}
	for _, k := range looks {
		r, err := v.launch(k)
		if err != nil {
			slog.Error("tessera firmware", "upright", k.upright, "err", err)
			continue
		}
		v.runs = append(v.runs, r)
	}
	if r := v.ready(l); r != nil {
		v.show(r)
	}
}

func (v *View) launch(l look) (*run, error) {
	w, h := v.opts.Panel()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("panel %dx%d", w, h)
	}
	screen := v.opts.Screen(l.upright)
	port := 0
	if screen.Relay != nil {
		port = screen.Relay.Port()
	}
	dpi := 0
	if v.opts.Diagonal > 0 {
		dpi = int(math.Round(math.Hypot(float64(w), float64(h)) / v.opts.Diagonal))
	}
	h2, err := firmware.Open(firmware.Options{
		Helper:   v.opts.Helper,
		Dir:      screen.Dir,
		Port:     port,
		Width:    min(w, h),
		Height:   max(w, h),
		DPI:      dpi,
		Upright:  l.upright,
		Language: l.language,
		Product:  v.opts.Product,
		Columns:  l.columns,
		Rows:     l.rows,
		Board:    v.opts.Board,
		Prefs:    []byte(v.preferred),
	})
	if err != nil {
		return nil, err
	}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	feed := &feed{
		frame:  h2.Frame(),
		live:   v.live,
		motion: v.motion,
		turn:   v.opts.Turn,
		redraw: v.opts.Redraw,
		wake:   v.woke,
	}
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		wg.Go(func() { _ = h2.Run(ctx) })
		wg.Go(func() { feed.run(ctx) })
		if screen.Relay != nil {
			wg.Go(func() { _ = screen.Relay.Run(ctx) })
		}
		wg.Wait()
	}()
	return &run{helper: h2, screen: screen, look: l, feed: feed, stop: stop, done: done}, nil
}

func (v *View) Draw(at ui.Rect) gogui.View {
	v.rest(nil)
	return v.draw(at)
}

func (v *View) Saver(at ui.Rect, wake func()) gogui.View {
	v.rest(wake)
	return v.draw(at)
}

func (v *View) rest(wake func()) {
	if wake == nil {
		v.wake.Store(nil)
	} else {
		v.wake.Store(&wake)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.front != nil {
		v.front.helper.Frame().Rest(wake != nil)
	}
}

func (v *View) SaverUp() bool { return v.wake.Load() != nil }

func (v *View) woke() {
	if wake := v.wake.Swap(nil); wake != nil {
		go (*wake)()
	}
}

func (v *View) draw(at ui.Rect) gogui.View {
	v.follow(v.look(at.H > at.W), v.prefs())
	touch := func(down bool) func(gogui.EventCtx) {
		return func(e gogui.EventCtx) {
			v.touch(e, at, down)
			e.Consume()
		}
	}
	move := func(e gogui.EventCtx) {
		if v.pressed() {
			v.touch(e, at, true)
			e.Consume()
		}
	}
	content := []gogui.View{
		gogui.Image(gogui.ImageCfg{Src: v.live.Src(), Width: float32(at.W), Height: float32(at.H)}),
	}
	if over := v.motion.overlay(at, v.live.Src()); over != nil {
		content = append(content, over)
	}
	return gogui.Column(gogui.ContainerCfg{
		ID:          "tessera-firmware",
		Sizing:      gogui.FixedFixed,
		Width:       float32(at.W),
		Height:      float32(at.H),
		Padding:     gogui.NoPadding,
		OnMouseDown: touch(true),
		OnMouseMove: move,
		OnMouseUp:   touch(false),
		Content:     content,
	})
}

func (v *View) follow(l look, prefs string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if prefs != v.preferred {
		v.preferred = prefs
		for _, r := range v.runs {
			if err := r.helper.Prefer([]byte(prefs)); err != nil {
				slog.Warn("tessera firmware prefs", "err", err)
			}
		}
	}
	if v.front == nil || v.front.look == l || v.turning {
		return
	}
	v.turning = true
	go v.turn(l)
}

func (v *View) pressed() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.down
}

func (v *View) touch(e gogui.EventCtx, at ui.Rect, down bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.down = down
	if v.front == nil || e.Event == nil {
		return
	}
	f := v.front.helper.Frame()
	w, h := f.Size()
	if w == 0 || at.W == 0 || at.H == 0 {
		return
	}
	s := e.Layout.Shape
	x := int((e.Event.MouseX - s.X) * float32(w) / float32(at.W))
	y := int((e.Event.MouseY - s.Y) * float32(h) / float32(at.H))
	slog.Debug("touch out", "down", down, "x", x, "y", y)
	v.motion.track(float32(x), down)
	f.Touch(x, y, down)
}

type feed struct {
	frame  *firmware.Frame
	live   *backend.Live
	motion *motion
	turn   func() schema.TesseraTurn
	redraw func()
	wake   func()

	cur, prev, rgba, old []byte
	w, h                 int
	seq, turns, woke     uint32
	shown, moved         bool

	on, fresh atomic.Bool
}

func (f *feed) reveal() {
	f.fresh.Store(true)
	f.on.Store(true)
}

func (f *feed) hide() { f.on.Store(false) }

func (f *feed) run(ctx context.Context) {
	tick := time.NewTicker(time.Second / 60)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if !f.on.Load() {
				continue
			}
			moving := f.motion.moving()
			if f.pull() || moving || f.moved {
				f.redraw()
			}
			f.moved = moving
		}
	}
}

func (f *feed) pull() bool {
	if f.fresh.Swap(false) {
		f.shown = false
	}
	if t := f.frame.Turn(); t.Count != f.turns {
		f.turns = t.Count
		f.turned(t)
	}
	if n := f.frame.Woke(); n != f.woke {
		f.woke = n
		f.wake()
	}
	if f.shown && f.frame.Seq() == f.seq {
		return false
	}
	w, h := f.frame.Size()
	n := w * h * 2
	if n == 0 {
		return false
	}
	if w != f.w || h != f.h {
		f.cur, f.prev, f.rgba = make([]byte, n), make([]byte, n), make([]byte, w*h*4)
		f.w, f.h, f.shown = w, h, false
	}
	seq, ok := f.frame.Copy(f.cur)
	if !ok {
		return false
	}
	top, bottom := 0, h
	if f.shown {
		top, bottom = changedRows(f.prev, f.cur, w*2)
	}
	f.seq = seq
	f.cur, f.prev = f.prev, f.cur
	if top >= bottom {
		return false
	}
	rows := f.rgba[:(bottom-top)*w*4]
	toRGBA(rows, f.prev[top*w*2:bottom*w*2])
	f.live.Write(w, h, image.Rect(0, top, w, bottom), rows)
	f.shown = true
	return true
}

func (f *feed) turned(t firmware.Turn) {
	if !f.shown || f.turn == nil || f.motion.skip() {
		return
	}
	kind := f.turn()
	area := t.Area.Intersect(image.Rect(0, 0, f.w, f.h))
	if kind == schema.TurnNone || kind == "" || area.Empty() || t.Step == 0 {
		return
	}
	n := area.Dx() * area.Dy() * 4
	if len(f.old) < n {
		f.old = make([]byte, n)
	}
	row := area.Dx() * 4
	for y := area.Min.Y; y < area.Max.Y; y++ {
		i := (y*f.w + area.Min.X) * 2
		toRGBA(f.old[(y-area.Min.Y)*row:(y-area.Min.Y+1)*row], f.prev[i:i+area.Dx()*2])
	}
	f.motion.old.Write(f.w, f.h, area, f.old[:n])
	p := f.old[:4]
	ground := gogui.RGB(p[0], p[1], p[2])
	f.motion.start(kind, t.Step, t.From, area, image.Pt(f.w, f.h), ground)
}

func changedRows(a, b []byte, stride int) (top, bottom int) {
	rows := len(a) / stride
	for top < rows && string(a[top*stride:(top+1)*stride]) == string(b[top*stride:(top+1)*stride]) {
		top++
	}
	bottom = rows
	for bottom > top &&
		string(a[(bottom-1)*stride:bottom*stride]) == string(b[(bottom-1)*stride:bottom*stride]) {
		bottom--
	}
	return top, bottom
}

func toRGBA(dst, rgb565 []byte) {
	for i := 0; i+1 < len(rgb565); i += 2 {
		p := uint16(rgb565[i]) | uint16(rgb565[i+1])<<8
		r, g, b := byte(p>>11), byte(p>>5&0x3f), byte(p&0x1f)
		o := i * 2
		dst[o] = r<<3 | r>>2
		dst[o+1] = g<<2 | g>>4
		dst[o+2] = b<<3 | b>>2
		dst[o+3] = 0xff
	}
}
