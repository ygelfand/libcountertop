package thermostatview

import (
	"math"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	kit "github.com/ygelfand/libcountertop/pkg/display/panelkit"
	"github.com/ygelfand/libcountertop/pkg/display/style"
)

var wheelLook = look{
	back: hex(0x0e1726),
	sheet: sheet{
		shade: rgba(0x05080e, 0.6), panel: hex(0x13203a), ink: hex(0xe8eef7),
		muted: hex(0x8fa3bf), accent: hex(0xf08a4b), chosen: hex(0x1c2d4f),
		radius: 0.03, edge: hex(0x1c2d4f),
	},
	draw: drawWheel,
}

const wheelStep = 64

func drawWheel(f *face) {
	w, h := f.rw, f.rh
	card, band := hex(0x13203a), hex(0x1c2d4f)
	ink, muted := hex(0xe8eef7), hex(0x8fa3bf)
	tone := muted
	switch f.doing() {
	case heating:
		tone = hex(0xf08a4b)
	case cooling:
		tone = hex(0x4fa8ff)
	}
	compact := f.shape == small
	pad, nameSize, nameY := float32(40), float32(28), float32(66)
	switch f.shape {
	case full:
		pad, nameSize, nameY = 80, 32, 104
	case small:
		pad, nameSize, nameY = 26, 22, 44
	}
	f.paint(func(dc *gogui.DrawContext) {
		nw := write(dc, pad, nameY, f.name(), nameSize, ink, font{bold: true}, left)
		if f.working() {
			write(
				dc,
				w-pad,
				nameY,
				f.action(),
				nameSize*0.75,
				tone,
				font{bold: true},
				right,
			)
		}
		rf := float32(20)
		ry := nameY + 40
		if f.shape == full {
			rf, ry = 22, 158
		}
		if compact {
			if f.st.Current != nil {
				x := pad + nw + 16
				icon(dc, x, nameY-7, "mdi:thermometer", 18, muted, left)
				write(dc, x+20, nameY, f.number(f.st.Current)+"°", 18, hex(0xc3d0e3), font{}, left)
			}
			return
		}
		x := pad
		if f.st.Current != nil {
			icon(dc, x-4, ry-rf*0.35, "mdi:thermometer", rf*1.1, muted, left)
			x += rf * 1.1
			x += write(
				dc,
				x,
				ry,
				f.number(f.st.Current)+"°",
				rf,
				hex(0xc3d0e3),
				font{},
				left,
			) + rf*1.4
		}
		if hum := f.humidity(); hum != "" {
			icon(dc, x-4, ry-rf*0.35, "mdi:water-outline", rf*1.1, muted, left)
			x += rf * 1.1
			x += write(dc, x, ry, hum, rf, hex(0xc3d0e3), font{}, left) + rf*0.6
		}
		if out := f.outside(); out != "" {
			write(dc, x, ry, "· "+out+" out", rf, muted, font{}, left)
		}
	})
	var num rect
	var lists []rect
	switch f.shape {
	case full:
		num = rect{80, 230, 380, 460}
		for i := range 3 {
			lists = append(lists, rect{540 + float32(i)*230, 230, 200, 460})
		}
	case tall:
		num = rect{pad, 136, w - 2*pad, 360}
		sw := (w - 2*pad - 24) / 3
		for i := range 3 {
			lists = append(lists, rect{pad + float32(i)*(sw+12), 520, sw, h - 520 - 34})
		}
	case mid:
		num = rect{pad, 136, 300, h - 136 - 34}
		sw := (w - 2*pad - 300 - 24 - 24) / 3
		for i := range 3 {
			lists = append(lists, rect{pad + 324 + float32(i)*(sw+12), 136, sw, h - 136 - 34})
		}
	default:
		nw := float32(190)
		if w > 700 {
			nw = 210
		}
		num = rect{pad, 66, nw, h - 66 - 22}
		sw := (w - 2*pad - nw - 32) / 3
		for i := range 3 {
			lists = append(lists, rect{pad + nw + 16 + float32(i)*(sw+8), 66, sw, h - 66 - 22})
		}
	}
	f.numberWheel(num, card, band, ink, tone)
	kinds := f.kinds()
	for i, kind := range kinds {
		if i >= len(lists) {
			break
		}
		col := ink
		if kind == pickMode {
			col = tone
		}
		f.listWheel(lists[i], kind, card, band, col, muted)
	}
}

