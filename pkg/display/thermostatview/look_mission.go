package thermostatview

import (
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/say"
)

var missionLook = look{
	back: hex(0x1c1f22),
	sheet: sheet{
		shade: rgba(0x000000, 0.6), panel: hex(0x26292d), ink: hex(0xd9d4c7),
		muted: hex(0x9c968a), accent: hex(0xffb52e), chosen: hex(0x121416),
		upper: true, radius: 0.006, edge: hex(0x3a3f45),
	},
	draw: drawMission,
}

var (
	amber      = hex(0xffb52e)
	panelLabel = hex(0x9c968a)
	panelEdge  = hex(0x3a3f45)
)

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

func segments(dc *gogui.DrawContext, cx, cy, h float32, text string, on, off gogui.Color) {
	runes := []rune(text)
	cw := h * 0.5
	gap := cw * 0.38
	total := float32(len(runes))*cw + float32(len(runes)-1)*gap
	x := cx - total/2
	for _, r := range runes {
		if r == '.' {
			dc.FilledCircle(x-gap/2, cy+h/2-cw*0.09, cw*0.09, on)
			continue
		}
		sevenSegment(dc, x, cy-h/2, cw, h, r, on, off)
		x += cw + gap
	}
}

func screwHead(dc *gogui.DrawContext, cx, cy float32) { dc.FilledCircle(cx, cy, 5, panelEdge) }

