package thermostatview

import (
	"math"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/thermostat"
)

const (
	dialStart = 135.0
	dialSweep = 270.0
)

var dialLook = look{
	back: hex(0x14171a),
	sheet: sheet{
		shade: rgba(0x000000, 0.6), panel: hex(0x1e2227), ink: hex(0xeef1f4),
		muted: hex(0x8b949e), accent: hex(0xff8a3d), chosen: hex(0x2a2f36),
		radius: 0.03, edge: hex(0x2a2f36),
	},
	draw: drawDial,
}

func (f *face) dialTone() gogui.Color {
	switch f.doing() {
	case heating:
		return hex(0xff8a3d)
	case cooling:
		return hex(0x4fa8ff)
	}
	return hex(0x8b949e)
}

func drawDial(f *face) {
	w, h := f.rw, f.rh
	k := map[shape]float32{full: 1.25, mid: 0.85, tall: 0.82, small: 0.62}[f.shape]
	cx, cy := w/2, h*0.47
	r := min(w, h) * 0.34
	stroke := max(10, r*0.09)
	tone := f.dialTone()
	lo, hi := f.span()
	angle := func(v float64) float64 { return dialStart + frac(v, lo, hi)*dialSweep }
	at := func(v float64, rad float32) (float32, float32) {
		a := angle(v) * math.Pi / 180
		return cx + rad*float32(math.Cos(a)), cy + rad*float32(math.Sin(a))
	}
	sans := font{}
	light := font{family: style.LightFamily}
	ink, muted, dim := hex(0xeef1f4), hex(0x8b949e), hex(0xc4cbd3)
	set := f.setpoint()
	var shownSet float64
	if set != nil {
		shownSet = f.eased("set", *set)
	}
	inside := f.st.Current
	goal := f.goal(shownSet)
	handle := set != nil && f.st.Mode != thermostat.Off
	var otherEnd *float64
	if f.st.Ranged() {
		otherEnd = f.st.High
		if f.v.end(f.st.Entity) == High {
			otherEnd = f.st.Low
		}
	}
	top := 52 * k
	nameSize := max(14, 24*k)
	readSize := max(14, 20*k)
	f.paint(func(dc *gogui.DrawContext) {
		dc.Arc(cx, cy, r, r, float32(dialStart*math.Pi/180), float32(dialSweep*math.Pi/180),
			hex(0x262b31), stroke)
		if goal != nil && inside != nil && f.working() {
			a, b := min(*inside, *goal), max(*inside, *goal)
			dc.Arc(cx, cy, r, r, float32(angle(a)*math.Pi/180),
				float32((angle(b)-angle(a))*math.Pi/180), tone, stroke)
			for _, v := range []float64{a, b} {
				x, y := at(v, r)
				dc.FilledCircle(x, y, stroke/2, tone)
			}
		}
		ticks := 8
		for i := 0; i <= ticks; i++ {
			v := lo + (hi-lo)*float64(i)/float64(ticks)
			x0, y0 := at(v, r-stroke*1.4)
			x1, y1 := at(v, r-stroke*0.9)
			dc.Line(x0, y0, x1, y1, hex(0x3d444c), max(1.5, 2*k))
		}
		if inside != nil {
			x, y := at(*inside, r)
			dc.FilledCircle(x, y, stroke*0.28, ink)
		}
		if otherEnd != nil {
			x, y := at(*otherEnd, r)
			dc.FilledCircle(x, y, stroke*0.7, hex(0x101214))
			dc.Circle(x, y, stroke*0.55, dim, stroke*0.16)
		}
		if handle {
			x, y := at(shownSet, r)
			dc.FilledCircle(x, y, stroke*0.95, hex(0x101214))
			dc.FilledCircle(x, y, stroke*0.75, gogui.White)
			dc.Circle(x, y, stroke*0.75, tone, stroke*0.22)
		}
		write(dc, 30*k, top, f.name(), nameSize, ink, font{bold: true}, left)
		x := w - 30*k
		if hum := f.humidity(); hum != "" {
			x -= write(dc, x, top, hum, readSize, dim, sans, right)
			icon(dc, x-4, top-readSize*0.35, "mdi:water-outline", readSize*1.1, muted, right)
			x -= readSize*1.6 + 12*k
		}
		if inside != nil {
			x -= write(dc, x, top, f.number(inside)+"°", readSize, dim, sans, right)
			icon(dc, x-4, top-readSize*0.35, "mdi:thermometer", readSize*1.1, muted, right)
		}
		big := r * 0.62
		label := max(14, 18*k)
		if f.st.Ranged() {
			f.ends(dc, cx, cy-big*0.62, label, tone, hex(0x5d6670), font{spacing: 2}, center)
		} else {
			write(dc, cx, cy-big*0.62, upper(f.action()), label, tone, font{spacing: 2}, center)
		}
		if set != nil {
			degrees(dc, cx, cy+big*0.36, f.format(shownSet), big, ink, hex(0x9aa3ad), light, center)
		}
	})
	if goal != nil && inside != nil && f.working() {
		ph := f.phase(2400 * time.Millisecond)
		a, b := min(*inside, *goal), max(*inside, *goal)
		if f.doing() == cooling {
			ph = 1 - ph
		}
		v := a + (b-a)*ph
		f.motion(func(dc *gogui.DrawContext) {
			x, y := at(v, r)
			for i := 3; i >= 1; i-- {
				dc.FilledCircle(x, y, stroke*0.25*float32(i), gogui.White.WithOpacity(0.12))
			}
		})
	}
	bs := r * 0.17
	by := cy + r*0.72
	for i, sym := range []string{"−", "+"} {
		bx := cx + bs*1.3*float32(2*i-1)
		f.paint(func(dc *gogui.DrawContext) {
			dc.Circle(bx, by, bs, hex(0x3d444c), max(1.5, 2*k))
			write(dc, bx, by+bs*0.36, sym, bs*1.1, dim, sans, center)
		})
		f.step("step"+sym, rect{bx - bs*1.2, by - bs*1.2, bs * 2.4, bs * 2.4}, 2*i-1)
	}
	f.drag("ring", rect{cx - r - stroke*2, cy - r - stroke*2, 2*r + stroke*4, 2*r + stroke*4},
		func(x, y, _, _ float32, start float64) float64 {
			deg := math.Atan2(float64(y-cy), float64(x-cx)) * 180 / math.Pi
			rel := math.Mod(deg-dialStart+720, 360)
			if rel > dialSweep {
				if rel > dialSweep+(360-dialSweep)/2 {
					rel = 0
				} else {
					rel = dialSweep
				}
			}
			return lo + rel/dialSweep*(hi-lo)
		})
	if f.st.Ranged() {
		f.endTap("low", rect{cx - r*0.6, cy - r*0.62*0.95, r * 0.6, r * 0.3}, Low)
		f.endTap("high", rect{cx, cy - r*0.62*0.95, r * 0.6, r * 0.3}, High)
	}
	f.chips(chipStyle{
		fill: hex(0x1e2227), label: muted, value: ink, accent: tone, k: k,
		x: 30 * k, w: w - 60*k, y: h - 58*k - 26*k, h: 58 * k,
	})
}

