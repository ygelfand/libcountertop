package thermostatview

import (
	"math"
	"strconv"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/thermostat"
	"github.com/ygelfand/libcountertop/pkg/say"
)

var barsLook = look{
	back: hex(0x121212),
	sheet: sheet{
		shade: rgba(0x000000, 0.65), panel: hex(0x1c1c1c), ink: hex(0xf2f2f2),
		muted: hex(0x9a9a9a), accent: hex(0x4fa8ff), chosen: hex(0x2a2a2a),
		upper: true, radius: 0.012, edge: hex(0x2a2a2a),
	},
	draw: drawBars,
}

func drawBars(f *face) {
	w, h := f.rw, f.rh
	tone := hex(0x8a8a8a)
	switch f.doing() {
	case heating:
		tone = hex(0xff8a3d)
	case cooling:
		tone = hex(0x4fa8ff)
	}
	compact := f.shape == small
	pad, nameY, nameSize := float32(40), float32(64), float32(28)
	switch f.shape {
	case full:
		pad, nameY, nameSize = 80, 100, 30
	case small:
		pad, nameY, nameSize = 26, 44, 22
	}
	df := float32(18)
	if compact {
		df = 16
	}
	type drop struct {
		kind  pick
		text  string
		color gogui.Color
		bold  bool
	}
	var drops []drop
	for _, kind := range f.kinds() {
		switch kind {
		case pickMode:
			drops = append(drops, drop{kind, upper(f.modeName()), tone, true})
		case pickPreset:
			text := upper(say.T("thermostat.no-preset"))
			if f.st.Preset != "" && f.st.Preset != "none" {
				text = upper(f.presetName())
			}
			drops = append(drops, drop{kind, text, hex(0xd9d9d9), false})
		case pickFan:
			drops = append(
				drops,
				drop{
					kind,
					upper(say.T("thermostat.fan") + " " + f.fanName()),
					hex(0xd9d9d9),
					false,
				},
			)
		}
	}
	textW := func(s string, bold bool) float32 {
		k := float32(0.66)
		if bold {
			k = 0.72
		}
		return float32(len([]rune(s))) * df * k
	}
	total := float32(0)
	for _, d := range drops {
		total += textW(d.text, d.bold) + 20 + 22
	}
	inline := f.shape == full || f.shape == mid
	dx, dy, lineY := pad, nameY+44, nameY+60
	if compact {
		dy, lineY = nameY+34, nameY+50
	}
	if inline {
		dx, dy, lineY = w-pad-total+22, nameY, nameY+28
	}
	var (
		big, nY, stripY, stripH float32
	)
	switch f.shape {
	case full:
		big, nY, stripY, stripH = 150, 396, 520, 120
	case tall:
		big, nY, stripY, stripH = 130, 300, 470, 160
	case mid:
		big, nY, stripY, stripH = 130, 270, 380, 150
	default:
		big, nY, stripY, stripH = 84, lineY+108, h-124, 52
	}
	lf := float32(16)
	if compact {
		lf = 14
	}
	light := font{family: style.LightFamily}
	f.paint(func(dc *gogui.DrawContext) {
		write(dc, pad, nameY, f.name(), nameSize, hex(0xf2f2f2), font{bold: true}, left)
		x := dx
		for _, d := range drops {
			tw := write(dc, x, dy, d.text, df, d.color, font{bold: d.bold}, left)
			c := hex(0x8a8a8a)
			if d.bold {
				c = d.color
			}
			chevronDown(dc, x+tw+6, dy-df*0.42, 10, c)
			x += tw + 20 + 22
		}
		dc.Line(pad, lineY, w-pad, lineY, hex(0x262626), 2)
		if f.st.Ranged() {
			f.ends(dc, pad, nY-big*0.8, lf, tone, hex(0x666666), font{spacing: 2}, left)
		} else {
			write(
				dc,
				pad,
				nY-big*0.8,
				upper(say.T("thermostat.set")),
				lf,
				hex(0x9a9a9a),
				font{spacing: 2},
				left,
			)
		}
		degrees(dc, pad-4, nY, f.number(f.setpoint()), big, gogui.White, hex(0x9a9a9a), light, left)
		ix := pad + big*1.5
		write(
			dc,
			ix,
			nY-big*0.8,
			upper(say.T("thermostat.inside")),
			lf,
			hex(0x9a9a9a),
			font{spacing: 2},
			left,
		)
		degrees(
			dc,
			ix-4,
			nY,
			f.number(f.st.Current),
			big,
			hex(0x777777),
			hex(0x666666),
			light,
			left,
		)
		out, hum := f.outside(), f.humidity()
		ox := ix + big*1.4
		switch {
		case !compact && ox+220 < w:
			if out != "" {
				write(
					dc,
					ox,
					nY-big*0.8,
					upper(say.T("thermostat.outside")),
					lf,
					hex(0x9a9a9a),
					font{spacing: 2},
					left,
				)
				write(dc, ox, nY-big*0.8+46, out, 36, hex(0xe6e6e6), light, left)
			}
			if hum != "" {
				write(
					dc,
					ox,
					nY-big*0.8+100,
					upper(say.T("thermostat.humidity")),
					lf,
					hex(0x9a9a9a),
					font{spacing: 2},
					left,
				)
				write(dc, ox, nY-big*0.8+146, hum, 36, hex(0xe6e6e6), light, left)
			}
		case !compact:
			ry := nY + 44
			if out != "" {
				icon(dc, pad, ry-7, "mdi:weather-sunny", 22, hex(0x8a8a8a), left)
				write(dc, pad+30, ry, out, 20, hex(0xd9d9d9), font{}, left)
			}
			if hum != "" {
				icon(dc, pad+100, ry-7, "mdi:water-outline", 22, hex(0x8a8a8a), left)
				write(dc, pad+130, ry, hum, 20, hex(0xd9d9d9), font{}, left)
			}
		default:
			rx := w - pad
			if out != "" {
				write(dc, rx, nY-big*0.5, out, 20, hex(0xd9d9d9), font{}, right)
				icon(dc, rx-74, nY-big*0.5-7, "mdi:weather-sunny", 22, hex(0x8a8a8a), left)
			}
			if hum != "" {
				write(dc, rx, nY-big*0.5+34, hum, 20, hex(0xd9d9d9), font{}, right)
				icon(dc, rx-74, nY-big*0.5+27, "mdi:water-outline", 22, hex(0x8a8a8a), left)
			}
		}
	})
	x := dx
	for _, d := range drops {
		tw := textW(d.text, d.bold) + 20
		f.chooser("drop-"+d.kind.title(), rect{x - 8, dy - df - 10, tw + 16, df + 24}, d.kind)
		x += tw + 22
	}
	if f.st.Ranged() {
		f.endTap("low", rect{pad - 6, nY - big*0.8 - lf - 8, 64, lf + 16}, Low)
		f.endTap("high", rect{pad + 58, nY - big*0.8 - lf - 8, 70, lf + 16}, High)
	}
	f.barStrip(pad, stripY, stripH, w, tone, compact)
}