func drawMission(f *face) {
	w, h := f.rw, f.rh
	compact := f.shape == small
	m := float32(22)
	if compact {
		m = 14
	}
	if f.shape == full {
		m = 40
	}
	pad := m + 32
	if f.shape == full {
		pad = 80
	}
	title := upper(say.T("thermostat.environment")) + " · " + upper(f.name())
	if compact {
		title = "ENV · " + upper(f.name())
	}
	f.paint(func(dc *gogui.DrawContext) {
		dc.FilledRoundedRect(m, m, w-2*m, h-2*m, 10, hex(0x26292d))
		dc.RoundedRect(m, m, w-2*m, h-2*m, 10, panelEdge, 2)
		for _, p := range [][2]float32{{m + 14, m + 14}, {w - m - 14, m + 14}, {m + 14, h - m - 14}, {w - m - 14, h - m - 14}} {
			screwHead(dc, p[0], p[1])
		}
		ts := float32(16)
		ty := m + 46
		switch f.shape {
		case small:
			ts, ty = 14, m+34
		case full:
			ts, ty = 18, 96
		}
		write(dc, pad, ty, title, ts, hex(0xd9d4c7), font{bold: true, spacing: 4}, left)
	})
	set := f.setpoint()
	var thumb float64
	if set != nil {
		thumb = f.eased("thumb", *set)
	}
	readout := func(r rect, label, text string, ranged bool) {
		f.paint(func(dc *gogui.DrawContext) {
			dc.FilledRoundedRect(r.x, r.y, r.w, r.h, 6, hex(0x121416))
			dc.RoundedRect(r.x, r.y, r.w, r.h, 6, panelEdge, 1)
			if ranged {
				f.ends(dc, r.x+r.w/2, r.y+30, 14, amber, panelLabel, font{spacing: 3}, center)
			} else {
				write(dc, r.x+r.w/2, r.y+30, label, 14, panelLabel, font{spacing: 3}, center)
			}
			dh := min(r.h*0.5, r.w*0.4)
			segments(dc, r.x+r.w/2, r.y+r.h/2+14, dh, text, amber, hex(0x1f1b10))
		})
		if ranged {
			f.endTap("low", rect{r.x, r.y, r.w / 2, r.h}, Low)
			f.endTap("high", rect{r.x + r.w/2, r.y, r.w / 2, r.h}, High)
		}
	}
	slider := func(x, y, ww float32, labels bool) {
		lo, hi := f.span()
		tx := x + 22 + float32(frac(thumb, lo, hi))*(ww-44)
		f.paint(func(dc *gogui.DrawContext) {
			dc.FilledRoundedRect(x, y, ww, 44, 8, hex(0x121416))
			dc.RoundedRect(x, y, ww, 44, 8, panelEdge, 1)
			if labels {
				first := float64(int(lo/10+0.999) * 10)
				for v := first; v <= hi; v += 10 {
					xx := x + 22 + float32(frac(v, lo, hi))*(ww-44)
					dc.Line(xx, y+44, xx, y+56, hex(0x55595e), 2)
					write(dc, xx, y+74, f.format(v), 14, hex(0x6b7076), font{}, center)
				}
			}
			if set == nil {
				return
			}
			boxShadow(dc, tx-20, y-12, 40, 68, 6, 5)
			dc.FilledRoundedRect(tx-20, y-12, 40, 68, 6, hex(0xd9d4c7))
			dc.RoundedRect(tx-20, y-12, 40, 68, 6, hex(0x16181a), 2)
			for _, yy := range []float32{y + 8, y + 22, y + 36} {
				dc.Line(tx-10, yy, tx+10, yy, hex(0x8a857a), 2)
			}
		})
		f.drag(
			"slider",
			rect{x - 10, y - 20, ww + 20, 84},
			func(px, _, _, _ float32, _ float64) float64 {
				return lo + float64((px-x-22)/(ww-44))*(hi-lo)
			},
		)
	}
	toggle := func(cx, y, hh float32) {
		f.paint(func(dc *gogui.DrawContext) {
			write(dc, cx, y-10, "INC", 13, panelLabel, font{spacing: 2}, center)
			boxShadow(dc, cx-30, y, 60, hh, 10, 5)
			dc.FilledRoundedRect(cx-30, y, 60, hh, 10, hex(0x16181a))
			dc.RoundedRect(cx-30, y, 60, hh, 10, panelEdge, 1)
			dc.FilledRoundedRect(cx-10, y+hh*0.18, 20, hh*0.36, 8, hex(0x9aa0a6))
			dc.FilledCircle(cx, y+hh*0.2, 12, hex(0xe6e1d5))
			write(dc, cx, y+hh+22, "DEC", 13, panelLabel, font{spacing: 2}, center)
		})
		f.step("inc", rect{cx - 34, y - 30, 68, hh/2 + 30}, 1)
		f.step("dec", rect{cx - 34, y + hh/2, 68, hh/2 + 34}, -1)
	}
	lamps := func(x, y, bw, gap float32) {
		type lampSpec struct {
			word string
			on   bool
		}
		list := []lampSpec{
			{upper(say.T("thermostat.action.heating")), f.doing() == heating},
			{upper(say.T("thermostat.action.cooling")), f.doing() == cooling},
			{upper(say.T("thermostat.lamp.fan")), f.working() || f.st.Action == "fan"},
		}
		lit := 0.75 + 0.25*f.breath(2400*time.Millisecond)
		for i, l := range list {
			bx := x + float32(i)*(bw+gap)
			fanLamp := i == 2
			f.paint(func(dc *gogui.DrawContext) {
				fill, ink := hex(0x2b2f33), hex(0x6b7076)
				if l.on && fanLamp {
					fill, ink = hex(0x3b4a2a), hex(0xb8e07a)
				}
				dc.FilledRoundedRect(bx, y, bw, 46, 4, fill)
				if !l.on || fanLamp {
					write(dc, bx+bw/2, y+29, l.word, 14, ink, font{bold: true, spacing: 2}, center)
				}
			})
			if l.on && !fanLamp {
				f.motion(func(dc *gogui.DrawContext) {
					dc.FilledRoundedRect(
						bx-4,
						y-4,
						bw+8,
						54,
						7,
						hex(0xff8a1e).WithOpacity(0.18*lit),
					)
					dc.FilledRoundedRect(bx, y, bw, 46, 4, hex(0xff8a1e).WithOpacity(lit))
					write(
						dc,
						bx+bw/2,
						y+29,
						l.word,
						14,
						hex(0x2a1600),
						font{bold: true, spacing: 2},
						center,
					)
				})
			}
		}
	}
	readings := func(x, y float32, size float32, gap float32) {
		f.paint(func(dc *gogui.DrawContext) {
			if out := f.outside(); out != "" {
				write(
					dc,
					x,
					y,
					upper(say.T("thermostat.outside")),
					15,
					panelLabel,
					font{spacing: 3},
					left,
				)
				write(dc, x, y+size*1.1, out, size, amber, font{bold: true}, left)
			}
			if hum := f.humidity(); hum != "" {
				write(
					dc,
					x+gap,
					y,
					upper(say.T("thermostat.humidity")),
					15,
					panelLabel,
					font{spacing: 3},
					left,
				)
				write(dc, x+gap, y+size*1.1, hum, size, amber, font{bold: true}, left)
			}
		})
	}
	in := f.number(f.st.Current)
	sp := f.number(set)
	ranged := f.st.Ranged()
	switch f.shape {
	case full:
		readout(rect{80, 124, 340, 230}, upper(say.T("thermostat.inside")), in, false)
		readout(rect{440, 124, 340, 230}, upper(say.T("thermostat.setpoint")), sp, ranged)
		toggle(841, 150, 180)
		slider(80, 390, 700, true)
		readings(80, 530, 48, 220)
		lamps(80, 640, 160, 16)
		f.knobColumn(rect{920, 124, 280, 610}, 40)
	case tall:
		readout(rect{pad, 96, 250, 190}, upper(say.T("thermostat.inside")), in, false)
		readout(rect{pad + 270, 96, 250, 190}, upper(say.T("thermostat.setpoint")), sp, ranged)
		slider(pad, 330, w-2*pad-90, true)
		toggle(w-pad-40, 300, 120)
		readings(pad, 450, 42, 180)
		on := f.working()
		lit := 0.75 + 0.25*f.breath(2400*time.Millisecond)
		word := upper(f.action())
		f.paint(func(dc *gogui.DrawContext) {
			if !on {
				dc.FilledRoundedRect(w-pad-150, 450, 150, 46, 4, hex(0x2b2f33))
				write(
					dc,
					w-pad-75,
					479,
					word,
					14,
					hex(0x6b7076),
					font{bold: true, spacing: 2},
					center,
				)
			}
		})
		if on {
			f.motion(func(dc *gogui.DrawContext) {
				dc.FilledRoundedRect(w-pad-150, 450, 150, 46, 4, hex(0xff8a1e).WithOpacity(lit))
				write(
					dc,
					w-pad-75,
					479,
					word,
					14,
					hex(0x2a1600),
					font{bold: true, spacing: 2},
					center,
				)
			})
		}
		f.knobRow(rect{pad, 530, w - 2*pad, h - 530 - m - 24}, 44)
	case mid:
		readout(rect{pad, 92, 200, 170}, upper(say.T("thermostat.inside")), in, false)
		readout(rect{pad + 216, 92, 200, 170}, upper(say.T("thermostat.setpoint")), sp, ranged)
		slider(pad, 312, 416, true)
		readings(pad, 432, 40, 170)
		lamps(pad, h-m-76, 130, 14)
		toggle(pad+452, 120, 130)
		f.knobColumn(rect{w - pad - 186, 96, 186 + pad - m - 18, h - m - 26 - 96}, 32)
	default:
		rw := float32(130)
		if w > 700 {
			rw = 160
		}
		readout(rect{pad, 70, rw, 150}, upper(say.T("thermostat.inside")), in, false)
		readout(rect{pad + rw + 14, 70, rw, 150}, upper(say.T("thermostat.setpoint")), sp, ranged)
		slider(pad, h-m-96, 2*rw+14, true)
		toggle(pad+2*rw+14+44, 92, 104)
		kw := float32(186)
		if w > 700 {
			kw = 200
		}
		f.knobColumn(rect{w - pad - kw, 56, kw + pad - m - 18, h - m - 16 - 56}, 18)
	}
}

