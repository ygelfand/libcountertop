package thermostatview

import (
	"strconv"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/say"
)

var columnLook = look{
	back: hex(0xf4f1ea),
	sheet: sheet{
		shade: rgba(0x1d2a38, 0.35), panel: hex(0xffffff), ink: hex(0x1d2a38),
		muted: hex(0x8a8476), accent: hex(0x2f7fd0), chosen: hex(0xece6dc),
		radius: 0.025, edge: hex(0xe3ded3),
	},
	draw: drawColumn,
}

type cardKind int

const (
	cardRow cardKind = iota
	cardList
	cardSlim
)

func drawColumn(f *face) {
	w, h := f.rw, f.rh
	ink, muted, track := hex(0x1d2a38), hex(0x8a8476), hex(0xe3ded3)
	tone, word := hex(0xb9b2a4), ink
	switch f.doing() {
	case heating:
		tone, word = hex(0xe0703d), hex(0xc85a28)
	case cooling:
		tone, word = hex(0x3d8fe0), hex(0x2f7fd0)
	}
	compact := f.shape == small
	pad := float32(40)
	if compact {
		pad = 28
	}
	var (
		y0, y1, cw, colx, insx, insy, big float32
		card                              rect
		kind                              cardKind
	)
	switch f.shape {
	case full:
		y0, y1, cw, colx = 140, h-100, 76, 124
		card, kind = rect{w - 400, 140, 320, 300}, cardList
		insx, insy, big = 460, 440, 220
	case tall:
		y0, y1, cw, colx = 104, 520, 60, pad+10
		card, kind = rect{pad, 560, w - 2*pad, h - 580}, cardRow
		insx, insy, big = 300, 300, 150
	case mid:
		y0, y1, cw, colx = 96, h-44, 60, pad+10
		card, kind = rect{330, h - 174, w - pad - 330, 130}, cardRow
		insx, insy, big = 330, 270, 150
	default:
		cardW := float32(190)
		if w > 700 {
			cardW = 210
		}
		y0, y1, cw, colx = 70, h-26, 40, pad
		card, kind = rect{w - pad - cardW, 66, cardW, h - 92}, cardSlim
		insy, big = 236, 110
		if h >= 410 {
			insy = 246
		}
	}
	lo, hi := f.span()
	set := f.setpoint()
	shown := lo
	if set != nil {
		shown = f.eased("set", *set)
	}
	yOf := func(v float64) float32 { return y1 - float32(frac(v, lo, hi))*(y1-y0) }
	yt := yOf(shown)
	tabW, tabH := float32(150), float32(60)
	if compact {
		tabW, tabH = 124, 50
	}
	tx := colx + cw + 40
	if compact {
		tx = colx + cw + 14
	}
	insx = max(insx, tx+tabW+30)
	if compact {
		insx = tx + tabW + 26
	}
	nameSize, nameY := float32(28), float32(64)
	if compact {
		nameSize, nameY = 22, 44
	}
	light := font{family: style.LightFamily}
	f.paint(func(dc *gogui.DrawContext) {
		write(dc, pad, nameY, f.name(), nameSize, ink, font{bold: true}, left)
		dc.FilledRoundedRect(colx, y0, cw, y1-y0, cw/2, track)
		if set != nil {
			dc.FilledRoundedRect(colx, yt, cw, y1-yt, cw/2, tone)
			dc.FilledRect(colx, yt, cw, min(cw/2, y1-yt), tone)
		}
		if !compact {
			first := float64(int(lo/10+0.999) * 10)
			for v := first; v <= hi; v += 10 {
				y := yOf(v)
				if y < yt-tabH/2-14 || y > yt+tabH/2+14 {
					write(dc, colx+cw+12, y+6, strconv.Itoa(int(v)), 16, muted, font{}, left)
				}
			}
		}
		if set == nil {
			return
		}
		dc.FilledRoundedRect(colx-cw*0.3, yt-7, cw*1.6, 14, 7, ink)
		dc.FilledRoundedRect(tx, yt-tabH/2, tabW, tabH, tabH/2, ink)
		ts := float32(26)
		if compact {
			ts = 22
		}
		base := yt + ts*0.36
		write(dc, tx+tabH*0.36, base, "−", ts, gogui.White, font{}, left)
		write(dc, tx+tabW/2, base, f.format(shown)+"°", ts, gogui.White, font{bold: true}, center)
		write(dc, tx+tabW-tabH*0.36, base, "+", ts, gogui.White, font{}, right)
	})
	f.paint(func(dc *gogui.DrawContext) {
		if f.st.Ranged() {
			f.ends(dc, insx+4, insy-big*0.86, 16, word, muted, font{spacing: 2}, left)
		} else if !compact {
			write(
				dc,
				insx+4,
				insy-big*0.86,
				upper(say.T("thermostat.inside")),
				16,
				muted,
				font{spacing: 2},
				left,
			)
		}
		degrees(dc, insx, insy, f.number(f.st.Current), big, ink, muted, light, left)
		ry := insy + 52
		rf := float32(20)
		if compact {
			ry, rf = insy+36, 16
		}
		x := insx
		if out := f.outside(); out != "" {
			icon(dc, x, ry-rf*0.35, "mdi:weather-sunny", rf*1.1, muted, left)
			x += rf * 1.5
			x += write(dc, x, ry, out, rf, hex(0x4a4539), font{}, left) + rf*1.2
		}
		if hum := f.humidity(); hum != "" {
			icon(dc, x, ry-rf*0.35, "mdi:water-outline", rf*1.1, muted, left)
			write(dc, x+rf*1.5, ry, hum, rf, hex(0x4a4539), font{}, left)
		}
	})
	if set != nil && f.working() {
		ph := float32(f.phase(2600 * time.Millisecond))
		if f.doing() == cooling {
			ph = 1 - ph
		}
		f.motion(func(dc *gogui.DrawContext) {
			span := y1 - yt
			if span < cw {
				return
			}
			band := min(span*0.35, cw*2)
			y := y1 - band - (span-band)*ph
			dc.FilledRectGradient(colx, y, cw, band, &gogui.CanvasGradient{
				X1: 0, Y1: y, X2: 0, Y2: y + band, Stops: []gogui.GradientStop{
					{Color: gogui.White.WithOpacity(0), Pos: 0},
					{Color: gogui.White.WithOpacity(0.22), Pos: 0.5},
					{Color: gogui.White.WithOpacity(0), Pos: 1},
				},
			})
		})
	}
	f.drag("column", rect{colx - 30, y0 - 20, cw + 60, y1 - y0 + 40},
		func(_, y, _, _ float32, _ float64) float64 {
			return lo + float64((y1-y)/(y1-y0))*(hi-lo)
		})
	if set != nil {
		f.step("down", rect{tx, yt - tabH/2, tabW * 0.36, tabH}, -1)
		f.step("up", rect{tx + tabW*0.64, yt - tabH/2, tabW * 0.36, tabH}, 1)
	}
	if f.st.Ranged() {
		f.endTap("low", rect{insx, insy - big*0.86 - 22, 70, 30}, Low)
		f.endTap("high", rect{insx + 70, insy - big*0.86 - 22, 80, 30}, High)
	}
	f.columnCard(card, kind, ink, muted, word)
}

