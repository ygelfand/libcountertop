package thermostatview

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/go-gui-org/go-glyph"
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/icons"
	kit "github.com/ygelfand/libcountertop/pkg/display/panelkit"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/thermostat"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type shape int

const (
	full shape = iota
	mid
	tall
	small
)

var reference = map[shape][2]float32{
	full:  {1280, 800},
	mid:   {800, 640},
	tall:  {640, 800},
	small: {640, 400},
}

func shapeOf(w, h float32) shape {
	switch a := w / h; {
	case a < 0.95:
		return tall
	case a < 1.4:
		return mid
	case min(w/1280, h/800) >= 0.8:
		return full
	}
	return small
}

type rect struct{ x, y, w, h float32 }

func (r rect) place(v gogui.View) gogui.View { return kit.Place(r.x, r.y, r.w, r.h, v) }

type paint func(dc *gogui.DrawContext)

type face struct {
	v      *View
	w      *gogui.Window
	vw, vh float32
	st     thermostat.State
	unit   string
	still  bool
	board  bool
	now    time.Time

	look   schema.ThermostatLook
	shape  shape
	s      float32
	rw, rh float32
	key    []any
	fixed  []paint
	moving []paint
	hits   []gogui.View
}

func (f *face) frame() {
	f.shape = shapeOf(f.vw, f.vh)
	ref := reference[f.shape]
	f.s = min(f.vw/ref[0], f.vh/ref[1])
	f.rw, f.rh = f.vw/f.s, f.vh/f.s
}

func (f *face) live() bool { return !f.still && f.st.Available() }

func (f *face) animate() {
	if !f.still {
		f.v.again(f.w)
	}
}

func (f *face) phase(period time.Duration) float64 {
	if f.still {
		return 0
	}
	f.animate()
	return kit.Phase(f.now, period)
}

func (f *face) breath(period time.Duration) float32 {
	if f.still {
		return 0
	}
	f.animate()
	return kit.Breath(f.now, period)
}

func (f *face) paint(p paint) { f.fixed = append(f.fixed, p) }

func (f *face) motion(p paint) { f.moving = append(f.moving, p) }

func (f *face) mark(parts ...any) { f.key = append(f.key, parts...) }

func (f *face) eased(name string, v float64) float64 {
	if f.still {
		return v
	}
	got, moving := f.v.eased(f.st.Entity+"/"+name, v, f.now)
	if moving {
		f.animate()
	}
	f.mark(got)
	return got
}

func (f *face) view(back gogui.Color) gogui.View {
	f.mark(
		f.look,
		f.vw,
		f.vh,
		f.st.Entity,
		f.st.Name,
		f.st.Mode,
		f.st.Action,
		f.st.Fan,
		f.st.Preset,
		value(f.st.Current),
		value(f.st.Target),
		value(f.st.Low),
		value(f.st.High),
		value(f.st.Humidity),
		f.v.end(f.st.Entity),
		f.v.note(f.st.Entity),
		f.outside(),
	)
	s := f.s
	layer := func(id string, version uint64, ops []paint) gogui.View {
		return kit.Place(0, 0, f.vw, f.vh, gogui.DrawCanvas(gogui.DrawCanvasCfg{
			ID:      "thermostat-" + string(f.look) + "-" + f.st.Entity + "-" + id,
			Width:   f.vw,
			Height:  f.vh,
			Sizing:  gogui.FixedFixed,
			Version: version,
			OnDraw: func(dc *gogui.DrawContext) {
				dc.Save()
				dc.ScaleBy(s, s)
				for _, op := range ops {
					op(dc)
				}
				dc.Restore()
			},
		}))
	}
	views := []gogui.View{layer("fixed", hashText(fmt.Sprint(f.key...)), f.fixed)}
	if len(f.moving) > 0 {
		views = append(views, layer("moving", uint64(f.now.UnixNano()/int64(frame)), f.moving))
	}
	views = append(views, f.hits...)
	return gogui.Stack(gogui.ContainerCfg{
		Width: f.vw, Height: f.vh, Sizing: gogui.FixedFixed, Padding: gogui.NoPadding,
		Color: back, Clip: true, Content: views,
	})
}

func (f *face) px(r rect) rect { return rect{r.x * f.s, r.y * f.s, r.w * f.s, r.h * f.s} }

func (f *face) tap(id string, r rect, run func()) {
	if run == nil || !f.live() {
		return
	}
	p := f.px(r)
	f.hits = append(f.hits, p.place(f.v.o.UI.Presses.Pressable(gogui.Column, gogui.ContainerCfg{
		ID:      "thermostat-" + f.st.Entity + "-" + id,
		Width:   p.w,
		Height:  p.h,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
	}, func(gogui.EventCtx) { run() })))
}

