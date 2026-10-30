package homealarmview

import (
	"math"
	"strings"
	"time"

	"github.com/go-gui-org/go-glyph"
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/icons"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/homealarm"
	"github.com/ygelfand/libcountertop/pkg/say"
)

const pulsePeriod = 1200 * time.Millisecond

type skin struct {
	back, panel, key, keyInk gogui.Color
	ink, muted               gogui.Color
	calm, armed, warn, alarm gogui.Color
	chosen, chosenInk        gogui.Color
	edge                     gogui.Color
	edgeWide                 float32
	radius                   float32
	family                   string
	upper                    bool
	spaced                   bool
	glow                     bool
	flat                     bool
}

type action struct {
	id      string
	label   string
	glyph   string
	status  homealarm.Status
	run     func()
	current bool
}

type face struct {
	v        *View
	w        *gogui.Window
	vw, vh   float32
	state    homealarm.State
	dev      homealarm.Device
	note     string
	asking   bool
	popup    bool
	backdrop bool
	still    bool
	board    bool
	sk       skin
	now      time.Time
}

func (f *face) wear(sk skin) skin {
	f.sk = sk
	return sk
}

func (v *View) face(w *gogui.Window, popup bool) *face {
	vw, vh := w.WindowSize()
	note, asking := v.prompt()
	return &face{
		v:      v,
		w:      w,
		vw:     float32(vw),
		vh:     float32(vh),
		state:  v.shown(),
		dev:    v.shownDevice(),
		note:   note,
		asking: asking,
		popup:  popup,
		now:    time.Now(),
	}
}

func (f *face) unit() float32 { return min(f.vw, f.vh) }

func (f *face) title() string {
	switch f.state.Status {
	case homealarm.Unknown:
		if f.state.Entity == "" {
			return say.T("homealarm.status.unset")
		}
		return say.T("homealarm.status.waiting")
	case homealarm.Disarmed, homealarm.ArmedHome, homealarm.ArmedAway, homealarm.ArmedNight,
		homealarm.ArmedVacation, homealarm.ArmedBypass, homealarm.Arming, homealarm.Disarming,
		homealarm.Pending, homealarm.Triggered:
		return say.T("homealarm.status." + string(f.state.Status))
	}
	return say.T("homealarm.status.unavailable")
}

func (f *face) detail() string {
	if f.note != "" {
		return f.note
	}
	if r := f.ready(); r != "" {
		return r
	}
	return f.dev.Message
}

func (f *face) glyph() string {
	s := f.state.Status
	switch {
	case s == homealarm.Disarmed:
		return "mdi:shield-check-outline"
	case s.Armed():
		return "mdi:shield-lock-outline"
	case s == homealarm.Arming || s == homealarm.Disarming:
		return "mdi:shield-sync-outline"
	case s == homealarm.Pending:
		return "mdi:shield-alert-outline"
	case s == homealarm.Triggered:
		return "mdi:alarm-light-outline"
	}
	return "mdi:shield-off-outline"
}

func (f *face) tone(sk skin) gogui.Color {
	s := f.state.Status
	switch {
	case s == homealarm.Disarmed:
		return f.breathe(sk.calm, sk.back)
	case s.Armed():
		return f.breathe(sk.armed, sk.back)
	case s == homealarm.Arming || s == homealarm.Disarming || s == homealarm.Pending:
		return f.breathe(sk.warn, sk.back)
	case s == homealarm.Triggered:
		return f.flash(sk.alarm, sk.back)
	}
	return sk.muted
}

func (f *face) flash(c, toward gogui.Color) gogui.Color {
	if !f.still && (f.state.Status.Urgent() || f.state.Status == homealarm.Arming) {
		f.v.again(f.w)
	}
	phase := float64(f.now.UnixNano()%int64(pulsePeriod)) / float64(pulsePeriod)
	k := float32(0.35 * (1 - math.Cos(2*math.Pi*phase)) / 2)
	return mix(c, toward, k)
}

