package clockview

import (
	"math"
	"strings"

	"github.com/go-gui-org/go-glyph"
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
)

var (
	steelLight = theme.Color{R: 0xe4, G: 0xe6, B: 0xea}
	steelMid   = theme.Color{R: 0xb8, G: 0xbc, B: 0xc2}
	steelDark  = theme.Color{R: 0x7c, G: 0x81, B: 0x88}
	navy       = theme.Color{R: 0x1a, G: 0x2b, B: 0x55}
	navyRaised = theme.Color{R: 0x2a, G: 0x3f, B: 0x78}
	navyShade  = theme.Color{R: 0x10, G: 0x1b, B: 0x38}
	lume       = theme.Color{R: 0xf4, G: 0xf5, B: 0xf7}
)

const (
	octDial  = 0.8
	octPitch = 0.058
)

func Octagon(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	hour, minute, ok := clockHands(r)
	if !ok {
		return Plain(w, box, r, ink)
	}
	day := dayOf(r.Date)
	return dialCanvas(
		"octagon",
		box,
		r.Undated(),
		ink,
		true,
		func(dc *gogui.DrawContext, cx, cy, d float32) {
			rad := d / 2
			dc.FilledPolygonGradient(octagon(cx, cy, rad), vertical(steelLight, steelDark))
			dc.FilledPolygonGradient(octagon(cx, cy, rad*0.93), vertical(steelDark, steelLight))
			dc.FilledPolygonGradient(octagon(cx, cy, rad*0.9), vertical(steelMid, steelLight))
			for i := range 8 {
				dx, dy := heading(float64(i)*45 + 22.5)
				screw(dc, cx+dx*rad*0.86, cy+dy*rad*0.86, rad*0.035, float64(i)*45)
			}

			dial := rad * octDial
			dc.FilledCircle(cx, cy, dial*1.02, widgets.Color(steelDark))
			dc.FilledCircle(cx, cy, dial, widgets.Color(navyShade))
			dc.FilledCircle(cx, cy, dial*0.91, widgets.Color(navy))
			tapisserie(dc, cx, cy, dial*0.9, rad*octPitch)

			white := widgets.Color(lume)
			for i := range 60 {
				if i%5 != 0 {
					dc.FilledPolygon(
						blade(cx, cy, dial*0.93, dial*0.98, dial*0.008, dial*0.008, float64(i)*6),
						white,
					)
				}
			}
			for i := range 12 {
				deg := float64(i) * 30
				switch {
				case i == 3 && day != "":
					dateWindow(dc, cx+dial*0.72, cy, dial*0.15, day)
				case i == 0:
					for _, side := range []float32{-1, 1} {
						baton(dc, cx, cy, deg, dial*0.66, dial*0.9, dial*0.05, side*dial*0.04)
					}
				default:
					baton(dc, cx, cy, deg, dial*0.66, dial*0.9, dial*0.055, 0)
				}
			}

			watchHand(dc, cx, cy, hourAngle(hour, minute), dial*0.52, dial*0.07)
			watchHand(dc, cx, cy, float64(minute)*6, dial*0.8, dial*0.055)
			sec := float64(r.Second) * 6
			dc.FilledPolygon(
				blade(cx, cy, -dial*0.18, dial*0.86, dial*0.014, dial*0.008, sec),
				widgets.Color(steelLight),
			)
			dc.FilledCircle(cx, cy, dial*0.05, widgets.Color(steelMid))
			dc.FilledCircle(cx, cy, dial*0.022, widgets.Color(steelLight))
		},
	)
}

func octagon(cx, cy, r float32) []float32 {
	out := make([]float32, 0, 16)
	for i := range 8 {
		dx, dy := heading(float64(i)*45 + 22.5)
		out = append(out, cx+dx*r, cy+dy*r)
	}
	return out
}

func vertical(top, bottom theme.Color) *gogui.CanvasGradient {
	return &gogui.CanvasGradient{Stops: []gogui.GradientStop{
		{Color: widgets.Color(top), Pos: 0},
		{Color: widgets.Color(bottom), Pos: 1},
	}}
}

func screw(dc *gogui.DrawContext, x, y, r float32, deg float64) {
	hex := make([]float32, 0, 12)
	for i := range 6 {
		dx, dy := heading(float64(i)*60 + deg)
		hex = append(hex, x+dx*r, y+dy*r)
	}
	dc.FilledPolygonGradient(hex, vertical(steelLight, steelDark))
	dc.FilledPolygon(blade(x, y, -r*0.7, r*0.7, r*0.22, r*0.22, deg+90), widgets.Color(steelDark))
}

func tapisserie(dc *gogui.DrawContext, cx, cy, radius, pitch float32) {
	n := int(math.Ceil(float64(radius / pitch)))
	top, shade := widgets.Color(navyRaised), widgets.Color(navyShade)
	side := pitch * 0.68
	for j := -n; j <= n; j++ {
		for i := -n; i <= n; i++ {
			x, y := cx+float32(i)*pitch-side/2, cy+float32(j)*pitch-side/2
			far := max(abs32(x-cx), abs32(x+side-cx))
			farY := max(abs32(y-cy), abs32(y+side-cy))
			if far*far+farY*farY > radius*radius {
				continue
			}
			dc.FilledRect(x+side*0.12, y+side*0.12, side, side, shade)
			dc.FilledRect(x, y, side, side, top)
		}
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func baton(dc *gogui.DrawContext, cx, cy float32, deg float64, from, to, wide, offset float32) {
	pts := along(cx, cy, deg,
		from, offset-wide/2, to, offset-wide/2, to, offset+wide/2, from, offset+wide/2)
	dc.FilledPolygon(pts, widgets.Color(lume))
	groove := along(cx, cy, deg,
		from+wide*0.3, offset-wide*0.14, to-wide*0.3, offset-wide*0.14,
		to-wide*0.3, offset+wide*0.14, from+wide*0.3, offset+wide*0.14)
	dc.FilledPolygon(groove, widgets.Color(steelMid))
}

func watchHand(dc *gogui.DrawContext, cx, cy float32, deg float64, length, wide float32) {
	h := wide / 2
	dc.FilledPolygon(along(cx, cy, deg,
		-length*0.12, h*0.8, length*0.9, h, length, 0, length*0.9, -h, -length*0.12, -h*0.8,
	), widgets.Color(steelLight))
	dc.FilledPolygon(along(cx, cy, deg,
		length*0.25, h*0.45, length*0.86, h*0.45, length*0.86, -h*0.45, length*0.25, -h*0.45,
	), widgets.Color(lume))
	dc.FilledPolygon(along(cx, cy, deg,
		-length*0.12, 0, length*0.9, 0, length, 0, length*0.9, -h, -length*0.12, -h*0.8,
	), widgets.Color(steelMid).WithOpacity(0.5))
}

func dateWindow(dc *gogui.DrawContext, x, y, size float32, day string) {
	dc.FilledRoundedRect(
		x-size*0.55,
		y-size*0.42,
		size*1.1,
		size*0.84,
		size*0.08,
		widgets.Color(steelDark),
	)
	dc.FilledRect(x-size*0.5, y-size*0.37, size, size*0.74, widgets.Color(lume))
	st := Lettering(glyph.TypefaceBold, size*0.62, navy)
	dc.Text(x-dc.TextWidth(day, st)/2, y-dc.FontHeight(st)/2, day, st)
}

func dayOf(date string) string {
	for _, f := range strings.FieldsFunc(date, func(c rune) bool { return c < '0' || c > '9' }) {
		return f
	}
	return ""
}
