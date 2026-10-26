package clockview

import (
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
)

var (
	romanFace = theme.Color{R: 0xf2, G: 0xea, B: 0xd8}
	romanInk  = theme.Color{R: 0x1c, G: 0x18, B: 0x14}

	romanHours = []string{"XII", "I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X", "XI"}
)

const (
	romanStem    = 0.15
	romanHair    = 0.055
	romanSerif   = 0.07
	romanLetterI = 0.3
	romanLetterV = 0.66
	romanKern    = 0.05
)

func Roman(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	hour, minute, ok := clockHands(r)
	if !ok {
		return Plain(w, box, r, ink)
	}
	black := widgets.Color(romanInk)
	return dialCanvas("roman", box, r, ink, false, func(dc *gogui.DrawContext, cx, cy, d float32) {
		rad := d / 2
		dc.FilledCircle(cx, cy, rad, black)
		dc.FilledCircle(cx, cy, rad*0.975, widgets.Color(romanFace))
		for _, ring := range []float32{0.955, 0.895, 0.875, 0.62} {
			dc.Circle(cx, cy, rad*ring, black, rad*0.008)
		}
		for i := range 60 {
			deg := float64(i) * 6
			if i%5 == 0 {
				dx, dy := heading(deg)
				dc.FilledCircle(cx+dx*rad*0.925, cy+dy*rad*0.925, rad*0.017, black)
				continue
			}
			dc.FilledPolygon(blade(cx, cy, rad*0.9, rad*0.95, rad*0.008, rad*0.008, deg), black)
		}

		size := rad * 0.2
		for i, numeral := range romanHours {
			for _, stroke := range romanNumeral(numeral, size) {
				dc.FilledPolygon(onDial(cx, cy, rad*0.75, float64(i)*30, stroke), black)
			}
		}

		for _, part := range spadeHour(rad) {
			dc.FilledPolygon(along(cx, cy, hourAngle(hour, minute), part...), black)
		}
		for _, part := range spadeMinute(rad) {
			dc.FilledPolygon(along(cx, cy, float64(minute)*6, part...), black)
		}
		dc.FilledCircle(cx, cy, rad*0.055, black)
		dc.FilledCircle(cx, cy, rad*0.028, widgets.Color(romanFace))
		dc.FilledCircle(cx, cy, rad*0.014, black)
	})
}

func romanNumeral(numeral string, h float32) [][]float32 {
	var wide float32
	for i, c := range numeral {
		if i > 0 {
			wide += romanKern * h
		}
		wide += romanWidth(c) * h
	}
	var out [][]float32
	x := -wide / 2
	for _, c := range numeral {
		out = append(out, romanLetter(c, x, h)...)
		x += romanWidth(c)*h + romanKern*h
	}
	return out
}

func romanWidth(c rune) float32 {
	if c == 'I' {
		return romanLetterI
	}
	return romanLetterV
}

func romanLetter(c rune, x, h float32) [][]float32 {
	top, bottom := -h/2, h/2
	stem, hair, slab := romanStem*h, romanHair*h, romanSerif*h
	w := romanWidth(c) * h
	serif := func(at, y, span float32, under bool) []float32 {
		y0, y1 := y, y+slab
		if under {
			y0, y1 = y-slab, y
		}
		return []float32{at - span/2, y0, at + span/2, y0, at + span/2, y1, at - span/2, y1}
	}
	switch c {
	case 'I':
		mid := x + w/2
		return [][]float32{
			{mid - stem/2, top, mid + stem/2, top, mid + stem/2, bottom, mid - stem/2, bottom},
			serif(mid, top, w, false),
			serif(mid, bottom, w, true),
		}
	case 'V':
		left, right, low := x+w*0.2, x+w*0.8, x+w/2
		return [][]float32{
			{left - stem/2, top, left + stem/2, top, low + stem/4, bottom, low - stem/4, bottom},
			{right - hair/2, top, right + hair/2, top, low + hair/2, bottom, low - hair/2, bottom},
			serif(left, top, w*0.42, false),
			serif(right, top, w*0.34, false),
		}
	}
	left, right := x+w*0.2, x+w*0.8
	return [][]float32{
		{left - stem/2, top, left + stem/2, top, right + stem/2, bottom, right - stem/2, bottom},
		{right - hair/2, top, right + hair/2, top, left + hair/2, bottom, left - hair/2, bottom},
		serif(left, top, w*0.42, false),
		serif(right, top, w*0.34, false),
		serif(left, bottom, w*0.34, true),
		serif(right, bottom, w*0.42, true),
	}
}

func onDial(cx, cy, radius float32, deg float64, pts []float32) []float32 {
	dx, dy := heading(deg)
	tx, ty := -dy, dx
	ox, oy := cx+dx*radius, cy+dy*radius
	out := make([]float32, len(pts))
	for i := 0; i+1 < len(pts); i += 2 {
		x, y := pts[i], pts[i+1]
		out[i] = ox + tx*x - dx*y
		out[i+1] = oy + ty*x - dy*y
	}
	return out
}

func spadeHour(rad float32) [][]float32 {
	return [][]float32{
		{
			-rad * 0.14,
			rad * 0.016,
			rad * 0.32,
			rad * 0.016,
			rad * 0.32,
			-rad * 0.016,
			-rad * 0.14,
			-rad * 0.016,
		},
		{
			rad * 0.28, 0,
			rad * 0.34, rad * 0.075,
			rad * 0.42, rad * 0.06,
			rad * 0.52, 0,
			rad * 0.42, -rad * 0.06,
			rad * 0.34, -rad * 0.075,
		},
		circle(-rad*0.14, 0, rad*0.04),
	}
}

func spadeMinute(rad float32) [][]float32 {
	return [][]float32{
		{
			-rad * 0.18,
			rad * 0.012,
			rad * 0.56,
			rad * 0.012,
			rad * 0.56,
			-rad * 0.012,
			-rad * 0.18,
			-rad * 0.012,
		},
		{
			rad * 0.5, 0,
			rad * 0.58, rad * 0.04,
			rad * 0.88, 0,
			rad * 0.58, -rad * 0.04,
		},
		circle(-rad*0.18, 0, rad*0.032),
	}
}

func circle(x, y, r float32) []float32 {
	const n = 20
	out := make([]float32, 0, 2*n)
	for i := range n {
		dx, dy := heading(float64(i) * 360 / n)
		out = append(out, x+dx*r, y+dy*r)
	}
	return out
}
