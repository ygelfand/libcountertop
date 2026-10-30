package thermostatview

import (
	"math"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/say"
)

var brassLook = look{
	back: hex(0x2c1b10),
	sheet: sheet{
		shade: rgba(0x120a04, 0.6), panel: hex(0xd9b062), ink: hex(0x2a1a08),
		muted: hex(0x5c3e12), accent: hex(0x2a1a08), chosen: hex(0xf0d08a),
		family: style.SerifFamily, upper: true, radius: 0.012, edge: hex(0x5c3e12),
	},
	draw: drawBrass,
}

var (
	brassDark = hex(0x3a2410)
	brassInk  = hex(0x2a1a08)
	brassLine = hex(0x5c3e12)
)

func stops(pairs ...any) []gogui.GradientStop {
	out := make([]gogui.GradientStop, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(
			out,
			gogui.GradientStop{Pos: float32(pairs[i].(float64)), Color: pairs[i+1].(gogui.Color)},
		)
	}
	return out
}

func radial(cx, cy, r, fx, fy float32, s []gogui.GradientStop) *gogui.CanvasGradient {
	return &gogui.CanvasGradient{Radial: true, CX: cx, CY: cy, R: r, FX: fx, FY: fy, Stops: s}
}

func shadow(dc *gogui.DrawContext, cx, cy, r, dy float32) {
	for i := 4; i >= 1; i-- {
		k := float32(i) / 4
		dc.FilledCircle(cx+dy*0.15, cy+dy, r+dy*0.5*k, gogui.Black.WithOpacity(0.09))
	}
}

func boxShadow(dc *gogui.DrawContext, x, y, w, h, radius, dy float32) {
	for i := 4; i >= 1; i-- {
		g := dy * 0.5 * float32(i) / 4
		dc.FilledRoundedRect(x-g, y+dy-g, w+2*g, h+2*g, radius+g, gogui.Black.WithOpacity(0.09))
	}
}

func brassCap(dc *gogui.DrawContext, cx, cy, r float32) {
	dc.FilledCircleGradient(cx, cy, r, radial(cx, cy, r*1.4, cx-r*0.3, cy-r*0.4, stops(
		0.0, hex(0xfff2c4), 0.5, hex(0xc8963f), 1.0, hex(0x5c3e12))))
}

func wood(dc *gogui.DrawContext, w, h float32) {
	dc.FilledRectGradient(
		0,
		0,
		w,
		h,
		&gogui.CanvasGradient{X1: 0, Y1: 0, X2: w, Y2: h, Stops: stops(
			0.0, hex(0x3b2416), 1.0, hex(0x1f120a))},
	)
	for y := float32(4); y < h; y += 14 {
		pts := make([]float32, 0, 34)
		for i := 0; i <= 16; i++ {
			x := w * float32(i) / 16
			pts = append(pts, x, y+2.5*float32(math.Sin(float64(x/w*7+y*0.13))))
		}
		dc.Polyline(pts, gogui.Black.WithOpacity(0.16), 2)
	}
}

func plate(dc *gogui.DrawContext, x, y, w, h float32) {
	boxShadow(dc, x, y, w, h, 12, 12)
	dc.FilledRoundedRectGradient(
		x,
		y,
		w,
		h,
		12,
		&gogui.CanvasGradient{X1: 0, Y1: y, X2: 0, Y2: y + h, Stops: stops(
			0.0, hex(0xe8c06a), 1.0, hex(0x9a6d27))},
	)
	dc.RoundedRect(x, y, w, h, 12, brassLine, 1.5)
	for _, p := range [][2]float32{{x + 16, y + 16}, {x + w - 16, y + 16}, {x + 16, y + h - 16}, {x + w - 16, y + h - 16}} {
		dc.FilledCircle(p[0], p[1], 5, brassLine)
	}
}

func brassAngle(v, lo, hi float64) float64 { return -135 + frac(v, lo, hi)*270 }

