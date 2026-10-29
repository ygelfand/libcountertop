package thermostatview

import (
	"math"
	"strconv"
	"strings"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	kit "github.com/ygelfand/libcountertop/pkg/display/panelkit"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/thermostat"
	"github.com/ygelfand/libcountertop/pkg/say"
)

type skin struct {
	back, panel, ink, muted gogui.Color
	accent, accentInk       gogui.Color
	heat, cool              gogui.Color
	edge                    gogui.Color
	edgeWide                float32
	radius                  float32
	text                    kit.Type
	digits                  kit.Type
	glow                    bool
}

type face struct {
	v       *View
	w       *gogui.Window
	vw, vh  float32
	st      thermostat.State
	unit    string
	reserve float32
	still   bool
	board   bool
	now     time.Time
	sk      skin
}

type rect struct{ x, y, w, h float32 }

func (r rect) cx() float32 { return r.x + r.w/2 }
func (r rect) cy() float32 { return r.y + r.h/2 }

func (r rect) inset(d float32) rect {
	return rect{r.x + d, r.y + d, max(r.w-2*d, 1), max(r.h-2*d, 1)}
}

func (r rect) place(v gogui.View) gogui.View { return kit.Place(r.x, r.y, r.w, r.h, v) }

func (f *face) wear(sk skin) skin {
	f.sk = sk
	return sk
}

func (f *face) u() float32 { return min(f.vw, f.vh) }

func (f *face) wide() bool { return f.vw >= f.vh*1.15 }

func (f *face) tall() bool { return f.vh >= f.vw*1.15 }

func (f *face) live() bool { return !f.still && f.st.Available() }

func (f *face) animate() {
	if !f.still {
		f.v.again(f.w)
	}
}

func (f *face) breath(period time.Duration) float32 {
	f.animate()
	return kit.Breath(f.now, period)
}

func (f *face) phase(period time.Duration) float64 {
	f.animate()
	return kit.Phase(f.now, period)
}

func (f *face) number(v *float64) string {
	if v == nil {
		return "--"
	}
	if f.st.Step > 0 && f.st.Step < 1 && math.Abs(*v-math.Round(*v)) > 0.01 {
		return strconv.FormatFloat(*v, 'f', 1, 64)
	}
	return strconv.FormatFloat(math.Round(*v), 'f', 0, 64)
}

func (f *face) letter() string { return strings.TrimPrefix(f.unit, "°") }

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

func (f *face) name() string {
	if f.st.Name != "" {
		return f.st.Name
	}
	if f.st.Entity == "" {
		return say.T("thermostat.app")
	}
	return f.st.Entity
}

func (f *face) status() string {
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
	case f.st.Mode == thermostat.Off:
		return say.T("thermostat.off")
	case f.st.Ranged():
		return say.T("thermostat.between")
	}
	switch f.st.Action {
	case thermostat.Heating, thermostat.Preheating:
		return say.T("thermostat.heating")
	case thermostat.Cooling:
		return say.T("thermostat.cooling")
	case thermostat.Drying:
		return say.T("thermostat.drying")
	case thermostat.Fan:
		return say.T("thermostat.fanning")
	}
	return say.T("thermostat.holding")
}

func (f *face) tone(sk skin) gogui.Color {
	switch f.st.Action {
	case thermostat.Heating, thermostat.Preheating:
		return sk.heat
	case thermostat.Cooling:
		return sk.cool
	}
	switch f.st.Mode {
	case thermostat.Heat:
		return sk.heat
	case thermostat.Cool:
		return sk.cool
	case thermostat.Off:
		return sk.muted
	}
	return sk.accent
}

func (f *face) working() bool {
	switch f.st.Action {
	case thermostat.Heating, thermostat.Cooling, thermostat.Preheating, thermostat.Drying,
		thermostat.Fan:
		return true
	}
	return false
}

func (f *face) humidity() string {
	if f.st.Humidity == nil {
		return ""
	}
	return strconv.FormatFloat(math.Round(*f.st.Humidity), 'f', 0, 64) + "%"
}

func (f *face) facts() []string {
	var out []string
	if f.st.Current != nil {
		out = append(out, say.T("thermostat.in")+" "+f.number(f.st.Current)+"°")
	}
	if f.v.o.Outside != nil {
		if t, ok := f.v.o.Outside(); ok {
			out = append(out, say.T("thermostat.out")+" "+f.number(&t)+"°")
		}
	}
	if h := f.humidity(); h != "" {
		out = append(out, say.T("thermostat.humidity")+" "+h)
	}
	if f.st.Fan != "" {
		out = append(out, say.T("thermostat.fan")+" "+f.st.Fan)
	}
	return out
}

func (f *face) clock() string {
	if f.v.o.Clock24 != nil && f.v.o.Clock24() {
		return f.now.Format("15:04")
	}
	return f.now.Format("3:04 PM")
}

type choice struct {
	mode    thermostat.Mode
	label   string
	glyph   string
	current bool
	run     func()
}

