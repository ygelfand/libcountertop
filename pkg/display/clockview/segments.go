package clockview

import (
	"github.com/go-gui-org/go-glyph"
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
)

// Segment order a b c d e f g: top, top right, bottom right, bottom, bottom left, top left, middle.
var lit = map[rune][7]bool{
	'0': {true, true, true, true, true, true, false},
	'1': {false, true, true, false, false, false, false},
	'2': {true, true, false, true, true, false, true},
	'3': {true, true, true, true, false, false, true},
	'4': {false, true, true, false, false, true, true},
	'5': {true, false, true, true, false, true, true},
	'6': {true, false, true, true, true, true, true},
	'7': {true, true, true, false, false, false, false},
	'8': {true, true, true, true, true, true, true},
	'9': {true, true, true, true, false, true, true},
}

const (
	segDigit = 0.56
	segColon = 0.26
	segSpace = 0.12
	segThick = 0.14
	segGap   = 0.022
	segTag   = 0.17
)

func Segments(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	hour, minute, ok := split(r)
	if !ok {
		return Plain(w, box, r, ink)
	}
	rows := []string{hour + ":" + minute}
	if box.H > box.W {
		rows = []string{hour, minute}
	}
	tag := Lettering(glyph.TypefaceBold, 100, ink.Text)
	tagUnits := float32(0)
	if r.Suffix != "" {
		tagUnits = w.TextWidth(r.Suffix, tag)/100*segTag + segSpace
	}
	widest := float32(0)
	for i, row := range rows {
		wide := segUnits(row)
		if i == 0 {
			wide += tagUnits
		}
		widest = max(widest, wide)
	}
	bw, bh := float32(box.W), float32(box.H)
	height := min(bw*0.78/widest, bh*0.25)
	if len(rows) > 1 {
		height = min(bw*0.78/widest, bh*0.2)
	}
	gap := height * 0.22
	tall := height*float32(len(rows)) + gap*float32(len(rows)-1)
	tag.Size = height * segTag

	on := widgets.Color(ink.Text)
	off := widgets.Color(ink.Text.Blend(ink.Background, 0.94))
	digits := gogui.DrawCanvas(gogui.DrawCanvasCfg{
		ID: "segments", Width: bw, Height: tall,
		Version: canvasVersion(box, r, ink, false),
		OnDraw: func(dc *gogui.DrawContext) {
			y := float32(0)
			for n, row := range rows {
				wide := segUnits(row)
				if n == 0 {
					wide += tagUnits
				}
				x := (bw - wide*height) / 2
				if n == 0 && r.Suffix != "" {
					dc.Text(x, y+height*0.04, r.Suffix, tag)
					x += tagUnits * height
				}
				for i, c := range row {
					if i > 0 {
						x += height * segSpace
					}
					if c == ':' {
						colon(dc, x, y, height, on)
						x += height * segColon
						continue
					}
					digit(dc, x, y, height, lit[c], on, off)
					x += height * segDigit
				}
				y += height + gap
			}
		},
	})
	cfg := centred(append([]gogui.View{digits}, dateLine(r, height*0.27, ink)...)...)
	cfg.Spacing = gogui.SpacingPx(height * 0.2)
	return gogui.Column(cfg)
}

func segUnits(row string) float32 {
	var wide float32
	for i, c := range row {
		if i > 0 {
			wide += segSpace
		}
		if c == ':' {
			wide += segColon
		} else {
			wide += segDigit
		}
	}
	return wide
}

func digit(dc *gogui.DrawContext, x, y, height float32, lit [7]bool, on, off gogui.Color) {
	t, g := height*segThick, height*segGap
	l, r := x+t/2, x+height*segDigit-t/2
	top, mid, bottom := y+t/2, y+height/2, y+height-t/2
	bars := [7][4]float32{
		{l, top, r, top},
		{r, top, r, mid},
		{r, mid, r, bottom},
		{l, bottom, r, bottom},
		{l, mid, l, bottom},
		{l, top, l, mid},
		{l, mid, r, mid},
	}
	for k, b := range bars {
		paint := off
		if lit[k] {
			paint = on
		}
		dc.FilledPolygon(segment(b[0], b[1], b[2], b[3], t, g), paint)
	}
}

func segment(x0, y0, x1, y1, thick, gap float32) []float32 {
	h := thick / 2
	if y0 == y1 {
		a, b := x0+gap, x1-gap
		return []float32{a, y0, a + h, y0 - h, b - h, y0 - h, b, y0, b - h, y0 + h, a + h, y0 + h}
	}
	a, b := y0+gap, y1-gap
	return []float32{x0, a, x0 + h, a + h, x0 + h, b - h, x0, b, x0 - h, b - h, x0 - h, a + h}
}

func colon(dc *gogui.DrawContext, x, y, height float32, on gogui.Color) {
	side := height * segThick
	left := x + (height*segColon-side)/2
	for _, cy := range []float32{y + height/3, y + height*2/3} {
		dc.FilledRoundedRect(left, cy-side/2, side, side, side*0.22, on)
	}
}