func drawBrass(f *face) {
	w, h := f.rw, f.rh
	serif := font{family: style.SerifFamily, spacing: 3}
	f.paint(func(dc *gogui.DrawContext) { wood(dc, w, h) })
	readings := f.outside()
	if hum := f.humidity(); hum != "" {
		if readings != "" {
			readings += " · "
		}
		readings += hum
	}
	switch f.shape {
	case full:
		f.gauge(480, 400, 330, 1)
		f.paint(func(dc *gogui.DrawContext) {
			if hum := f.humidity(); hum != "" {
				miniGauge(
					dc,
					960,
					170,
					92,
					hum,
					upper(say.T("thermostat.humidity")),
					f.st.Humidity,
					0,
					100,
				)
			}
			if out := f.outside(); out != "" {
				miniGauge(dc, 1150, 170, 92, out, upper(say.T("thermostat.outside")), nil, 0, 100)
			}
			plate(dc, 872, 310, 376, 420)
			write(
				dc,
				1060,
				362,
				upper(f.name()),
				20,
				brassDark,
				font{family: style.SerifFamily, spacing: 6},
				center,
			)
			dc.Line(904, 382, 1216, 382, brassLine.WithOpacity(0.6), 1)
			dc.Line(904, 626, 1216, 626, brassLine.WithOpacity(0.6), 1)
		})
		f.knobs(rect{872, 400, 376, 200}, false, 1)
		f.lamp(1060, 676, 15)
	case tall:
		f.gauge(w/2, 262, 228, 0.74)
		px, py, pw, ph := float32(30), float32(520), w-60, h-544
		f.paint(func(dc *gogui.DrawContext) {
			plate(dc, px, py, pw, ph)
			write(
				dc,
				px+30,
				py+44,
				upper(f.name()),
				18,
				brassDark,
				font{family: style.SerifFamily, spacing: 5},
				left,
			)
			write(dc, px+pw-30, py+44, readings, 18, brassDark, serif, right)
			dc.Line(px+24, py+60, px+pw-24, py+60, brassLine.WithOpacity(0.6), 1)
		})
		f.knobs(rect{px, py + 74, pw, ph - 74 - 46}, false, 0.62)
		f.lamp(px+pw/2, py+ph-18, 14)
	case mid:
		f.gauge(270, h/2+6, 250, 0.8)
		px, py, pw, ph := float32(540), float32(30), w-570, h-60
		f.paint(func(dc *gogui.DrawContext) {
			plate(dc, px, py, pw, ph)
			write(
				dc,
				px+pw/2,
				py+48,
				upper(f.name()),
				16,
				brassDark,
				font{family: style.SerifFamily, spacing: 4},
				center,
			)
			write(dc, px+pw/2, py+76, readings, 16, brassLine, serif, center)
			dc.Line(px+20, py+92, px+pw-20, py+92, brassLine.WithOpacity(0.6), 1)
		})
		f.knobs(rect{px, py + 96, pw, ph - 150}, true, 0.66)
		f.lamp(px+pw/2, py+ph-26, 14)
	default:
		r := h/2 - 16
		f.gauge(r+22, h/2+2, r, r/330)
		px := 2*r + 46
		pw := w - px - 18
		f.paint(func(dc *gogui.DrawContext) {
			plate(dc, px, 18, pw, h-36)
			write(
				dc,
				px+pw/2,
				52,
				upper(f.name()),
				15,
				brassDark,
				font{family: style.SerifFamily, spacing: 3},
				center,
			)
		})
		f.knobs(rect{px + 8, 62, pw - 16, h - 36 - 44 - 52}, true, 0.5)
		f.lamp(px+pw/2, h-34, 14)
	}
}

func miniGauge(
	dc *gogui.DrawContext,
	cx, cy, r float32,
	text, label string,
	v *float64,
	lo, hi float64,
) {
	shadow(dc, cx, cy, r, 14)
	bezel(dc, cx, cy, r)
	fr := r * 0.85
	gaugeFace(dc, cx, cy, fr)
	write(dc, cx, cy+40*(r/92), text, 34*(r/92), brassDark, font{family: style.SerifFamily}, center)
	write(
		dc,
		cx,
		cy+64*(r/92),
		label,
		12*(r/92),
		hex(0x6b5328),
		font{family: style.SerifFamily, spacing: 3},
		center,
	)
	a := -60.0
	if v != nil {
		a = brassAngle(*v, lo, hi)
	}
	needle(dc, cx, cy, fr*0.75, a, 2*(r/92))
	brassCap(dc, cx, cy, 9*(r/92))
	sheen(dc, cx, cy, fr)
}

