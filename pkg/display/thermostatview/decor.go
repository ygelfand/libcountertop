package thermostatview

import (
	"math"
	"strconv"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	kit "github.com/ygelfand/libcountertop/pkg/display/panelkit"
)

func (f *face) sweep(id string, r rect, c gogui.Color) gogui.View {
	if !f.working() || f.still {
		return kit.Stack(1, 1)
	}
	k := float32(f.phase(2200 * time.Millisecond))
	return f.canvas(id, r, kit.Version(r.w, r.h, k), func(dc *gogui.DrawContext) {
		w := r.w * 0.22
		x := -w + (r.w+w)*k
		dc.FilledRectGradient(max(x, 0), 0, min(w, r.w-x), r.h, &gogui.CanvasGradient{
			X1: x, Y1: 0, X2: x + w, Y2: 0, Stops: []gogui.GradientStop{
				{
					Color: c.WithOpacity(0),
					Pos:   0,
				},
				{Color: c, Pos: 0.5},
				{Color: c.WithOpacity(0), Pos: 1},
			},
		})
	})
}

func (f *face) halo(r rect, radius float32, c gogui.Color) gogui.View {
	if !f.working() || f.still {
		return kit.Stack(1, 1)
	}
	b := f.breath(2400 * time.Millisecond)
	g := min(r.w, r.h) * (0.03 + 0.05*b)
	wide := max(2, min(r.w, r.h)*0.025)
	return kit.Place(r.x-g, r.y-g, r.w+2*g, r.h+2*g, gogui.Row(gogui.ContainerCfg{
		Width: r.w + 2*g, Height: r.h + 2*g, Sizing: gogui.FixedFixed,
		ColorBorder: c.WithOpacity(0.55 * (1 - b*0.7)), SizeBorder: gogui.BorderPx(wide),
		Radius: gogui.RadiusPx(radius + g),
	}))
}

func (f *face) orbit(id string, r rect, c gogui.Color) gogui.View {
	if f.still || !f.st.Available() {
		return kit.Stack(1, 1)
	}
	period := 9 * time.Second
	if f.working() {
		period = 3 * time.Second
	}
	k := float32(f.phase(period))
	return f.canvas(id, r, kit.Version(r.w, r.h, k), func(dc *gogui.DrawContext) {
		rad := min(r.w, r.h)/2 - max(2, min(r.w, r.h)*0.012)
		start := k * 2 * math.Pi
		wide := max(2, min(r.w, r.h)*0.012)
		dc.Circle(r.w/2, r.h/2, rad, c.WithOpacity(0.08), wide)
		dc.Arc(r.w/2, r.h/2, rad, rad, start, math.Pi*0.35, c.WithOpacity(0.7), wide)
		ca, sa := float32(math.Cos(float64(start))), float32(math.Sin(float64(start)))
		dc.FilledCircle(r.w/2+ca*rad, r.h/2+sa*rad, wide*1.6, c)
	})
}

func (f *face) lamp(r rect, c, back gogui.Color) gogui.View {
	if f.still || !f.st.Available() {
		return kit.Stack(1, 1)
	}
	k := float32(math.Abs(2*f.phase(4*time.Second) - 1))
	return f.canvas("lamp", r, kit.Version(r.w, r.h, k), func(dc *gogui.DrawContext) {
		w := r.w * 0.06
		x := (r.w - w) * k
		dc.FilledRect(x-r.h*0.5, 0, w+r.h, r.h, back)
		dc.FilledRoundedRect(x, r.h*0.25, w, r.h*0.5, r.h*0.25, c)
	})
}

type dial struct {
	from, to      float64
	start, sweep  float64
	ticks, labels int
	value         *float64
	face, rim     gogui.Color
	tick, needle  gogui.Color
	text          kit.Type
	caption       string
	reading       string
	bezel         float32
	hub           gogui.Color
}

func (f *face) dial(id string, r rect, d dial) gogui.View {
	at := -1.0
	if d.value != nil {
		at = *d.value
	}
	return f.canvas(
		id,
		r,
		kit.Version(r.w, r.h, float32(at), float32(d.from)),
		func(dc *gogui.DrawContext) {
			cx, cy := r.w/2, r.h/2
			rad := min(r.w, r.h) / 2
			if d.bezel > 0 {
				dc.FilledCircle(cx, cy, rad, d.rim)
				rad -= d.bezel * rad
			}
			if d.face.A > 0 {
				dc.FilledCircle(cx, cy, rad, d.face)
			}
			if d.bezel == 0 && d.rim.A > 0 {
				dc.Circle(cx, cy, rad, d.rim, max(1, rad*0.02))
			}
			angle := func(v float64) float64 {
				k := min(max((v-d.from)/(d.to-d.from), 0), 1)
				return d.start + k*d.sweep
			}
			for i := 0; i <= d.ticks; i++ {
				a := d.start + float64(i)/float64(d.ticks)*d.sweep
				long := d.labels > 0 && i%(d.ticks/d.labels) == 0
				r0, r1 := rad*0.82, rad*0.9
				if long {
					r0 = rad * 0.76
				}
				ca, sa := float32(math.Cos(a)), float32(math.Sin(a))
				dc.Line(cx+ca*r0, cy+sa*r0, cx+ca*r1, cy+sa*r1, d.tick, max(1, rad*0.012))
				if long {
					v := d.from + float64(i)/float64(d.ticks)*(d.to-d.from)
					s := strconv.FormatFloat(v, 'f', 0, 64)
					st := d.text.Style(rad*0.13, d.tick, true)
					tw := dc.TextWidth(s, st)
					dc.Text(cx+ca*rad*0.62-tw/2, cy+sa*rad*0.62-rad*0.08, s, st)
				}
			}
			if d.caption != "" {
				st := d.text.Style(rad*0.13, d.tick, true)
				tw := dc.TextWidth(d.caption, st)
				dc.Text(cx-tw/2, cy-rad*0.36, d.caption, st)
			}
			if d.reading != "" {
				st := d.text.Style(rad*0.2, d.tick, true)
				tw := dc.TextWidth(d.reading, st)
				dc.Text(cx-tw/2, cy+rad*0.3, d.reading, st)
			}
			if d.value != nil {
				a := angle(*d.value)
				ca, sa := float32(math.Cos(a)), float32(math.Sin(a))
				dc.Line(
					cx-ca*rad*0.12,
					cy-sa*rad*0.12,
					cx+ca*rad*0.8,
					cy+sa*rad*0.8,
					d.needle,
					max(2, rad*0.035),
				)
			}
			hub := d.hub
			if hub.A == 0 {
				hub = d.needle
			}
			dc.FilledCircle(cx, cy, rad*0.08, hub)
		},
	)
}

