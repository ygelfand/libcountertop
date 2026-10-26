package clockview

import (
	"math"

	"github.com/go-gui-org/go-glyph"
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
)

var (
	smileTop    = theme.Color{R: 0xff, G: 0xc9, B: 0x2a}
	smileBottom = theme.Color{R: 0xf5, G: 0xa1, B: 0x00}
	smileInk    = theme.Color{R: 0x12, G: 0x12, B: 0x12}
	smileWhite  = theme.Color{R: 0xff, G: 0xff, B: 0xff}

	slate     = theme.Color{R: 0x23, G: 0x26, B: 0x28}
	slateEdge = theme.Color{R: 0x16, G: 0x18, B: 0x19}
	chalk     = theme.Color{R: 0xf1, G: 0xf1, B: 0xec}
	chalkRed  = theme.Color{R: 0xe0, G: 0x2e, B: 0x2e}

	mathHours = []string{
		"4·3",
		"0!",
		"ln e²",
		"√9",
		"2²",
		"10÷2",
		"3!",
		"15−8",
		"2³",
		"√81",
		"√100",
		"0xB",
	}
)

func Smiley(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	hour, minute, ok := clockHands(r)
	if !ok {
		return Plain(w, box, r, ink)
	}
	black, white := widgets.Color(smileInk), widgets.Color(smileWhite)
	return dialCanvas("smiley", box, r, ink, true, func(dc *gogui.DrawContext, cx, cy, d float32) {
		rad := d / 2
		dc.FilledCircle(cx, cy, rad, black)
		dc.FilledCircleGradient(cx, cy, rad*0.94, vertical(smileTop, smileBottom))
		for i := range 12 {
			dc.FilledPolygon(
				blade(cx, cy, rad*0.74, rad*0.88, rad*0.022, rad*0.022, float64(i)*30),
				white,
			)
		}
		for _, side := range []float32{-1, 1} {
			dc.FilledPolygon(ellipse(cx+side*rad*0.24, cy-rad*0.22, rad*0.075, rad*0.13), black)
		}
		dc.Arc(cx, cy+rad*0.02, rad*0.5, rad*0.42, math.Pi*0.15, math.Pi*0.7, black, rad*0.06)

		dc.FilledPolygon(
			blade(cx, cy, -rad*0.08, rad*0.46, rad*0.05, rad*0.03, hourAngle(hour, minute)),
			white,
		)
		dc.FilledPolygon(
			blade(cx, cy, -rad*0.08, rad*0.7, rad*0.036, rad*0.022, float64(minute)*6),
			white,
		)
		dc.FilledPolygon(
			blade(cx, cy, -rad*0.12, rad*0.8, rad*0.012, rad*0.008, float64(r.Second)*6),
			white,
		)
		dc.FilledCircle(cx, cy, rad*0.04, white)
	})
}

func Math(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	hour, minute, ok := clockHands(r)
	if !ok {
		return Plain(w, box, r, ink)
	}
	white := widgets.Color(chalk)
	return dialCanvas("math", box, r, ink, true, func(dc *gogui.DrawContext, cx, cy, d float32) {
		rad := d / 2
		dc.FilledCircle(cx, cy, rad, widgets.Color(slateEdge))
		dc.FilledCircleGradient(cx, cy, rad*0.97, &gogui.CanvasGradient{
			Radial: true,
			Stops: []gogui.GradientStop{
				{Color: widgets.Color(slate.Blend(chalk, 0.05)), Pos: 0},
				{Color: widgets.Color(slate), Pos: 1},
			},
		})
		st := Lettering(glyph.TypefaceRegular, rad*0.12, chalk)
		st.Color = st.Color.WithOpacity(0.92)
		th := dc.FontHeight(st)
		for i, eq := range mathHours {
			dx, dy := heading(float64(i) * 30)
			x, y := cx+dx*rad*0.72, cy+dy*rad*0.72
			dc.Text(x-dc.TextWidth(eq, st)/2, y-th/2, eq, st)
		}
		dc.FilledPolygon(
			blade(cx, cy, -rad*0.06, rad*0.42, rad*0.04, rad*0.03, hourAngle(hour, minute)),
			white,
		)
		dc.FilledPolygon(
			blade(cx, cy, -rad*0.06, rad*0.62, rad*0.03, rad*0.02, float64(minute)*6),
			white,
		)
		red := widgets.Color(chalkRed)
		dc.FilledPolygon(
			blade(cx, cy, -rad*0.14, rad*0.66, rad*0.012, rad*0.008, float64(r.Second)*6),
			red,
		)
		dc.FilledCircle(cx, cy, rad*0.035, white)
		dc.FilledCircle(cx, cy, rad*0.015, red)
	})
}

