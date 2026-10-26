package clockview

import (
	"github.com/go-gui-org/go-glyph"
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
)

const (
	flipTall  = 1.42
	flipInner = 0.06
	flipPair  = 0.28
	flipRound = 0.08
	flipSplit = 0.016
)

func Flip(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	hour, minute, ok := split(r)
	if !ok {
		return Plain(w, box, r, ink)
	}
	bw, bh := float32(box.W), float32(box.H)
	across := bw >= bh
	pairW := float32(2 + flipInner)
	var tw, width, height float32
	if across {
		tw = min(bw*0.9/(2*pairW+flipPair), bh*0.62/flipTall)
		width, height = tw*(2*pairW+flipPair), tw*flipTall
	} else {
		tw = min(bw*0.86/pairW, bh*0.7/(2*flipTall+flipPair))
		width, height = tw*pairW, tw*(2*flipTall+flipPair)
	}
	th := tw * flipTall

	tiles := gogui.DrawCanvas(gogui.DrawCanvasCfg{
		ID: "flip", Width: width, Height: height,
		Version: canvasVersion(box, r, ink, false),
		OnDraw: func(dc *gogui.DrawContext) {
			st := Lettering(glyph.TypefaceBold, th*0.84, ink.Text)
			if wide := dc.TextWidth("0", st); wide > tw*0.74 {
				st.Size *= tw * 0.74 / wide
			}
			pair := func(x, y float32, digits string) {
				flipTile(dc, x, y, tw, th, digits[:1], st, ink)
				flipTile(dc, x+tw*(1+flipInner), y, tw, th, digits[1:], st, ink)
			}
			pair(0, 0, hour)
			if across {
				pair(tw*(pairW+flipPair), 0, minute)
			} else {
				pair(0, th+tw*flipPair, minute)
			}
			if r.Suffix != "" {
				dc.Text(
					tw*0.09,
					th*0.05,
					r.Suffix,
					Lettering(glyph.TypefaceBold, th*0.09, ink.Muted),
				)
			}
		},
	})
	cfg := centred(append([]gogui.View{tiles}, dateLine(r, th*0.11, ink)...)...)
	cfg.Spacing = gogui.SpacingPx(th * 0.18)
	return gogui.Column(cfg)
}

func flipTile(
	dc *gogui.DrawContext,
	x, y, tw, th float32,
	digit string,
	st gogui.TextStyle,
	ink theme.Theme,
) {
	upper := widgets.Color(ink.Surface.Blend(ink.Text, 0.14))
	gap := widgets.Color(ink.Background)
	dc.FilledRoundedRect(
		x,
		y,
		tw,
		th,
		tw*flipRound,
		widgets.Color(ink.Surface.Blend(ink.Text, 0.09)),
	)
	dc.FilledRoundedRect(x, y, tw, th/2, tw*flipRound, upper)
	dc.FilledRect(x, y+th/4, tw, th/4, upper)

	fh := dc.FontHeight(st)
	dc.Text(x+(tw-dc.TextWidth(digit, st))/2, y+(th-fh)/2, digit, st)

	split := th * flipSplit
	dc.FilledRect(x, y+th/2-split/2, tw, split, gap)
	hw, hh := tw*0.03, th*0.07
	dc.FilledRect(x, y+th/2-hh/2, hw, hh, gap)
	dc.FilledRect(x+tw-hw, y+th/2-hh/2, hw, hh, gap)
}