func bezel(dc *gogui.DrawContext, cx, cy, r float32) {
	dc.FilledCircleGradient(cx, cy, r, radial(cx, cy, r*1.6, cx-r*0.3, cy-r*0.44, stops(
		0.0, hex(0xfbe7ad), 0.35, hex(0xd4a54f), 0.7, hex(0x9a6d27), 1.0, hex(0x5c3e12))))
}

func gaugeFace(dc *gogui.DrawContext, cx, cy, r float32) {
	dc.FilledCircleGradient(cx, cy, r, radial(cx, cy, r*1.44, cx-r*0.1, cy-r*0.24, stops(
		0.0, hex(0xfdf8ea), 0.7, hex(0xeee2c2), 1.0, hex(0xd9c79d))))
	dc.Circle(cx, cy, r-r*0.02, hex(0x2a1a06).WithOpacity(0.25), r*0.04)
}

func sheen(dc *gogui.DrawContext, cx, cy, r float32) {
	dc.FilledCircleGradient(
		cx-r*0.3,
		cy-r*0.45,
		r*0.62,
		radial(cx-r*0.3, cy-r*0.45, r*0.62, cx-r*0.3, cy-r*0.45, stops(
			0.0, gogui.White.WithOpacity(0.38), 1.0, gogui.White.WithOpacity(0))),
	)
}

func needle(dc *gogui.DrawContext, cx, cy, length float32, deg float64, width float32) {
	x, y := polar(cx, cy, length, deg)
	bx, by := polar(cx, cy, length*0.12, deg+180)
	dc.Line(bx+2, by+4, x+2, y+4, gogui.Black.WithOpacity(0.25), width*2)
	dc.Line(bx, by, x, y, hex(0x15100a), width*2)
}

