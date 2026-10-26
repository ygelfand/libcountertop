package clockview

import (
	"github.com/go-gui-org/go-glyph"
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
)

const dialShare = 0.78

var (
	railFace  = theme.Color{R: 0xf7, G: 0xf7, B: 0xf5}
	railBlack = theme.Color{R: 0x14, G: 0x14, B: 0x14}
	railRed   = theme.Color{R: 0xeb, G: 0x00, B: 0x00}
	railRim   = theme.Color{R: 0x2b, G: 0x2d, B: 0x30}
)

type numerals struct {
	text   []string
	size   float32
	radius float32
}

var arabic = numerals{text: []string{"12", "3", "6", "9"}, size: 0.19, radius: 0.32}

func Analog(seconds bool) View { return dial(arabic, seconds) }

func dial(marks numerals, seconds bool) View {
	return func(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
		hour, minute, ok := clockHands(r)
		if !ok {
			return Plain(w, box, r, ink)
		}
		text := widgets.Color(ink.Text)
		muted := widgets.Color(ink.Muted)
		accent := widgets.Color(ink.Accent)
		return dialCanvas(
			"analog",
			box,
			r,
			ink,
			seconds,
			func(dc *gogui.DrawContext, cx, cy, d float32) {
				for i := range 60 {
					deg := float64(i) * 6
					if i%5 == 0 {
						dc.FilledPolygon(blade(cx, cy, d*0.43, d*0.49, d*0.016, d*0.016, deg), text)
						continue
					}
					dc.FilledPolygon(blade(cx, cy, d*0.465, d*0.49, d*0.006, d*0.006, deg), muted)
				}

				num := Lettering(glyph.TypefaceBold, d*marks.size, ink.Text)
				nh := dc.FontHeight(num)
				step := 360 / float64(len(marks.text))
				for i, label := range marks.text {
					dx, dy := heading(float64(i) * step)
					x, y := cx+d*marks.radius*dx, cy+d*marks.radius*dy
					dc.Text(x-dc.TextWidth(label, num)/2, y-nh/2, label, num)
				}

				shade := widgets.Color(ink.Text.Blend(ink.Background, 0.35))
				for _, hand := range []struct {
					length, width float32
					deg           float64
				}{
					{d * 0.26, d * 0.062, hourAngle(hour, minute)},
					{d * 0.42, d * 0.046, float64(minute) * 6},
				} {
					dc.FilledPolygon(taper(cx, cy, hand.length, hand.width, d*0.07, hand.deg), text)
					dc.FilledPolygon(
						facet(cx, cy, hand.length, hand.width, d*0.07, hand.deg),
						shade,
					)
				}
				dc.FilledCircle(cx, cy, d*0.042, text)
				if seconds {
					deg := float64(r.Second) * 6
					dc.FilledPolygon(blade(cx, cy, -d*0.11, d*0.46, d*0.016, d*0.008, deg), accent)
					dc.FilledCircle(cx, cy, d*0.026, accent)
				}
				dc.FilledCircle(cx, cy, d*0.009, widgets.Color(ink.Background))
			},
		)
	}
}

func Railway(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	hour, minute, ok := clockHands(r)
	if !ok {
		return Plain(w, box, r, ink)
	}
	black, red := widgets.Color(railBlack), widgets.Color(railRed)
	return dialCanvas("railway", box, r, ink, true, func(dc *gogui.DrawContext, cx, cy, d float32) {
		rad := d / 2
		dc.FilledCircle(cx, cy, rad, widgets.Color(railRim))
		dc.FilledCircle(cx, cy, rad*0.955, widgets.Color(railFace))
		for i := range 60 {
			deg := float64(i) * 6
			if i%5 == 0 {
				dc.FilledPolygon(
					blade(cx, cy, rad*0.70, rad*0.92, rad*0.072, rad*0.072, deg),
					black,
				)
				continue
			}
			dc.FilledPolygon(blade(cx, cy, rad*0.855, rad*0.92, rad*0.024, rad*0.024, deg), black)
		}

		dc.FilledPolygon(
			blade(cx, cy, -rad*0.22, rad*0.62, rad*0.11, rad*0.085, hourAngle(hour, minute)),
			black,
		)
		dc.FilledPolygon(
			blade(cx, cy, -rad*0.22, rad*0.90, rad*0.085, rad*0.062, float64(minute)*6),
			black,
		)

		deg := float64(r.Second) * 6
		dc.FilledPolygon(blade(cx, cy, -rad*0.30, rad*0.58, rad*0.022, rad*0.022, deg), red)
		dx, dy := heading(deg)
		dc.FilledCircle(cx+dx*rad*0.60, cy+dy*rad*0.60, rad*0.098, red)
		dc.FilledCircle(cx, cy, rad*0.032, red)
	})
}

func dialCanvas(
	id string,
	box ui.Rect,
	r Reading,
	ink theme.Theme,
	seconds bool,
	draw func(dc *gogui.DrawContext, cx, cy, d float32),
) gogui.View {
	bw, bh := float32(box.W), float32(box.H)
	d := min(bw, bh) * dialShare
	return gogui.DrawCanvas(gogui.DrawCanvasCfg{
		ID:      id,
		Width:   bw,
		Height:  bh,
		Version: canvasVersion(box, r, ink, seconds),
		OnDraw: func(dc *gogui.DrawContext) {
			date := Lettering(glyph.TypefaceRegular, d*0.075, ink.Muted)
			dateH, under := float32(0), float32(0)
			if r.Dated() {
				dateH, under = dc.FontHeight(date), d*0.10
			}
			cx := bw / 2
			cy := (bh-(d+under+dateH))/2 + d/2
			draw(dc, cx, cy, d)
			if r.Dated() {
				dc.Text(cx-dc.TextWidth(r.Date, date)/2, cy+d/2+under, r.Date, date)
			}
		},
	})
}

func hourAngle(hour, minute int) float64 { return float64(hour%12)*30 + float64(minute)*0.5 }

func along(cx, cy float32, deg float64, pts ...float32) []float32 {
	dx, dy := heading(deg)
	px, py := -dy, dx
	out := make([]float32, len(pts))
	for i := 0; i+1 < len(pts); i += 2 {
		a, c := pts[i], pts[i+1]
		out[i] = cx + dx*a + px*c
		out[i+1] = cy + dy*a + py*c
	}
	return out
}

func blade(cx, cy, from, to, wideFrom, wideTo float32, deg float64) []float32 {
	return along(cx, cy, deg, from, wideFrom/2, to, wideTo/2, to, -wideTo/2, from, -wideFrom/2)
}

func taper(cx, cy, length, width, tail float32, deg float64) []float32 {
	h := width / 2
	return along(cx, cy, deg,
		-tail, h*0.7,
		0, h,
		length*0.86, h*0.42,
		length, 0,
		length*0.86, -h*0.42,
		0, -h,
		-tail, -h*0.7,
	)
}

func facet(cx, cy, length, width, tail float32, deg float64) []float32 {
	h := width / 2
	return along(cx, cy, deg,
		-tail, 0,
		length, 0,
		length*0.86, -h*0.42,
		0, -h,
		-tail, -h*0.7,
	)
}