func Neon(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	hour, minute, ok := clockHands(r)
	if !ok {
		return Plain(w, box, r, ink)
	}
	return dialCanvas("neon", box, r, ink, false, func(dc *gogui.DrawContext, cx, cy, d float32) {
		rad := d / 2
		glow := func(draw func(width float32, c gogui.Color)) {
			for _, layer := range []struct{ width, alpha float32 }{
				{0.07, 0.07}, {0.045, 0.12}, {0.026, 0.25},
			} {
				draw(rad*layer.width, widgets.Color(ink.Accent).WithOpacity(layer.alpha))
			}
			draw(rad*0.011, widgets.Color(ink.Accent.Blend(ink.Text, 0.45)))
		}
		glow(func(width float32, c gogui.Color) { dc.Circle(cx, cy, rad*0.86, c, width) })
		for i := range 4 {
			deg := float64(i) * 90
			glow(func(width float32, c gogui.Color) {
				dc.FilledPolygon(
					blade(cx, cy, rad*0.64-width/2, rad*0.74+width/2, width, width, deg),
					c,
				)
			})
		}
		glow(func(width float32, c gogui.Color) {
			dc.FilledPolygon(
				blade(cx, cy, -width/2, rad*0.4+width/2, width, width, hourAngle(hour, minute)),
				c,
			)
		})
		glow(func(width float32, c gogui.Color) {
			dc.FilledPolygon(
				blade(cx, cy, -width/2, rad*0.62+width/2, width, width, float64(minute)*6),
				c,
			)
		})
	})
}

func Binary(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	hour, minute, ok := split(r)
	if !ok {
		return Plain(w, box, r, ink)
	}
	second := []byte{byte('0' + r.Second/10), byte('0' + r.Second%10)}
	digits := []byte{hour[0], hour[1], minute[0], minute[1], second[0], second[1]}
	bits := []int{2, 4, 3, 4, 3, 4}
	bw, bh := float32(box.W), float32(box.H)
	cell := min(bw*0.8/(6+5*0.45+2*0.6), bh*0.5/(4+3*0.45))
	gap, pair := cell*0.45, cell*0.6
	width := 6*cell + 5*gap + 2*pair
	height := 4*cell + 3*gap
	on := widgets.Color(ink.Accent)
	off := widgets.Color(ink.Text.Blend(ink.Background, 0.86))
	leds := gogui.DrawCanvas(gogui.DrawCanvasCfg{
		ID: "binary", Width: width, Height: height,
		Version: canvasVersion(box, r, ink, true),
		OnDraw: func(dc *gogui.DrawContext) {
			x := float32(0)
			for col, c := range digits {
				if col > 0 {
					x += gap
					if col%2 == 0 {
						x += pair
					}
				}
				v := int(c - '0')
				for b := range bits[col] {
					y := height - float32(b+1)*cell - float32(b)*gap
					if v>>b&1 == 1 {
						dc.FilledRoundedRect(
							x-cell*0.12,
							y-cell*0.12,
							cell*1.24,
							cell*1.24,
							cell*0.3,
							on.WithOpacity(0.25),
						)
						dc.FilledRoundedRect(x, y, cell, cell, cell*0.18, on)
						continue
					}
					dc.FilledRoundedRect(x, y, cell, cell, cell*0.18, off)
				}
				x += cell
			}
		},
	})
	cfg := centred(append([]gogui.View{leds}, dateLine(r, cell*0.55, ink)...)...)
	cfg.Spacing = gogui.SpacingPx(cell)
	return gogui.Column(cfg)
}

func ellipse(cx, cy, rx, ry float32) []float32 {
	const n = 24
	out := make([]float32, 0, 2*n)
	for i := range n {
		t := 2 * math.Pi * float64(i) / n
		out = append(out, cx+rx*float32(math.Cos(t)), cy+ry*float32(math.Sin(t)))
	}
	return out
}