var modeGlyphs = map[thermostat.Mode]string{
	thermostat.Heat:     "mdi:fire",
	thermostat.Cool:     "mdi:snowflake",
	thermostat.HeatCool: "mdi:sun-snowflake-variant",
	thermostat.Auto:     "mdi:thermostat-auto",
	thermostat.Dry:      "mdi:water-percent",
	thermostat.FanOnly:  "mdi:fan",
	thermostat.Off:      "mdi:power",
}

func (f *face) choices() []choice {
	modes := f.st.Modes
	if len(modes) == 0 && f.st.Mode != "" && f.st.Available() {
		modes = []thermostat.Mode{f.st.Mode}
	}
	out := make([]choice, 0, len(modes))
	for _, m := range modes {
		c := choice{
			mode:    m,
			label:   say.T("thermostat.mode." + string(m)),
			glyph:   modeGlyphs[m],
			current: m == f.st.Mode,
		}
		if f.live() {
			entity := f.st.Entity
			c.run = func() { f.v.setMode(entity, m) }
		}
		out = append(out, c)
	}
	return out
}

func (f *face) step(steps int) func() {
	if !f.live() || !f.st.Settable() {
		return nil
	}
	entity := f.st.Entity
	return func() { f.v.nudge(entity, steps) }
}

type btn struct {
	id       string
	r        rect
	fill     gogui.Color
	ink      gogui.Color
	edge     gogui.Color
	edgeWide float32
	radius   float32
	glow     gogui.Color
	content  []gogui.View
	run      func()
}

func (f *face) button(b btn) gogui.View {
	cfg := gogui.ContainerCfg{
		ID:          "thermostat-" + f.st.Entity + "-" + b.id,
		Width:       b.r.w,
		Height:      b.r.h,
		Sizing:      gogui.FixedFixed,
		Color:       b.fill,
		ColorBorder: b.edge,
		SizeBorder:  gogui.BorderPx(b.edgeWide),
		Radius:      gogui.RadiusPx(b.radius),
		HAlign:      gogui.HAlignCenter,
		VAlign:      gogui.VAlignMiddle,
		Padding:     gogui.NoPadding,
		Spacing:     gogui.SpacingPx(b.r.h * 0.14),
		Clip:        true,
		Content:     b.content,
	}
	if b.glow.A > 0 {
		cfg.Shadow = &gogui.BoxShadow{Color: b.glow, BlurRadius: min(b.r.w, b.r.h) * 0.35}
	}
	if b.run == nil || f.still {
		return gogui.Row(cfg)
	}
	run := b.run
	return f.v.o.UI.Presses.Pressable(gogui.Row, cfg, func(gogui.EventCtx) { run() })
}

func (f *face) arrow(sk skin, id string, r rect, up bool, glyph string, b btn) gogui.View {
	if glyph == "" {
		glyph = "mdi:chevron-down"
		if up {
			glyph = "mdi:chevron-up"
		}
	}
	steps := -1
	if up {
		steps = 1
	}
	b.id, b.r, b.run = id, r, f.step(steps)
	ink := b.ink
	if !ink.IsSet() {
		ink = sk.ink
	}
	if b.run == nil && !f.still {
		ink = ink.WithOpacity(0.35)
	}
	b.content = append([]gogui.View{kit.Icon(glyph, min(r.w, r.h)*0.62, ink)}, b.content...)
	return f.button(b)
}

type digits struct {
	size   float32
	ink    gogui.Color
	dim    gogui.Color
	deg    gogui.Color
	letter bool
	degree float32
}

func (f *face) reading(sk skin, r rect, d digits) gogui.View {
	t := sk.digits
	if f.st.Ranged() {
		return f.ranged(sk, r, d)
	}
	text := f.number(f.setpoint())
	size := d.size
	if size == 0 {
		size = t.Fit(f.w, text+"°", r.w*0.92, r.h, true)
	}
	degSize := size * 0.42
	if d.degree > 0 {
		degSize = size * d.degree
	}
	deg := "°"
	if d.letter {
		deg += f.letter()
	}
	dc := d.deg
	if !dc.IsSet() {
		dc = d.ink
	}
	return gogui.Row(gogui.ContainerCfg{
		Width: r.w, Height: r.h, Sizing: gogui.FixedFixed, Padding: gogui.NoPadding,
		HAlign: gogui.HAlignCenter, VAlign: gogui.VAlignTop,
		Content: []gogui.View{
			gogui.Column(gogui.ContainerCfg{
				Padding: gogui.NewPadding((r.h-size*1.2)/2, 0, 0, 0),
				Content: []gogui.View{t.Label(text, size, d.ink, true)},
			}),
			gogui.Column(gogui.ContainerCfg{
				Padding: gogui.NewPadding((r.h-size*1.2)/2+size*0.08, 0, 0, size*0.02),
				Content: []gogui.View{t.Label(deg, degSize, dc, true)},
			}),
		},
	})
}