func (f *face) ends(
	dc *gogui.DrawContext,
	x, base, size float32,
	on, off gogui.Color,
	fn font,
	a anchor,
) {
	low, high := f.number(f.st.Low)+"°", f.number(f.st.High)+"°"
	dash := "  –  "
	wl, wd, wh := measure(
		dc,
		low,
		size,
		fn,
	), measure(
		dc,
		dash,
		size,
		fn,
	), measure(
		dc,
		high,
		size,
		fn,
	)
	switch a {
	case center:
		x -= (wl + wd + wh) / 2
	case right:
		x -= wl + wd + wh
	}
	lc, hc := on, off
	if f.v.end(f.st.Entity) == High {
		lc, hc = off, on
	}
	write(dc, x, base, low, size, lc, fn, left)
	write(dc, x+wl, base, dash, size, off, fn, left)
	write(dc, x+wl+wd, base, high, size, hc, fn, left)
}

type chipStyle struct {
	fill, label, value, accent gogui.Color
	k                          float32
	x, y, w, h                 float32
}

func (f *face) kinds() []pick {
	out := []pick{pickMode}
	if f.has(pickPreset) {
		out = append(out, pickPreset)
	}
	if f.has(pickFan) {
		out = append(out, pickFan)
	}
	return out
}

func (f *face) pickValue(kind pick) string {
	switch kind {
	case pickPreset:
		return f.presetName()
	case pickFan:
		return f.fanName()
	}
	return f.modeName()
}

func modeGlyph(m thermostat.Mode) string {
	switch m {
	case thermostat.Cool:
		return "mdi:snowflake"
	case thermostat.HeatCool, thermostat.Auto:
		return "mdi:sun-snowflake-variant"
	case thermostat.Off:
		return "mdi:power"
	case thermostat.Dry:
		return "mdi:water-percent"
	case thermostat.FanOnly:
		return "mdi:fan"
	}
	return "mdi:fire"
}

var pickGlyphs = map[pick]string{
	pickMode:   "mdi:fire",
	pickPreset: "mdi:leaf",
	pickFan:    "mdi:fan",
}

func (f *face) chips(c chipStyle) {
	kinds := f.kinds()
	gap := 12 * c.k
	cw := min((c.w-gap*float32(len(kinds)-1))/float32(len(kinds)), 220*c.k)
	total := cw*float32(len(kinds)) + gap*float32(len(kinds)-1)
	x0 := c.x + (c.w-total)/2
	for i, kind := range kinds {
		x := x0 + float32(i)*(cw+gap)
		glyph := pickGlyphs[kind]
		tint := c.label
		if kind == pickMode {
			tint, glyph = c.accent, modeGlyph(f.st.Mode)
		}
		label, val := kind.title(), f.pickValue(kind)
		f.paint(func(dc *gogui.DrawContext) {
			dc.FilledRoundedRect(x, c.y, cw, c.h, c.h/2, c.fill)
			icon(dc, x+18*c.k, c.y+c.h/2, glyph, 22*c.k, tint, left)
			tx := x + 50*c.k
			if c.k >= 1 {
				write(dc, tx, c.y+c.h*0.42, label, 13*c.k, c.label, font{}, left)
				write(dc, tx, c.y+c.h*0.78, val, 18*c.k, c.value, font{}, left)
			} else {
				write(dc, tx, c.y+c.h*0.5+6, val, max(15, 18*c.k), c.value, font{}, left)
			}
		})
		f.chooser("chip"+label, rect{x, c.y, cw, c.h}, kind)
	}
}