func (f *face) gauge(cx, cy, R, k float32) {
	lo, hi := f.span()
	set := f.setpoint()
	var shown float64
	if set != nil {
		shown = f.springy("set", *set)
	}
	var inside float64
	if f.st.Current != nil {
		inside = f.springy("inside", *f.st.Current)
	}
	sk := max(k, 0.7)
	fr := R * 0.885
	pw, ph := 152*sk, 46*sk
	py := cy + fr*0.69
	br := 26 * sk
	f.paint(func(dc *gogui.DrawContext) {
		shadow(dc, cx, cy, R, 18*k)
		bezel(dc, cx, cy, R)
		dc.Circle(cx, cy, R*0.91, hex(0xf6d892), max(4, R*0.03))
		dc.Circle(
			cx,
			cy,
			R*0.91+max(4, R*0.03)*0.4,
			hex(0x4a300c).WithOpacity(0.5),
			max(1.5, R*0.01),
		)
		gaugeFace(dc, cx, cy, fr)
		step := 1.0
		if k < 0.6 {
			step = 2
		}
		if hi-lo > 60 {
			step *= 2
		}
		for v := lo; v <= hi+0.01; v += step {
			iv := int(math.Round(v))
			long := iv%10 == 0
			r1 := fr * 0.86
			if iv%5 == 0 {
				r1 = fr * 0.82
			}
			if long {
				r1 = fr * 0.8
			}
			a := brassAngle(v, lo, hi)
			x0, y0 := polar(cx, cy, r1, a)
			x1, y1 := polar(cx, cy, fr*0.93, a)
			dc.Line(x0, y0+1.5, x1, y1+1.5, gogui.White.WithOpacity(0.7), 2)
			dc.Line(x0, y0, x1, y1, hex(0x3a2a14), 2.2)
		}
		lf := max(14, 28*k)
		first := math.Ceil(lo/10) * 10
		for v := first; v <= hi; v += 10 {
			x, y := polar(cx, cy, fr*0.64, brassAngle(v, lo, hi))
			txt := f.format(v)
			write(
				dc,
				x,
				y+lf*0.35+1.5,
				txt,
				lf,
				gogui.White.WithOpacity(0.7),
				font{family: style.SerifFamily},
				center,
			)
			write(dc, x, y+lf*0.35, txt, lf, hex(0x3a2a14), font{family: style.SerifFamily}, center)
		}
		write(
			dc,
			cx,
			cy+fr*0.45,
			f.number(f.st.Current)+"°",
			max(14, 54*k),
			hex(0x3a2a14),
			font{family: style.SerifFamily},
			center,
		)
		if k >= 0.75 {
			write(
				dc,
				cx,
				cy+fr*0.45+32*k,
				upper(say.T("thermostat.inside")),
				max(14, 17*k),
				hex(0x6b5328),
				font{family: style.SerifFamily, spacing: 5},
				center,
			)
		}
		boxShadow(dc, cx-pw/2, py, pw, ph, 6, 5)
		dc.FilledRoundedRectGradient(
			cx-pw/2,
			py,
			pw,
			ph,
			6,
			&gogui.CanvasGradient{X1: 0, Y1: py, X2: 0, Y2: py + ph, Stops: stops(
				0.0, hex(0xe8c06a), 1.0, hex(0x9a6d27))},
		)
		dc.RoundedRect(cx-pw/2, py, pw, ph, 6, brassLine, 1)
		label := upper(say.T("thermostat.set")) + " " + f.number(set) + "°"
		if f.st.Ranged() {
			label = f.number(f.st.Low) + "–" + f.number(f.st.High) + "°"
		}
		write(
			dc,
			cx,
			py+ph*0.7,
			label,
			max(14, 22*sk),
			brassDark,
			font{family: style.SerifFamily, spacing: 3},
			center,
		)
		for i, sym := range []string{"−", "+"} {
			bx := cx + (pw/2+br*1.9)*float32(2*i-1)
			shadow(dc, bx, py+ph/2, br, 5)
			brassCap(dc, bx, py+ph/2, br)
			dc.Circle(bx, py+ph/2, br, brassDark, 2)
			write(
				dc,
				bx,
				py+ph/2+br*0.4,
				sym,
				br*1.15,
				brassInk,
				font{family: style.SerifFamily},
				center,
			)
		}
		if f.st.Current != nil {
			ni := brassAngle(inside, lo, hi)
			nx, ny := polar(cx, cy, fr*0.8, ni)
			dc.Line(cx+3, cy+6, nx+3, ny+6, gogui.Black.WithOpacity(0.25), max(3, 8*k))
			dc.FilledPolygon(needleShape(cx, cy, fr*0.8, ni, 6*k), hex(0x15100a))
		}
		brassCap(dc, cx, cy, 22*k)
		dc.Circle(cx, cy, 22*k, brassDark, 1.5)
		pointer := func(v float64, c gogui.Color) {
			a := brassAngle(v, lo, hi)
			tx, ty := polar(cx, cy, R*0.89, a)
			lx, ly := polar(cx, cy, R*0.97, a-3.2*float64(330/R))
			rx, ry := polar(cx, cy, R*0.97, a+3.2*float64(330/R))
			dc.FilledPolygon(
				[]float32{tx + 2, ty + 4, lx + 2, ly + 4, rx + 2, ry + 4},
				gogui.Black.WithOpacity(0.3),
			)
			dc.FilledPolygon([]float32{tx, ty, lx, ly, rx, ry}, c)
		}
		if f.st.Ranged() {
			other := *f.st.High
			if f.v.end(f.st.Entity) == High {
				other = *f.st.Low
			}
			pointer(other, hex(0x6d5a3a))
		}
		if set != nil && f.st.Mode != "off" {
			pointer(shown, hex(0xb3311f))
		}
		sheen(dc, cx, cy, fr)
	})
	if set != nil {
		f.drag(
			"bezel",
			rect{cx - R, cy - R, 2 * R, 2 * R},
			func(x, y, _, _ float32, _ float64) float64 {
				deg := math.Atan2(float64(x-cx), float64(cy-y)) * 180 / math.Pi
				deg = min(max(deg, -135), 135)
				return lo + (deg+135)/270*(hi-lo)
			},
		)
	}
	for i := range 2 {
		bx := cx + (pw/2+br*1.9)*float32(2*i-1)
		f.step(
			"brass"+[]string{"-", "+"}[i],
			rect{bx - br*1.3, py + ph/2 - br*1.3, br * 2.6, br * 2.6},
			2*i-1,
		)
	}
	if f.st.Ranged() {
		f.endTap("low", rect{cx - pw/2, py, pw / 2, ph}, Low)
		f.endTap("high", rect{cx, py, pw / 2, ph}, High)
	}
}