func mix(a, b gogui.Color, k float32) gogui.Color {
	lerp := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*k) }
	return gogui.RGBA(lerp(a.R, b.R), lerp(a.G, b.G), lerp(a.B, b.B), a.A)
}

var modeGlyphs = map[homealarm.Status]string{
	homealarm.ArmedHome:     "mdi:home-outline",
	homealarm.ArmedAway:     "mdi:logout",
	homealarm.ArmedNight:    "mdi:weather-night",
	homealarm.ArmedVacation: "mdi:airplane",
	homealarm.ArmedBypass:   "mdi:shield-half-full",
}

func (f *face) actions() []action {
	s := f.state.Status
	switch {
	case s == homealarm.Disarmed:
		var out []action
		for _, m := range f.state.Modes() {
			out = append(out, action{
				id:     "arm-" + string(m),
				label:  say.T("homealarm.mode." + string(m)),
				glyph:  modeGlyphs[m],
				status: m,
				run:    f.v.arm(m),
			})
		}
		return out
	case s.Armed(), s == homealarm.Arming, s == homealarm.Pending, s == homealarm.Triggered:
		return []action{{
			id:     "disarm",
			label:  say.T("homealarm.disarm"),
			glyph:  "mdi:shield-off-outline",
			status: homealarm.Disarmed,
			run:    f.v.disarm,
		}}
	}
	return nil
}

func (f *face) emergency() bool {
	return len(f.dev.Buttons) > 0 || f.state.CanTrigger() && f.state.Status != homealarm.Triggered
}

func (f *face) clock() string {
	if f.v.o.Clock24 != nil && f.v.o.Clock24() {
		return f.now.Format("15:04")
	}
	return f.now.Format("3:04")
}

func (f *face) meridiem() string {
	if f.v.o.Clock24 != nil && f.v.o.Clock24() {
		return ""
	}
	return f.now.Format("PM")
}

func (f *face) date() string { return f.now.Format("Monday, January 2") }

func (sk skin) onKey() gogui.Color {
	if sk.keyInk.IsSet() {
		return sk.keyInk
	}
	return sk.ink
}

func (sk skin) words(s string) string {
	if sk.upper {
		s = strings.ToUpper(s)
	}
	if sk.spaced {
		s = strings.Join(strings.Split(s, ""), " ")
	}
	return s
}

func (sk skin) text(size float32, c gogui.Color, bold bool) gogui.TextStyle {
	st := gogui.CurrentTheme().Cfg.TextStyleDef
	st.Size, st.Color = max(size, 1), c
	if sk.family != "" {
		st.Family = sk.family
	}
	if bold {
		st.Typeface = glyph.TypefaceBold
	}
	return st
}

func (sk skin) label(s string, size float32, c gogui.Color, bold bool) gogui.View {
	return gogui.Label(sk.words(s), sk.text(size, c, bold))
}

func icon(name string, size float32, c gogui.Color) gogui.View {
	st := widgets.IconStyle(c)
	st.Family, st.Size = icons.Family, size
	return gogui.Label(icons.Glyph(icons.Named(name)), st)
}

func (f *face) short(sk skin, s string, maxW, size float32, bold bool) string {
	st := sk.text(size, sk.ink, bold)
	r := []rune(sk.words(s))
	if f.w.TextWidth(string(r), st) <= maxW {
		return s
	}
	for len(r) > 1 {
		r = r[:len(r)-1]
		if f.w.TextWidth(string(r)+"…", st) <= maxW {
			break
		}
	}
	return string(r) + "…"
}

func (f *face) fit(sk skin, s string, maxW, maxH float32, bold bool) float32 {
	st := sk.text(maxH, sk.ink, bold)
	text := sk.words(s)
	size := maxH
	for range 8 {
		st.Size = size
		wide := f.w.TextWidth(text, st)
		if wide <= maxW || size <= 1 {
			return size
		}
		size = max(min(size*maxW/wide, size-1), 1)
	}
	return size
}

