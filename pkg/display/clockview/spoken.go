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
	spokenFace = theme.Color{R: 0xe8, G: 0xe9, B: 0xeb}
	spokenEdge = theme.Color{R: 0xb4, G: 0xb7, B: 0xbc}
	spokenInk  = theme.Color{R: 0x16, G: 0x17, B: 0x19}
	spokenLit  = theme.Color{R: 0xd8, G: 0x23, B: 0x23}
)

func Spoken(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	hour, minute, ok := clockHands(r)
	if !ok {
		return Plain(w, box, r, ink)
	}
	black := widgets.Color(spokenInk)
	return dialCanvas("spoken", box, r, ink, false, func(dc *gogui.DrawContext, cx, cy, d float32) {
		rad := d / 2
		dc.FilledCircleGradient(cx, cy, rad, vertical(steelLight, steelDark))
		dc.FilledCircleGradient(cx, cy, rad*0.95, vertical(spokenFace, spokenEdge))

		word := Lettering(glyph.TypefaceBold, rad*0.1, spokenInk)
		word.LetterSpacing = rad * 0.012
		for i, name := range hourNames {
			radialText(dc, cx, cy, float64(i)*30, rad*0.66, name, word)
		}

		deg := hourAngle(hour, minute)
		near := int(math.Round(deg/30)) % 12
		lit := word
		lit.Color = widgets.Color(spokenLit)
		radialText(dc, cx, cy, float64(near)*30, rad*0.66, hourNames[near], lit)
		frame := blade(cx, cy, -rad*0.06, rad*0.92, rad*0.17, rad*0.17, deg)
		frame = append(frame, frame[0], frame[1], frame[2], frame[3])
		dc.PolylineJoined(frame, black, rad*0.014)
		dc.FilledPolygon(
			blade(cx, cy, -rad*0.1, rad*0.9, rad*0.022, rad*0.014, float64(minute)*6),
			black,
		)
		dc.FilledCircle(cx, cy, rad*0.05, black)
	})
}

var hourNames = []string{
	"TWELVE", "ONE", "TWO", "THREE", "FOUR", "FIVE",
	"SIX", "SEVEN", "EIGHT", "NINE", "TEN", "ELEVEN",
}

func radialText(
	dc *gogui.DrawContext,
	cx, cy float32,
	deg float64,
	at float32,
	text string,
	st gogui.TextStyle,
) {
	turn := deg - 90
	if math.Mod(deg+360, 360) > 180 {
		turn = deg + 90
	}
	rad := turn * math.Pi / 180
	ux, uy := float32(math.Cos(rad)), float32(math.Sin(rad))
	vx, vy := -uy, ux
	dx, dy := heading(deg)
	mx, my := cx+dx*at, cy+dy*at
	tw, th := dc.TextWidth(text, st), dc.FontHeight(st)
	st.RotationRadians = float32(rad)
	dc.Text(mx-ux*tw/2-vx*th/2, my-uy*tw/2-vy*th/2, strings.ToUpper(text), st)
}
