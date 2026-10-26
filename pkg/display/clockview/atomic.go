package clockview

import (
	"math"
	"strconv"

	"github.com/go-gui-org/go-glyph"
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
)

var (
	atomicMint  = theme.Color{R: 0xa6, G: 0xd6, B: 0xc0}
	atomicCream = theme.Color{R: 0xf0, G: 0xea, B: 0xda}
	atomicInk   = theme.Color{R: 0x16, G: 0x16, B: 0x16}
	atomicRed   = theme.Color{R: 0xd8, G: 0x23, B: 0x23}
)

func Atomic(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	hour, minute, ok := clockHands(r)
	if !ok {
		return Plain(w, box, r, ink)
	}
	mint := widgets.Color(atomicMint)
	cream := widgets.Color(atomicCream)
	black := widgets.Color(atomicInk)
	return dialCanvas("atomic", box, r, ink, true, func(dc *gogui.DrawContext, cx, cy, d float32) {
		rad := d / 2
		dc.FilledCircle(cx, cy, rad, mint)
		for i := range 60 {
			dc.FilledPolygon(teardrop(cx, cy, float64(i)*6, rad*0.66, rad*0.93, rad*0.032), cream)
		}
		dc.FilledCircleGradient(cx, cy, rad*0.62, vertical(steelLight, steelDark))
		dc.FilledCircleGradient(cx, cy, rad*0.585, vertical(steelDark, steelLight))
		dc.FilledCircle(cx, cy, rad*0.56, cream)

		num := Lettering(glyph.TypefaceBold, rad*0.1, atomicCream)
		nh := dc.FontHeight(num)
		for i := range 12 {
			deg := float64(i) * 30
			dc.FilledPolygon(teardrop(cx, cy, deg, rad*0.1, rad*0.5, rad*0.085), mint)
			label := strconv.Itoa(12)
			if i > 0 {
				label = strconv.Itoa(i)
			}
			dx, dy := heading(deg)
			x, y := cx+dx*rad*0.41, cy+dy*rad*0.41
			dc.Text(x-dc.TextWidth(label, num)/2, y-nh/2, label, num)
		}

		dc.FilledPolygon(
			taper(cx, cy, rad*0.32, rad*0.06, rad*0.06, hourAngle(hour, minute)),
			black,
		)
		dc.FilledPolygon(taper(cx, cy, rad*0.5, rad*0.045, rad*0.06, float64(minute)*6), black)
		dc.FilledCircle(cx, cy, rad*0.04, black)
		sec := float64(r.Second) * 6
		dc.FilledPolygon(
			blade(cx, cy, -rad*0.12, rad*0.54, rad*0.014, rad*0.008, sec),
			widgets.Color(atomicRed),
		)
		dc.FilledCircle(cx, cy, rad*0.018, widgets.Color(atomicRed))
	})
}

func teardrop(cx, cy float32, deg float64, from, to, round float32) []float32 {
	const n = 14
	pts := []float32{from, 0}
	center := to - round
	for i := range n + 1 {
		t := math.Pi * (float64(i)/n - 0.5)
		pts = append(pts, center+round*float32(math.Cos(t)), round*float32(math.Sin(t)))
	}
	return along(cx, cy, deg, pts...)
}
