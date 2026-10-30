package thermostatview

import (
	"math"
	"strings"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/say"
)

var splitLook = look{
	back: hex(0xece6dc),
	sheet: sheet{
		shade: rgba(0x3a3129, 0.4), panel: hex(0xf5f0e8), ink: hex(0x3a3129),
		muted: hex(0x7a6d5f), accent: hex(0xc8562b), chosen: hex(0xe3dbcf),
		family: style.SerifFamily, radius: 0.004, edge: hex(0xd6cdbf),
	},
	draw: drawSplit,
}

func (f *face) splitWord() string {
	if p := f.problem(); p != "" {
		return p
	}
	switch f.doing() {
	case heating:
		return say.T("thermostat.heating")
	case cooling:
		return say.T("thermostat.cooling")
	case stopped:
		return say.T("thermostat.off")
	}
	return say.T("thermostat.holding")
}

func drawSplit(f *face) {
	w, h := f.rw, f.rh
	serif := font{family: style.SerifFamily}
	sans := font{spacing: 3}
	hot := hex(0x6f6a63)
	switch f.doing() {
	case heating:
		hot = hex(0xc8562b)
	case cooling:
		hot = hex(0x2f6fae)
	}
	compact := f.shape == small
	var hx, hy, hw, hh, lx, ly, lw, lh float32
	if f.shape == tall {
		hx, hy, hw, hh = 0, 0, w, h*0.5
		lx, ly, lw, lh = 0, h*0.5, w, h*0.5
	} else {
		hx, hy, hw, hh = w*0.5, 0, w*0.5, h
		lx, ly, lw, lh = 0, 0, w*0.5, h
	}
	pad, lab := float32(40), float32(18)
	if compact {
		pad, lab = 26, 14
	}
	tick := float32(16)
	if compact {
		tick = 14
	}
	big := min(hh*0.5, hw*0.42)
	if f.shape == tall {
		big = 200
	}
	bx := hx + pad
	by := hy + hh/2 + big*0.38 + 14
	if compact {
		by = hy + hh/2 + big*0.38 + 8
	}
	sx := hx + hw - pad - 44
	top, bot := hy+110, hy+hh-70
	if compact {
		sx, top, bot = hx+hw-pad-30, hy+72, hy+hh-52
	}
	steps := 7
	if compact {
		steps = 5
	}
	span := steps - 1
	step := f.st.Step
	if step <= 0 {
		step = 1
	}
	set := f.setpoint()
	var shown float64
	if set != nil {
		shown = f.eased("set", *set)
	}
	gap := (bot - top) / float32(span)
	wordY := hy + 56
	if compact {
		wordY = hy + 38
	}
	f.paint(func(dc *gogui.DrawContext) {
		dc.FilledRect(hx, hy, hw, hh, hot)
		if f.st.Ranged() {
			f.ends(dc, hx+pad, wordY, lab, gogui.White, hex(0xfbe9df).WithOpacity(0.55), sans, left)
		} else {
			write(dc, hx+pad, wordY, upper(f.splitWord()), lab, hex(0xfbe9df), sans, left)
		}
		if set != nil {
			text := f.format(*set)
			nw := measure(dc, text, big, serif)
			write(dc, bx-6, by, text, big, gogui.White, serif, left)
			rr := max(5, big*0.05)
			dc.Circle(bx-6+nw+rr*2.2, by-big*0.62, rr, hex(0xfbe9df), max(2.5, rr*0.4))
		}
		if set == nil {
			return
		}
		base := math.Round(shown/step) * step
		off := float32((shown - base) / step)
		mid := top + float32(span/2)*gap
		for i := -span/2 - 1; i <= span/2+1; i++ {
			y := mid + (float32(i)+off)*gap
			if y < top-1 || y > bot+1 {
				continue
			}
			v := base - float64(i)*step
			if i == 0 && math.Abs(float64(off)) < 0.5 {
				continue
			}
			dc.Line(sx, y, sx+20, y, hex(0xfbe9df).WithOpacity(0.55), 3)
			edge := y < top+gap*0.5 || y > bot-gap*0.5
			if edge {
				write(dc, sx-12, y+6, f.format(v), tick, hex(0xfbe9df), font{}, right)
			}
		}
		dc.Line(sx-6, mid, sx+36, mid, gogui.White, 6)
		write(dc, sx-12, mid+6, f.format(*set), tick, gogui.White, font{bold: true}, right)
		pm := float32(44)
		if compact {
			pm = 34
		}
		write(dc, sx+10, top-26+pm*0.36, "+", pm, gogui.White, font{}, center)
		write(dc, sx+10, bot+40+pm*0.1, "−", pm, gogui.White, font{}, center)
	})
	if f.working() {
		lift := 0.2 * f.breath(3500*time.Millisecond)
		f.motion(func(dc *gogui.DrawContext) {
			dc.FilledRect(hx, hy, hw, hh, gogui.White.WithOpacity(lift))
		})
	}
	if set != nil {
		f.drag("scale", rect{sx - 60, top - 10, hw - (sx - 60 - hx), bot - top + 20},
			func(_, _, _, dy float32, start float64) float64 {
				return start - float64(dy/gap)*step
			})
		f.step("plus", rect{sx - 20, top - 70, 60, 60}, 1)
		f.step("minus", rect{sx - 20, bot + 10, 60, 60}, -1)
		f.drag("number", rect{hx, hy + hh*0.2, sx - 60 - hx, hh * 0.6},
			func(_, _, _, dy float32, start float64) float64 {
				return start - float64(dy/gap)*step
			})
	}
	if f.st.Ranged() {
		f.endTap("low", rect{hx + pad - 8, wordY - lab - 8, 70, lab + 16}, Low)
		f.endTap("high", rect{hx + pad + 62, wordY - lab - 8, 80, lab + 16}, High)
	}
	ink, muted := hex(0x3a3129), hex(0x7a6d5f)
	nameY := ly + 56
	if compact {
		nameY = ly + 38
	}
	kinds := f.kinds()
	var rowX, rowY, rowW, rowH, rf float32
	var ib, iy float32
	var parts []string
	if out := f.outside(); out != "" {
		parts = append(parts, out+" "+strings.ToLower(say.T("thermostat.outside")))
	}
	if hum := f.humidity(); hum != "" {
		parts = append(parts, hum+" "+strings.ToLower(say.T("thermostat.humidity")))
	}
	readings := strings.Join(parts, " · ")
	if f.shape == tall {
		ib, iy = 150, ly+210
		rowX, rowW, rowH, rf = lx+w*0.5, w*0.5-pad, 58, 20
		rowY = ly + 92
	} else {
		ib = 120
		if compact {
			ib = 84
		}
		iy = ly + 110 + ib*0.74
		switch f.shape {
		case small:
			iy = ly + 74 + ib*0.74
		case full:
			ib, iy = 210, ly+430
		}
		rowH, rf = 54, 20
		if compact {
			rowH, rf = 40, 16
		}
		rowX, rowW = lx+pad, lw-2*pad
		rowY = ly + lh - float32(len(kinds))*rowH - 30
		if compact {
			rowY = ly + lh - float32(len(kinds))*rowH - 14
		}
	}
	f.paint(func(dc *gogui.DrawContext) {
		write(dc, lx+pad, nameY, upper(f.name()), lab, ink, sans, left)
		in := f.number(f.st.Current)
		nw := measure(dc, in, ib, serif)
		write(dc, lx+pad-4, iy, in, ib, ink, serif, left)
		rr := max(5, ib*0.05)
		dc.Circle(lx+pad-4+nw+rr*2.2, iy-ib*0.62, rr, muted, max(2.5, rr*0.4))
		rs := float32(16)
		if compact {
			rs = 14
		}
		if readings != "" {
			dy := float32(34)
			switch f.shape {
			case tall:
				dy = 36
			case small:
				dy = 28
			case full:
				dy, rs = 60, 18
			}
			write(dc, lx+pad, iy+dy, readings, rs, muted, font{}, left)
		}
		for i, kind := range kinds {
			y := rowY + float32(i)*rowH
			dc.Line(rowX, y, rowX+rowW, y, hex(0xd6cdbf), 1.5)
			write(dc, rowX, y+rowH*0.66, kind.title(), rf, muted, font{}, left)
			write(dc, rowX+rowW, y+rowH*0.66, f.pickValue(kind), rf, ink, font{}, right)
		}
	})
	for i, kind := range kinds {
		f.chooser("row"+kind.title(), rect{rowX, rowY + float32(i)*rowH, rowW, rowH}, kind)
	}
}