func (f *face) ranged(sk skin, r rect, d digits) gogui.View {
	t := sk.digits
	lowText, highText := f.number(f.st.Low), f.number(f.st.High)
	size := d.size
	if size == 0 {
		size = t.Fit(f.w, lowText+"°–"+highText+"°", r.w*0.92, r.h*0.8, true)
	}
	chosen := f.v.end(f.st.Entity)
	part := func(e End, text string) gogui.View {
		ink := d.ink
		if chosen != e {
			ink = d.dim
			if !ink.IsSet() {
				ink = d.ink.WithOpacity(0.45)
			}
		}
		cfg := gogui.ContainerCfg{
			ID:      "thermostat-" + f.st.Entity + "-end-" + strconv.Itoa(int(e)),
			Padding: gogui.PadVH(0, size*0.06),
			Content: []gogui.View{t.Label(text+"°", size, ink, true)},
		}
		if f.still || !f.live() {
			return gogui.Row(cfg)
		}
		entity := f.st.Entity
		return f.v.o.UI.Presses.Pressable(gogui.Row, cfg, func(gogui.EventCtx) {
			f.v.pick(entity, e)
		})
	}
	return gogui.Row(gogui.ContainerCfg{
		Width: r.w, Height: r.h, Sizing: gogui.FixedFixed, Padding: gogui.NoPadding,
		HAlign: gogui.HAlignCenter, VAlign: gogui.VAlignMiddle,
		Content: []gogui.View{
			part(Low, lowText),
			t.Label("–", size*0.6, d.ink.WithOpacity(0.6), true),
			part(High, highText),
		},
	})
}

type modeStyle func(c choice, r rect) btn

func (f *face) modes(r rect, gap float32, style modeStyle) gogui.View {
	cs := f.choices()
	if len(cs) == 0 {
		return kit.Row(r.w, r.h, 0, gogui.HAlignCenter)
	}
	rows := 1
	if n := len(cs); n > 3 && (r.w-gap*float32(n-1))/float32(n) < r.h*0.9 {
		rows = 2
	}
	per := (len(cs) + rows - 1) / rows
	bh := (r.h - gap*float32(rows-1)) / float32(rows)
	lines := make([]gogui.View, 0, rows)
	for i := 0; i < len(cs); i += per {
		line := cs[i:min(i+per, len(cs))]
		bw := (r.w - gap*float32(per-1)) / float32(per)
		keys := make([]gogui.View, 0, len(line))
		for _, c := range line {
			b := style(c, rect{w: bw, h: bh})
			b.id, b.r, b.run = "mode-"+string(c.mode), rect{w: bw, h: bh}, c.run
			keys = append(keys, f.button(b))
		}
		lines = append(lines, kit.Row(r.w, bh, gap, gogui.HAlignCenter, keys...))
	}
	return kit.Column(r.w, r.h, gap, gogui.HAlignCenter, lines...)
}

func (f *face) modeContent(sk skin, c choice, r rect, ink gogui.Color, glyph bool) []gogui.View {
	size := sk.text.Fit(f.w, c.label, r.w*0.7, r.h*0.36, true)
	out := []gogui.View{}
	if glyph && c.glyph != "" && r.w > r.h*1.6 {
		out = append(out, kit.Icon(c.glyph, r.h*0.42, ink))
		size = sk.text.Fit(f.w, c.label, r.w*0.5, r.h*0.34, true)
	}
	return append(out, sk.text.Label(c.label, size, ink, true))
}

func (f *face) canvas(
	id string,
	r rect,
	version uint64,
	draw func(dc *gogui.DrawContext),
) gogui.View {
	return r.place(gogui.DrawCanvas(gogui.DrawCanvasCfg{
		ID:      "thermostat-" + f.st.Entity + "-" + id,
		Width:   r.w,
		Height:  r.h,
		Sizing:  gogui.FixedFixed,
		Version: version,
		OnDraw:  draw,
	}))
}

func (f *face) facade(sk skin, views ...gogui.View) gogui.View {
	return kit.Stack(f.vw, f.vh, views...)
}

func (f *face) line(sk skin, r rect, text string, c gogui.Color, bold bool) gogui.View {
	return r.place(kit.Row(r.w, r.h, 0, gogui.HAlignLeft,
		sk.text.Fitted(f.w, text, r.w, r.h, c, bold)))
}

func (f *face) centered(sk skin, r rect, text string, c gogui.Color, bold bool) gogui.View {
	return r.place(kit.Row(r.w, r.h, 0, gogui.HAlignCenter,
		sk.text.Fitted(f.w, text, r.w, r.h, c, bold)))
}

func (f *face) right(sk skin, r rect, text string, c gogui.Color, bold bool) gogui.View {
	return r.place(kit.Row(r.w, r.h, 0, gogui.HAlignRight,
		sk.text.Fitted(f.w, text, r.w, r.h, c, bold)))
}

func ink(c gogui.Color, on bool, off gogui.Color) gogui.Color {
	if on {
		return c
	}
	return off
}