func (f *face) step(id string, r rect, steps int) {
	if !f.st.Settable() {
		return
	}
	entity := f.st.Entity
	f.tap(id, r, func() { f.v.nudge(entity, steps) })
}

func (f *face) chooser(id string, r rect, kind pick) {
	if len(options(f.st, kind)) < 2 {
		return
	}
	entity := f.st.Entity
	f.tap(id, r, func() { f.v.open(entity, kind) })
}

func (f *face) endTap(id string, r rect, e End) {
	if !f.st.Ranged() {
		return
	}
	entity := f.st.Entity
	f.tap(id, r, func() { f.v.pick(entity, e) })
}

type drag struct {
	key    string
	start  float64
	dx, dy float32
}

type pull func(x, y, dx, dy float32, start float64) float64

func (f *face) drag(id string, r rect, to pull) {
	if !f.live() || !f.st.Settable() {
		return
	}
	p := f.px(r)
	entity, key, s := f.st.Entity, f.st.Entity+"/"+id, f.s
	at := f.setpoint()
	if at == nil {
		return
	}
	start := *at
	f.hits = append(f.hits, p.place(gogui.Column(gogui.ContainerCfg{
		ID:      "thermostat-" + key + "-drag",
		Width:   p.w,
		Height:  p.h,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		OnGesture: func(e gogui.EventCtx) {
			if e.Event.GestureType != gogui.GesturePan {
				return
			}
			e.Consume()
			v := f.v
			v.mu.Lock()
			d := &v.dragging
			switch e.Event.GesturePhase {
			case gogui.GesturePhaseBegan:
				*d = drag{key: key, start: start}
			case gogui.GesturePhaseChanged:
				if d.key != key {
					*d = drag{key: key, start: start}
				}
			default:
				*d = drag{}
				v.mu.Unlock()
				return
			}
			d.dx += e.Event.GestureDX / s
			d.dy += e.Event.GestureDY / s
			x := (e.Event.CentroidX-e.Layout.Shape.X)/s + r.x
			y := (e.Event.CentroidY-e.Layout.Shape.Y)/s + r.y
			value := to(x, y, d.dx, d.dy, d.start)
			v.mu.Unlock()
			v.place(entity, value)
		},
	})))
}

func (f *face) setpoint() *float64 {
	if f.st.Ranged() {
		if f.v.end(f.st.Entity) == High {
			return f.st.High
		}
		return f.st.Low
	}
	if f.st.Mode == thermostat.Off {
		return f.st.Current
	}
	return f.st.Target
}

func (f *face) goal(shown float64) *float64 {
	if f.st.Mode == thermostat.Off || f.setpoint() == nil {
		return nil
	}
	if !f.st.Ranged() {
		return &shown
	}
	chosen := f.v.end(f.st.Entity)
	switch {
	case f.doing() == heating && chosen == Low, f.doing() == cooling && chosen == High:
		return &shown
	case f.doing() == heating:
		return f.st.Low
	case f.doing() == cooling:
		return f.st.High
	}
	return nil
}

func (f *face) span() (float64, float64) {
	lo, hi := f.st.Min, f.st.Max
	if hi <= lo {
		if f.unit == "°C" {
			return 7, 35
		}
		return 45, 95
	}
	return lo, hi
}

func (f *face) number(v *float64) string {
	if v == nil {
		return "--"
	}
	return f.format(*v)
}

func (f *face) format(v float64) string {
	if f.st.Step > 0 && f.st.Step < 1 && math.Abs(v-math.Round(v)) > 0.01 {
		return strconv.FormatFloat(v, 'f', 1, 64)
	}
	return strconv.FormatFloat(math.Round(v), 'f', 0, 64)
}

func (f *face) letter() string { return strings.TrimPrefix(f.unit, "°") }

func (f *face) name() string {
	if f.st.Name != "" {
		return f.st.Name
	}
	if f.st.Entity == "" {
		return say.T("thermostat.app")
	}
	return f.st.Entity
}

type doing int

const (
	resting doing = iota
	heating
	cooling
	stopped
)

func (f *face) doing() doing {
	switch f.st.Action {
	case thermostat.Heating, thermostat.Preheating:
		return heating
	case thermostat.Cooling:
		return cooling
	}
	if f.st.Mode == thermostat.Off || !f.st.Available() {
		return stopped
	}
	return resting
}