func (f *face) columnCard(r rect, form cardKind, ink, muted, accent gogui.Color) {
	kinds := f.kinds()
	radius := float32(22)
	if form == cardSlim {
		radius = 18
	}
	f.paint(func(dc *gogui.DrawContext) {
		dc.FilledRoundedRect(r.x, r.y, r.w, r.h, radius, gogui.White)
	})
	n := float32(len(kinds))
	for i, which := range kinds {
		lab, val := which.title(), f.pickValue(which)
		col, bold := ink, false
		if which == pickMode {
			col, bold = accent, accent != ink
		}
		var cell rect
		switch form {
		case cardRow:
			cw := r.w / n
			cell = rect{r.x + float32(i)*cw, r.y, cw, r.h}
			f.paint(func(dc *gogui.DrawContext) {
				if i > 0 {
					dc.Line(cell.x, r.y+22, cell.x, r.y+r.h-22, hex(0xece6dc), 2)
				}
				write(dc, cell.x+22, r.y+r.h/2-8, upper(lab), 14, muted, font{spacing: 2}, left)
				write(dc, cell.x+22, r.y+r.h/2+22, val, 22, col, font{bold: bold}, left)
				chevron(dc, cell.x+cell.w-30, r.y+r.h/2+2, 14, hex(0xb9b2a4))
			})
		default:
			rh := r.h / n
			cell = rect{r.x, r.y + float32(i)*rh, r.w, rh}
			f.paint(func(dc *gogui.DrawContext) {
				if i > 0 {
					dc.Line(r.x+20, cell.y, r.x+r.w-20, cell.y, hex(0xece6dc), 2)
				}
				if form == cardList {
					write(dc, r.x+22, cell.y+rh/2-6, upper(lab), 14, muted, font{spacing: 2}, left)
					write(dc, r.x+22, cell.y+rh/2+20, val, 22, col, font{bold: bold}, left)
				} else {
					write(dc, r.x+18, cell.y+rh/2+6, lab, 16, muted, font{}, left)
					write(dc, r.x+r.w-36, cell.y+rh/2+7, val, 18, col, font{bold: bold}, right)
				}
				chevron(dc, r.x+r.w-26, cell.y+rh/2-7, 14, hex(0xb9b2a4))
			})
		}
		f.chooser("card"+lab, cell, which)
	}
}

func chevron(dc *gogui.DrawContext, x, y, size float32, c gogui.Color) {
	dc.Line(x, y, x+size/2, y+size/2, c, 2.5)
	dc.Line(x+size/2, y+size/2, x, y+size, c, 2.5)
}

func chevronDown(dc *gogui.DrawContext, x, y, size float32, c gogui.Color) {
	dc.Line(x, y, x+size/2, y+size/2, c, 2)
	dc.Line(x+size/2, y+size/2, x+size, y, c, 2)
}