var missionTurn = []float64{0, 60, 120, -60, -120, 180}

func (f *face) missionKnob(dc *gogui.DrawContext, cx, cy, r float32, turn float64) {
	shadow(dc, cx, cy, r, 5)
	dc.FilledCircleGradient(cx, cy, r, radial(cx, cy, r*1.4, cx-r*0.2, cy-r*0.3, stops(
		0.0, hex(0x6a7078), 1.0, hex(0x22262a))))
	dc.Circle(cx, cy, r, hex(0x111111), 1)
	x0, y0 := polar(cx, cy, r*0.3, turn)
	x1, y1 := polar(cx, cy, r*0.86, turn)
	dc.Line(x0, y0, x1, y1, hex(0xe6e1d5), max(4, r*0.16))
}

func (f *face) knobTurn(kind pick) float64 {
	for j, o := range options(f.st, kind) {
		if o.current {
			return missionTurn[j%len(missionTurn)]
		}
	}
	return 0
}

func (f *face) knobColumn(r rect, kr float32) {
	kinds := f.kinds()
	rowH := r.h / float32(len(kinds))
	f.paint(func(dc *gogui.DrawContext) {
		dc.FilledRoundedRect(r.x, r.y, r.w, r.h, 6, hex(0x1f2226))
		dc.RoundedRect(r.x, r.y, r.w, r.h, 6, panelEdge, 1)
	})
	for i, kind := range kinds {
		top := r.y + float32(i)*rowH
		cx, cy := r.x+18+kr, top+rowH*0.62
		title, val, turn := upper(kind.title()), upper(f.pickValue(kind)), f.knobTurn(kind)
		f.paint(func(dc *gogui.DrawContext) {
			write(dc, r.x+18, cy-kr-12, title, 14, panelLabel, font{spacing: 3}, left)
			f.missionKnob(dc, cx, cy, kr, turn)
			write(dc, cx+kr+14, cy+6, val, 15, amber, font{bold: true, spacing: 2}, left)
		})
		f.chooser("mknob-"+title, rect{r.x, top, r.w, rowH}, kind)
	}
}

func (f *face) knobRow(r rect, kr float32) {
	kinds := f.kinds()
	kw := r.w / float32(len(kinds))
	f.paint(func(dc *gogui.DrawContext) {
		dc.FilledRoundedRect(r.x, r.y, r.w, r.h, 6, hex(0x1f2226))
		dc.RoundedRect(r.x, r.y, r.w, r.h, 6, panelEdge, 1)
	})
	for i, kind := range kinds {
		cx := r.x + kw*(float32(i)+0.5)
		cy := r.y + r.h*0.5
		title, val, turn := upper(kind.title()), upper(f.pickValue(kind)), f.knobTurn(kind)
		f.paint(func(dc *gogui.DrawContext) {
			write(dc, cx, cy-kr-22, title, 14, panelLabel, font{spacing: 3}, center)
			f.missionKnob(dc, cx, cy, kr, turn)
			write(dc, cx, cy+kr+32, val, 15, amber, font{bold: true, spacing: 2}, center)
		})
		f.chooser("mknob-"+title, rect{cx - kw/2, r.y, kw, r.h}, kind)
	}
}