func (f *face) working() bool { return f.doing() == heating || f.doing() == cooling }

func (f *face) problem() string {
	if n := f.v.note(f.st.Entity); n != "" && !f.still {
		return n
	}
	switch {
	case f.st.Entity == "":
		return say.T("thermostat.none")
	case !f.st.Known():
		return say.T("thermostat.waiting")
	case !f.st.Available():
		return say.T("thermostat.unavailable")
	}
	return ""
}

func (f *face) action() string {
	if p := f.problem(); p != "" {
		return p
	}
	switch f.st.Action {
	case thermostat.Heating, thermostat.Preheating:
		return say.T("thermostat.action.heating")
	case thermostat.Cooling:
		return say.T("thermostat.action.cooling")
	case thermostat.Drying:
		return say.T("thermostat.action.drying")
	case thermostat.Fan:
		return say.T("thermostat.action.fan")
	}
	if f.st.Mode == thermostat.Off {
		return say.T("thermostat.action.off")
	}
	return say.T("thermostat.action.idle")
}

func (f *face) humidity() string {
	if f.st.Humidity == nil {
		return ""
	}
	return strconv.FormatFloat(math.Round(*f.st.Humidity), 'f', 0, 64) + "%"
}

func (f *face) outside() string {
	if f.v.o.Outside == nil {
		return ""
	}
	t, ok := f.v.o.Outside()
	if !ok {
		return ""
	}
	return f.format(t) + "°"
}

func (f *face) modeName() string { return modeLabel(f.st.Mode) }

func (f *face) presetName() string { return presetLabel(f.st.Preset) }

func (f *face) fanName() string {
	if f.st.Fan == "" {
		return "--"
	}
	return fanLabel(f.st.Fan)
}

func (f *face) has(kind pick) bool { return len(options(f.st, kind)) > 0 }

type font struct {
	family  string
	bold    bool
	spacing float32
}

type anchor int

const (
	left anchor = iota
	center
	right
)

func textStyle(size float32, c gogui.Color, fn font) gogui.TextStyle {
	st := gogui.CurrentTheme().Cfg.TextStyleDef
	st.Size, st.Color = min(max(size, 1), kit.GlyphMost), c
	if fn.family != "" {
		st.Family = fn.family
	}
	st.Typeface = glyph.TypefaceRegular
	if fn.bold {
		st.Typeface = glyph.TypefaceBold
	}
	st.LetterSpacing = fn.spacing
	return st
}

func write(
	dc *gogui.DrawContext,
	x, base float32,
	s string,
	size float32,
	c gogui.Color,
	fn font,
	a anchor,
) float32 {
	st := textStyle(size, c, fn)
	w := dc.TextWidth(s, st)
	switch a {
	case center:
		x -= w / 2
	case right:
		x -= w
	}
	dc.Text(x, base-dc.FontHeight(st)*0.8, s, st)
	return w
}

func measure(dc *gogui.DrawContext, s string, size float32, fn font) float32 {
	return dc.TextWidth(s, textStyle(size, gogui.White, fn))
}

func degrees(
	dc *gogui.DrawContext,
	x, base float32,
	s string,
	size float32,
	c, mark gogui.Color,
	fn font,
	a anchor,
) {
	w := measure(dc, s, size, fn)
	x0 := x
	switch a {
	case center:
		x0 = x - w/2
	case right:
		x0 = x - w
	}
	write(dc, x0, base, s, size, c, fn, left)
	write(dc, x0+w+size*0.03, base-size*0.42, "°", size*0.3, mark, fn, left)
}

func icon(
	dc *gogui.DrawContext,
	x, cy float32,
	name string,
	size float32,
	c gogui.Color,
	a anchor,
) {
	st := textStyle(size, c, font{family: icons.Family})
	s := icons.Glyph(icons.Named(name))
	w := dc.TextWidth(s, st)
	switch a {
	case center:
		x -= w / 2
	case right:
		x -= w
	}
	dc.Text(x, cy-dc.FontHeight(st)/2, s, st)
}

func upper(s string) string { return strings.ToUpper(s) }

func value(v *float64) string {
	if v == nil {
		return "-"
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func hashText(s string) uint64 {
	var h uint64 = 1469598103934665603
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

func hex(v uint32) gogui.Color { return kit.Hex(v) }

func rgba(v uint32, a float32) gogui.Color { return kit.Hex(v).WithOpacity(a) }

func polar(cx, cy, r float32, deg float64) (float32, float32) {
	a := deg * math.Pi / 180
	return cx + r*float32(math.Sin(a)), cy - r*float32(math.Cos(a))
}