type key struct {
	id        string
	w, h      float32
	fill, ink gogui.Color
	edge      gogui.Color
	edgeWide  float32
	radius    float32
	content   []gogui.View
	align     gogui.HorizontalAlign
	run       func()
}

func (f *face) key(k key) gogui.View {
	cfg := gogui.ContainerCfg{
		ID:          "homealarm-" + k.id,
		Width:       k.w,
		Height:      k.h,
		Sizing:      gogui.FixedFixed,
		Color:       k.fill,
		ColorBorder: k.edge,
		SizeBorder:  gogui.BorderPx(k.edgeWide),
		Radius:      gogui.RadiusPx(k.radius),
		HAlign:      k.align,
		VAlign:      gogui.VAlignMiddle,
		Padding:     gogui.PadVH(0, k.h*0.22),
		Spacing:     gogui.SpacingPx(k.h * 0.18),
		Clip:        true,
		Content:     k.content,
	}
	if k.align == gogui.HAlignCenter {
		cfg.Padding = gogui.NoPadding
	}
	f.dress(&cfg, k)
	if k.run == nil || f.still {
		return gogui.Row(cfg)
	}
	run := k.run
	return f.v.o.UI.Presses.Pressable(gogui.Row, cfg, func(gogui.EventCtx) { run() })
}

func (f *face) holdKey(k key) gogui.View {
	run := k.run
	k.run = nil
	cfg := gogui.ContainerCfg{
		ID:          "homealarm-" + k.id,
		Width:       k.w,
		Height:      k.h,
		Sizing:      gogui.FixedFixed,
		Color:       k.fill,
		ColorBorder: k.edge,
		SizeBorder:  gogui.BorderPx(k.edgeWide),
		Radius:      gogui.RadiusPx(k.radius),
		HAlign:      gogui.HAlignCenter,
		VAlign:      gogui.VAlignMiddle,
		Padding:     gogui.NoPadding,
		Spacing:     gogui.SpacingPx(k.h * 0.15),
		Clip:        true,
		Content:     k.content,
	}
	f.dress(&cfg, k)
	if f.still {
		return gogui.Row(cfg)
	}
	cfg.OnClick = func(e gogui.EventCtx) {
		f.v.tell(say.T("homealarm.hold"))
		e.Consume()
	}
	cfg.OnGesture = func(e gogui.EventCtx) {
		if e.Event != nil && e.Event.GestureType == gogui.GestureLongPress &&
			e.Event.GesturePhase == gogui.GesturePhaseBegan {
			run()
			e.Consume()
		}
	}
	return gogui.Row(cfg)
}

func (f *face) emergencyKey(sk skin, w, h float32, fill, ink gogui.Color) gogui.View {
	buttons := f.dev.Buttons
	if len(buttons) == 0 {
		size := min(h*0.36, style.Reach()*0.7)
		return f.holdKey(key{
			id: "emergency", w: w, h: h, fill: fill, ink: ink, radius: sk.radius,
			content: []gogui.View{
				icon("mdi:alarm-light-outline", size*1.1, ink),
				sk.label(say.T("homealarm.emergency"), size, ink, true),
			},
			run: f.v.trigger,
		})
	}
	gap := min(w, h) * 0.06
	n := float32(len(buttons))
	stacked := (w-(n-1)*gap)/n < h*0.75 && (h-(n-1)*gap)/n >= w*0.22
	bw, bh := (w-(n-1)*gap)/n, h
	if stacked {
		bw, bh = w, (h-(n-1)*gap)/n
	}
	names := shortNames(buttons)
	glyph, textW, textH := bh*0.34, bw*0.82, bh*0.17
	if stacked {
		glyph, textW, textH = bh*0.46, bw-bh*1.2, bh*0.3
	}
	size := textH
	for _, n := range names {
		size = min(size, f.fit(sk, n, textW, textH, true))
	}
	bare := size < f.unit()*0.028
	if bare {
		glyph = min(bw, bh) * 0.5
	}
	keys := make([]gogui.View, 0, len(buttons))
	for i, b := range buttons {
		mark := icon(panicGlyph(b.Name), glyph, ink)
		content := []gogui.View{mark}
		if !bare {
			content = append(content, sk.label(names[i], size, ink, true))
		}
		if !stacked && !bare {
			content = []gogui.View{gogui.Column(gogui.ContainerCfg{
				Padding: gogui.NoPadding, HAlign: gogui.HAlignCenter,
				Spacing: gogui.SpacingPx(bh * 0.08), Content: content,
			})}
		}
		keys = append(keys, f.holdKey(key{
			id:       "button-" + b.Entity,
			w:        bw,
			h:        bh,
			fill:     fill,
			ink:      ink,
			edge:     sk.edge,
			edgeWide: sk.edgeWide,
			radius:   sk.radius,
			content:  content,
			run:      f.v.press(b.Entity),
		}))
	}
	if stacked {
		return column(w, h, gap, gogui.HAlignCenter, keys...)
	}
	return row(w, h, gap, gogui.HAlignCenter, keys...)
}