func (f *face) numberWheel(r rect, card, band, ink, tone gogui.Color) {
	radius := float32(28)
	if f.shape == small {
		radius = 20
	}
	cy := r.y + r.h/2
	bh := min(124, r.h*0.3)
	big := bh * 0.92
	set := f.setpoint()
	step := f.st.Step
	if step <= 0 {
		step = 1
	}
	light := font{family: style.LightFamily}
	var shown float64
	if set != nil {
		shown = f.eased("set", *set)
	}
	glow := float32(0)
	if f.working() {
		glow = f.breath(3 * time.Second)
	}
	f.paint(func(dc *gogui.DrawContext) {
		dc.FilledRoundedRect(r.x, r.y, r.w, r.h, radius, card)
		dc.FilledRoundedRect(r.x+12, cy-bh/2, r.w-24, bh, 20, band)
	})
	if glow > 0 {
		f.motion(func(dc *gogui.DrawContext) {
			dc.RoundedRect(r.x+12, cy-bh/2, r.w-24, bh, 20, tone.WithOpacity(0.15+0.25*glow), 3)
		})
	}
	if set == nil {
		f.paint(func(dc *gogui.DrawContext) {
			write(dc, r.x+r.w/2, cy+big*0.36, "--", big, ink, light, center)
		})
		return
	}
	base := math.Round(shown/step) * step
	off := float32((shown - base) / step)
	f.paint(func(dc *gogui.DrawContext) {
		dc.Save()
		top := r.y + 4
		if f.st.Ranged() {
			top = r.y + 44
		}
		for i := -3; i <= 3; i++ {
			d := float32(i) - off
			ad := float32(math.Abs(float64(d)))
			size := big * wheelScale(ad)
			y := cy - wheelOffset(d, bh, big)
			if y-size*0.4 < top || y+size*0.4 > r.y+r.h-4 {
				continue
			}
			c := kit.Mix(gogui.White, hex(0x5d7398), min(ad, 1))
			if ad > 1 {
				c = kit.Mix(hex(0x5d7398), hex(0x2f4060), min(ad-1, 1))
			}
			v := base + float64(i)*step
			if ad < 0.5 {
				degrees(
					dc,
					r.x+r.w/2-8,
					y+size*0.36,
					f.format(v),
					size,
					c,
					hex(0x8fa3bf),
					light,
					center,
				)
			} else {
				write(dc, r.x+r.w/2, y+size*0.36, f.format(v), size, c, light, center)
			}
		}
		dc.Restore()
		if f.st.Ranged() {
			f.ends(dc, r.x+r.w/2, r.y+30, 16, tone, hex(0x5d7398), font{spacing: 2}, center)
		}
	})
	f.drag("wheel", r, func(_, _, _, dy float32, start float64) float64 {
		return start + float64(dy/wheelStep)*step
	})
	up := wheelOffset(1, bh, big)
	f.step("next", rect{r.x, cy - up - big*0.3, r.w, big * 0.6}, 1)
	f.step("prev", rect{r.x, cy + up - big*0.3, r.w, big * 0.6}, -1)
	if f.st.Ranged() {
		f.endTap("low", rect{r.x, r.y, r.w / 2, 44}, Low)
		f.endTap("high", rect{r.x + r.w/2, r.y, r.w / 2, 44}, High)
	}
}

func wheelScale(d float32) float32 {
	switch {
	case d <= 1:
		return 1 - 0.4*d
	case d <= 2:
		return 0.6 - 0.16*(d-1)
	}
	return 0.44
}

func wheelOffset(d, bh, big float32) float32 {
	ad := float32(math.Abs(float64(d)))
	s1, s2 := big*0.6, big*0.44
	d1 := bh/2 + 6 + s1*0.36
	d2 := d1 + s1*0.5 + s2*0.5 + 4
	var o float32
	switch {
	case ad <= 1:
		o = d1 * ad
	case ad <= 2:
		o = d1 + (d2-d1)*(ad-1)
	default:
		o = d2 + (d2-d1)*(ad-2)
	}
	if d < 0 {
		return -o
	}
	return o
}

func (f *face) listWheel(r rect, kind pick, card, band, col, muted gogui.Color) {
	radius := float32(24)
	if f.shape == small {
		radius = 18
	}
	opts := options(f.st, kind)
	at := 0
	for i, o := range opts {
		if o.current {
			at = i
		}
	}
	val := f.pickValue(kind)
	top := r.y + 26
	if r.h > 300 {
		top = r.y + 34
	}
	cy := r.y + r.h/2 + 12
	if r.h > 300 {
		cy = r.y + r.h/2 + 14
	}
	bh := min(76, r.h*0.24)
	vf := min(30, max(16, r.w*0.16))
	nf := max(14, vf*0.78)
	step := max(nf*1.7, nf+14)
	rings := 1
	if r.h > 340 {
		rings = 2
	}
	type spot struct {
		y     float32
		label string
		value string
		ring  int
	}
	var spots []spot
	for k := 1; k <= rings; k++ {
		dy := bh/2 + step*(float32(k)-0.35)
		if i := at - k; i >= 0 && cy-dy-nf > top+4 {
			spots = append(spots, spot{cy - dy, opts[i].label, opts[i].value, k})
		}
		if i := at + k; i < len(opts) && cy+dy+nf*0.5 < r.y+r.h-8 {
			spots = append(spots, spot{cy + dy, opts[i].label, opts[i].value, k})
		}
	}
	f.paint(func(dc *gogui.DrawContext) {
		dc.FilledRoundedRect(r.x, r.y, r.w, r.h, radius, card)
		write(dc, r.x+r.w/2, top, upper(kind.title()), 14, muted, font{spacing: 3}, center)
		dc.FilledRoundedRect(r.x+8, cy-bh/2, r.w-16, bh, 16, band)
		write(dc, r.x+r.w/2, cy+vf*0.36, val, vf, col, font{bold: true}, center)
		for _, s := range spots {
			c := hex(0x3b4e6e)
			if s.ring > 1 {
				c = hex(0x2a3a58)
			}
			write(dc, r.x+r.w/2, s.y+nf*0.36, s.label, nf, c, font{}, center)
		}
	})
	f.chooser("list-"+kind.title(), rect{r.x, cy - bh/2, r.w, bh}, kind)
	if !f.live() {
		return
	}
	entity := f.st.Entity
	for _, s := range spots {
		value := s.value
		f.tap("pick-"+kind.title()+value, rect{r.x, s.y - step/2, r.w, step}, func() {
			f.v.apply(chooser{entity: entity, kind: kind}, value)
		})
	}
}