func (f *face) barStrip(pad, stripY, stripH, w float32, tone gogui.Color, compact bool) {
	pitch, bw := float32(26), float32(16)
	if compact {
		pitch, bw = 20, 12
	}
	ext, labelSize := float32(20), float32(16)
	if compact {
		ext, labelSize = 14, 14
	}
	step := f.st.Step
	if step <= 0 {
		step = 1
	}
	if step < 1 {
		step = 1
	}
	x0, x1 := pad+34, w-pad-34
	n := int((x1 - x0 + (pitch - bw)) / pitch)
	lo, hi := f.span()
	set := f.setpoint()
	in := f.st.Current
	middle := (lo + hi) / 2
	if set != nil && in != nil {
		middle = (*set + *in) / 2
	} else if set != nil {
		middle = *set
	}
	first := math.Round(middle - float64(n-1)/2*step)
	first = max(lo, min(hi-float64(n-1)*step, first))
	used := float32(n)*pitch - (pitch - bw)
	xs := x0 + (x1-x0-used)/2
	var ring float64
	if set != nil {
		ring = f.eased("ring", *set)
	}
	goal := f.goal(ring)
	f.paint(func(dc *gogui.DrawContext) {
		for i := range n {
			v := first + float64(i)*step
			bx := xs + float32(i)*pitch
			on := false
			if goal != nil && in != nil && f.working() {
				a, b := min(*goal, *in), max(*goal, *in)
				on = v >= a-0.01 && v <= b+0.01
			}
			c := hex(0x2a2a2a)
			if on {
				c = tone
			}
			dc.FilledRoundedRect(bx, stripY, bw, stripH, bw/2, c)
			iv := int(math.Round(v))
			if math.Abs(v-float64(iv)) < 0.01 && (iv%10 == 0 || n < 32 && iv%5 == 0) {
				write(
					dc,
					bx+bw/2,
					stripY+stripH+ext+36,
					strconv.Itoa(iv),
					labelSize,
					hex(0x666666),
					font{},
					center,
				)
			}
			if in != nil && math.Abs(v-math.Round(*in/step)*step) < 0.01 {
				dc.FilledRoundedRect(bx, stripY+stripH+ext+8, bw, 8, 4, hex(0x9a9a9a))
			}
		}
		if set != nil && f.st.Mode != thermostat.Off {
			k := float32((ring - first) / step)
			bx := xs + k*pitch
			dc.RoundedRect(bx-7, stripY-ext, bw+14, stripH+2*ext, (bw+14)/2, gogui.White, 4)
		}
		cy := stripY + stripH/2
		c := hex(0x8a8a8a)
		dc.Line(pad+16, cy-16, pad+4, cy, c, 3)
		dc.Line(pad+4, cy, pad+16, cy+16, c, 3)
		dc.Line(w-pad-16, cy-16, w-pad-4, cy, c, 3)
		dc.Line(w-pad-4, cy, w-pad-16, cy+16, c, 3)
	})
	if goal != nil && in != nil && f.working() {
		ph := f.phase(1800 * time.Millisecond)
		a, b := min(*goal, *in), max(*goal, *in)
		f.motion(func(dc *gogui.DrawContext) {
			span := b - a
			if span <= 0 {
				return
			}
			at := a + span*ph
			if f.doing() == cooling {
				at = b - span*ph
			}
			for i := range n {
				v := first + float64(i)*step
				if v < a-0.01 || v > b+0.01 {
					continue
				}
				d := math.Abs(v - at)
				glow := float32(max(0, 1-d/(1.5*step)))
				if glow <= 0 {
					continue
				}
				bx := xs + float32(i)*pitch
				dc.FilledRoundedRect(
					bx,
					stripY,
					bw,
					stripH,
					bw/2,
					gogui.White.WithOpacity(0.3*glow),
				)
			}
		})
	}
	f.drag("strip", rect{x0 - 10, stripY - ext, x1 - x0 + 20, stripH + 2*ext},
		func(x, _, _, _ float32, _ float64) float64 {
			return first + float64((x-xs-bw/2)/pitch)*step
		})
	f.step("left", rect{pad - 10, stripY, 40, stripH}, -1)
	f.step("right", rect{w - pad - 30, stripY, 40, stripH}, 1)
}