var segmentsOf = map[rune]uint8{
	'0': 0b0111111, '1': 0b0000110, '2': 0b1011011, '3': 0b1001111, '4': 0b1100110,
	'5': 0b1101101, '6': 0b1111101, '7': 0b0000111, '8': 0b1111111, '9': 0b1101111,
	'-': 0b1000000,
}

func sevenSegment(dc *gogui.DrawContext, x, y, w, h float32, ch rune, on, off gogui.Color) {
	bits := segmentsOf[ch]
	t := w * 0.18
	g := t * 0.2
	bar := func(seg int, pts []float32) {
		c := off
		if bits&(1<<seg) != 0 {
			c = on
		}
		if c.A > 0 {
			dc.FilledPolygon(pts, c)
		}
	}
	hor := func(seg int, yy float32) {
		bar(seg, []float32{
			x + g + t/2, yy, x + t + g, yy - t/2, x + w - t - g, yy - t/2,
			x + w - g - t/2, yy, x + w - t - g, yy + t/2, x + t + g, yy + t/2,
		})
	}
	ver := func(seg int, xx, y0, y1 float32) {
		bar(seg, []float32{
			xx, y0 + g + t/2, xx + t/2, y0 + t + g, xx + t/2, y1 - t - g,
			xx, y1 - g - t/2, xx - t/2, y1 - t - g, xx - t/2, y0 + t + g,
		})
	}
	mid := y + h/2
	hor(0, y+t/2)
	ver(1, x+w-t/2, y+t/2, mid)
	ver(2, x+w-t/2, mid, y+h-t/2)
	hor(3, y+h-t/2)
	ver(4, x+t/2, mid, y+h-t/2)
	ver(5, x+t/2, y+t/2, mid)
	hor(6, mid)
}

func (f *face) segmentText(id string, r rect, text string, on, off gogui.Color) gogui.View {
	runes := []rune(text)
	return f.canvas(
		id,
		r,
		kit.Version(r.w, r.h, float32(len(runes)), float32(on.R), float32(hashText(text))),
		func(dc *gogui.DrawContext) {
			n := float32(max(len(runes), 1))
			ch := r.h
			cw := min(ch*0.55, r.w/(n*1.18))
			ch = min(ch, cw/0.55)
			total := n*cw + (n-1)*cw*0.18
			x := (r.w - total) / 2
			y := (r.h - ch) / 2
			for _, c := range runes {
				sevenSegment(dc, x, y, cw, ch, c, on, off)
				x += cw * 1.18
			}
		},
	)
}

func hashText(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

func hazard(dc *gogui.DrawContext, x, y, w, h float32, a, b gogui.Color) {
	dc.FilledRect(x, y, w, h, a)
	step := w * 0.9
	for yy := y - w; yy < y+h+w; yy += step * 2 {
		y0, y1 := max(yy, y), min(yy+step, y+h)
		if y1 <= y0 {
			continue
		}
		dc.FilledPolygon([]float32{x, yy, x + w, yy + w, x + w, yy + w + step, x, yy + step}, b)
	}
}

func screw(dc *gogui.DrawContext, cx, cy, r float32, metal, slot gogui.Color) {
	dc.FilledCircle(cx, cy, r, metal)
	dc.Circle(cx, cy, r, slot.WithOpacity(0.6), max(1, r*0.15))
	dc.Line(cx-r*0.6, cy-r*0.6, cx+r*0.6, cy+r*0.6, slot, max(1, r*0.25))
}

func nixie(
	dc *gogui.DrawContext,
	x, y, w, h float32,
	ch string,
	st gogui.TextStyle,
	lit, glass gogui.Color,
) {
	dc.FilledRoundedRect(x, y, w, h, w*0.45, glass)
	dc.RoundedRect(x, y, w, h, w*0.45, gogui.RGBA(255, 255, 255, 40), max(1, w*0.03))
	for i := 1; i < 4; i++ {
		yy := y + h*float32(i)/4
		dc.Line(x+w*0.15, yy, x+w*0.85, yy, gogui.RGBA(120, 70, 40, 50), max(1, w*0.01))
	}
	ghost := st
	ghost.Color = lit.WithOpacity(0.08)
	tw := dc.TextWidth("8", st)
	dc.Text(x+(w-tw)/2, y+(h-st.Size*1.2)/2, "8", ghost)
	tw = dc.TextWidth(ch, st)
	dc.Text(x+(w-tw)/2, y+(h-st.Size*1.2)/2, ch, st)
	dc.FilledRect(x+w*0.15, y+h-h*0.06, w*0.7, h*0.04, gogui.RGBA(60, 60, 60, 220))
}
