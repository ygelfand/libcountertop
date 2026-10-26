package clockview

import (
	"github.com/go-gui-org/go-glyph"
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
)

const (
	nixieTall  = 1.75
	nixieGap   = 0.14
	nixieColon = 0.42
	nixieBody  = 0.9
)

var (
	neon      = theme.Color{R: 255, G: 122, B: 28}
	neonWhite = theme.Color{R: 255, G: 236, B: 214}
)

func Nixie(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	hour, minute, ok := split(r)
	if !ok {
		return Plain(w, box, r, ink)
	}
	bw, bh := float32(box.W), float32(box.H)
	across := bw >= bh
	var tw, width, height float32
	if across {
		tw = min(bw*0.9/(4+2*nixieGap+nixieColon), bh*0.6/nixieTall)
		width, height = tw*(4+2*nixieGap+nixieColon), tw*nixieTall
	} else {
		tw = min(bw*0.8/(2+nixieGap), bh*0.66/(2*nixieTall+2*nixieGap))
		width, height = tw*(2+nixieGap), tw*(2*nixieTall+2*nixieGap)
	}
	th := tw * nixieTall

	tubes := gogui.DrawCanvas(gogui.DrawCanvasCfg{
		ID: "nixie", Width: width, Height: height,
		Version: canvasVersion(box, r, ink, false),
		OnDraw: func(dc *gogui.DrawContext) {
			st := Lettering(glyph.TypefaceRegular, th*0.6, neon)
			if wide := dc.TextWidth("0", st); wide > tw*0.6 {
				st.Size *= tw * 0.6 / wide
			}
			pair := func(x, y float32, digits string) {
				tube(dc, x, y, tw, th, digits[0], st, ink)
				tube(dc, x+tw*(1+nixieGap), y, tw, th, digits[1], st, ink)
			}
			pair(0, 0, hour)
			if !across {
				pair(0, th+2*tw*nixieGap, minute)
				return
			}
			left := tw * (2 + nixieGap)
			pair(left+tw*(nixieColon+nixieGap), 0, minute)
			cx, cy := left+tw*(nixieColon+nixieGap)/2, th*nixieBody*0.54
			for _, dy := range []float32{-th * 0.14, th * 0.14} {
				lamp(dc, cx, cy+dy, tw*0.05)
			}
		},
	})
	content := []gogui.View{tubes}
	if r.Suffix != "" {
		content = append(
			content,
			gogui.Label(r.Suffix, Lettering(glyph.TypefaceBold, th*0.1, neon)),
		)
	}
	cfg := centred(append(content, dateLine(r, th*0.11, ink)...)...)
	cfg.Spacing = gogui.SpacingPx(th * 0.12)
	return gogui.Column(cfg)
}

func tube(
	dc *gogui.DrawContext,
	x, y, tw, th float32,
	digit byte,
	st gogui.TextStyle,
	ink theme.Theme,
) {
	body := th * nixieBody
	glass := widgets.Color(ink.Background.Blend(ink.Text, 0.07))
	dc.FilledRoundedRect(x, y, tw, body, tw*0.48, glass)
	dc.FilledRoundedRect(x, y+body*0.5, tw, body*0.5, tw*0.1, glass)
	dc.FilledRoundedRect(
		x-tw*0.05,
		y+body,
		tw*1.1,
		th-body,
		tw*0.05,
		widgets.Color(ink.Surface.Blend(ink.Muted, 0.35)),
	)

	cx, cy := x+tw/2, y+body*0.54
	fh := dc.FontHeight(st)
	at := func(text string, dx, dy float32, c gogui.Color) {
		s := st
		s.Color = c
		dc.Text(cx-dc.TextWidth(text, s)/2+dx, cy-fh/2+dy, text, s)
	}
	ghost := widgets.Color(neon).WithOpacity(0.08)
	for d := byte('0'); d <= '9'; d++ {
		if d != digit {
			at(string(d), 0, 0, ghost)
		}
	}
	dc.FilledCircleGradient(cx, cy, tw*0.62, &gogui.CanvasGradient{
		Radial: true,
		Stops: []gogui.GradientStop{
			{Color: widgets.Color(neon).WithOpacity(0.3), Pos: 0},
			{Color: widgets.Color(neon).WithOpacity(0), Pos: 1},
		},
	})
	halo, spread := widgets.Color(neon).WithOpacity(0.35), tw*0.018
	for _, o := range [][2]float32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {0.7, 0.7}, {-0.7, 0.7}, {0.7, -0.7}, {-0.7, -0.7}} {
		at(string(digit), o[0]*spread, o[1]*spread, halo)
	}
	at(string(digit), 0, 0, widgets.Color(neon.Blend(neonWhite, 0.35)))
}

func lamp(dc *gogui.DrawContext, cx, cy, radius float32) {
	dc.FilledCircleGradient(cx, cy, radius*3, &gogui.CanvasGradient{
		Radial: true,
		Stops: []gogui.GradientStop{
			{Color: widgets.Color(neon).WithOpacity(0.35), Pos: 0},
			{Color: widgets.Color(neon).WithOpacity(0), Pos: 1},
		},
	})
	dc.FilledCircle(cx, cy, radius, widgets.Color(neon.Blend(neonWhite, 0.35)))
}