func needleShape(cx, cy, length float32, deg float64, half float32) []float32 {
	tx, ty := polar(cx, cy, length, deg)
	lx, ly := polar(cx, cy, half, deg-90)
	rx, ry := polar(cx, cy, half, deg+90)
	tlx, tly := polar(tx, ty, 2, deg-90)
	trx, try := polar(tx, ty, 2, deg+90)
	return []float32{lx, ly, tlx, tly, trx, try, rx, ry}
}

func (f *face) springy(name string, v float64) float64 {
	shown := f.eased(name, v)
	if shown == v {
		return v
	}
	over := (v - shown) * 0.12 * math.Sin(float64(f.now.UnixMilli()%400)/400*2*math.Pi)
	return shown + over
}

var knobTurn = []float64{-35, 0, 35, 70, -70, 105}

func (f *face) knobs(r rect, vertical bool, k float32) {
	kr := 46 * k
	kinds := f.kinds()
	n := float32(len(kinds))
	for i, kind := range kinds {
		var cx, cy, lx, ly float32
		var a anchor
		if vertical {
			cx, cy = r.x+r.w*0.38, r.y+r.h*(float32(i)+0.5)/n
			lx, ly, a = cx+kr+20*k, cy-4, left
		} else {
			cx, cy = r.x+r.w*(float32(i)+0.5)/n, r.y+r.h*0.5
			lx, ly, a = cx, cy-kr-18*k, center
		}
		opts := options(f.st, kind)
		turn := 0.0
		for j, o := range opts {
			if o.current {
				turn = knobTurn[j%len(knobTurn)]
			}
		}
		val := upper(f.pickValue(kind))
		title := upper(kind.title())
		lf, vf := max(14, 14*k), max(14, 18*k)
		f.paint(func(dc *gogui.DrawContext) {
			shadow(dc, cx, cy, kr, 6)
			brassCap(dc, cx, cy, kr)
			dc.Circle(cx, cy, kr, brassDark, 2)
			dc.Circle(cx, cy, kr*0.78, brassLine.WithOpacity(0.55), 1.5)
			x0, y0 := polar(cx, cy, kr*0.13, turn)
			x1, y1 := polar(cx, cy, kr*0.87, turn)
			dc.Line(x0, y0, x1, y1, brassInk, 8*k)
			write(dc, lx, ly, title, lf, brassLine, font{family: style.SerifFamily, spacing: 3}, a)
			if vertical {
				write(
					dc,
					lx,
					ly+vf+6,
					val,
					vf,
					brassInk,
					font{family: style.SerifFamily, bold: true, spacing: 2},
					a,
				)
			} else {
				write(
					dc,
					cx,
					cy+kr+vf+10*k,
					val,
					vf,
					brassInk,
					font{family: style.SerifFamily, bold: true, spacing: 2},
					center,
				)
			}
		})
		f.chooser("knob-"+title, rect{cx - kr*1.3, cy - kr*1.3, kr * 2.6, kr * 2.6}, kind)
	}
}

func (f *face) lamp(cx, y, size float32) {
	if !f.working() {
		return
	}
	col := hex(0xff8a3d)
	word := upper(say.T("thermostat.action.heating"))
	if f.doing() == cooling {
		col, word = hex(0x6fb6ff), upper(say.T("thermostat.action.cooling"))
	}
	lit := 0.6 + 0.4*f.breath(2600*time.Millisecond)
	f.paint(func(dc *gogui.DrawContext) {
		tw := measure(dc, word, size, font{family: style.SerifFamily, spacing: 3})
		write(
			dc,
			cx+10,
			y,
			word,
			size,
			brassLine,
			font{family: style.SerifFamily, spacing: 3},
			center,
		)
		dc.Circle(cx-tw/2-6, y-size*0.35, 8, brassLine, 1.5)
	})
	f.motion(func(dc *gogui.DrawContext) {
		tw := measure(dc, word, size, font{family: style.SerifFamily, spacing: 3})
		x := cx - tw/2 - 6
		dc.FilledCircle(x, y-size*0.35, 14, col.WithOpacity(0.18*lit))
		dc.FilledCircle(x, y-size*0.35, 7, col.WithOpacity(lit))
	})
}