func hex(v uint32) gogui.Color { return gogui.RGB(uint8(v>>16), uint8(v>>8), uint8(v)) }

func (f *face) entries(most int) []string {
	log := f.v.history()
	if len(log) > most {
		log = log[len(log)-most:]
	}
	clock := "3:04 PM"
	if f.v.o.Clock24 != nil && f.v.o.Clock24() {
		clock = "15:04"
	}
	out := make([]string, 0, len(log))
	for i := len(log) - 1; i >= 0; i-- {
		e := log[i]
		line := e.at.Format(clock) + "  " + say.T("homealarm.status."+string(e.status))
		if e.by != "" {
			line += " · " + e.by
		}
		out = append(out, line)
	}
	return out
}

func (f *face) cornerWidth() float32 {
	if f.still || f.board {
		return 0
	}
	return float32(2*(widgets.CornerSide()+int(gogui.PadLarge)) + int(gogui.PadLarge))
}

func (f *face) corners() []gogui.View {
	p := f.v.o.UI.Presses
	side, pad := widgets.CornerSide(), int(gogui.PadLarge)
	x := int(f.vw) - pad - side
	level := f.v.o.Shell.Controls()
	out := []gogui.View{
		widgets.CloseCornerAt(p, "homealarm-close", x, level, func() { f.v.close(f.current()) }),
	}
	if !f.popup {
		out = append(out, widgets.SettingsCornerAt(p, "homealarm-settings", x-pad-side, level,
			func() { f.v.o.Shell.Push(f.v.Page()) }))
	}
	return out
}

func (f *face) current() shell.View {
	if f.popup {
		return f.v.popup
	}
	return f.v.app
}

func place(x, y, w, h float32, v gogui.View) gogui.View {
	return gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFill,
		Padding: gogui.NewPadding(max(y, 0), 0, 0, max(x, 0)),
		Content: []gogui.View{gogui.Column(gogui.ContainerCfg{
			Width: w, Height: h, Sizing: gogui.FixedFixed, Padding: gogui.NoPadding,
			Content: []gogui.View{v},
		})},
	})
}

func column(w, h, spacing float32, align gogui.HorizontalAlign, content ...gogui.View) gogui.View {
	return gogui.Column(gogui.ContainerCfg{
		Width: w, Height: h, Sizing: gogui.FixedFixed, Padding: gogui.NoPadding,
		Spacing: gogui.SpacingPx(spacing), HAlign: align, VAlign: gogui.VAlignMiddle,
		Content: content,
	})
}

func row(w, h, spacing float32, align gogui.HorizontalAlign, content ...gogui.View) gogui.View {
	return gogui.Row(gogui.ContainerCfg{
		Width: w, Height: h, Sizing: gogui.FixedFixed, Padding: gogui.NoPadding,
		Spacing: gogui.SpacingPx(spacing), HAlign: align, VAlign: gogui.VAlignMiddle,
		Content: content,
	})
}
