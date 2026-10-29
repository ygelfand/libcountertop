// Package panelkit holds the drawing pieces themed full-screen panels share: colors, placement, glyphs and fitted text.
package panelkit

import (
	"math"
	"strings"
	"time"

	"github.com/go-gui-org/go-glyph"
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/icons"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
)

func Hex(v uint32) gogui.Color { return gogui.RGB(uint8(v>>16), uint8(v>>8), uint8(v)) }

func Mix(a, b gogui.Color, k float32) gogui.Color {
	lerp := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*k) }
	return gogui.RGBA(lerp(a.R, b.R), lerp(a.G, b.G), lerp(a.B, b.B), a.A)
}

func Icon(name string, size float32, c gogui.Color) gogui.View {
	st := widgets.IconStyle(c)
	st.Family, st.Size = icons.Family, size
	return gogui.Label(icons.Glyph(icons.Named(name)), st)
}

func Place(x, y, w, h float32, v gogui.View) gogui.View {
	return gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFill,
		Padding: gogui.NewPadding(max(y, 0), 0, 0, max(x, 0)),
		Content: []gogui.View{gogui.Column(gogui.ContainerCfg{
			Width: w, Height: h, Sizing: gogui.FixedFixed, Padding: gogui.NoPadding,
			Content: []gogui.View{v},
		})},
	})
}

func Column(w, h, spacing float32, align gogui.HorizontalAlign, content ...gogui.View) gogui.View {
	return gogui.Column(gogui.ContainerCfg{
		Width: w, Height: h, Sizing: gogui.FixedFixed, Padding: gogui.NoPadding,
		Spacing: gogui.SpacingPx(spacing), HAlign: align, VAlign: gogui.VAlignMiddle,
		Content: content,
	})
}

func Row(w, h, spacing float32, align gogui.HorizontalAlign, content ...gogui.View) gogui.View {
	return gogui.Row(gogui.ContainerCfg{
		Width: w, Height: h, Sizing: gogui.FixedFixed, Padding: gogui.NoPadding,
		Spacing: gogui.SpacingPx(spacing), HAlign: align, VAlign: gogui.VAlignMiddle,
		Content: content,
	})
}

func Stack(w, h float32, content ...gogui.View) gogui.View {
	return gogui.Stack(gogui.ContainerCfg{
		Width: w, Height: h, Sizing: gogui.FixedFixed, Padding: gogui.NoPadding, Content: content,
	})
}

// go-glyph rasterizes glyphs no larger than 256 px.
const GlyphMost = 248

type Type struct {
	Family string
	Upper  bool
	Spaced bool
	Thin   bool
}

func (t Type) Words(s string) string {
	if t.Upper {
		s = strings.ToUpper(s)
	}
	if t.Spaced {
		s = strings.Join(strings.Split(s, ""), " ")
	}
	return s
}

func (t Type) Style(size float32, c gogui.Color, bold bool) gogui.TextStyle {
	st := gogui.CurrentTheme().Cfg.TextStyleDef
	st.Size, st.Color = min(max(size, 1), GlyphMost), c
	if t.Family != "" {
		st.Family = t.Family
	}
	switch {
	case bold:
		st.Typeface = glyph.TypefaceBold
	case t.Thin:
		st.Typeface = glyph.TypefaceRegular
	}
	return st
}

func (t Type) Label(s string, size float32, c gogui.Color, bold bool) gogui.View {
	return gogui.Label(t.Words(s), t.Style(size, c, bold))
}

func (t Type) Fit(w *gogui.Window, s string, maxW, maxH float32, bold bool) float32 {
	st := t.Style(maxH, gogui.White, bold)
	text := t.Words(s)
	size := min(maxH, GlyphMost)
	for range 8 {
		st.Size = size
		wide := w.TextWidth(text, st)
		if wide <= maxW || size <= 1 {
			return size
		}
		size = max(min(size*maxW/wide, size-1), 1)
	}
	return size
}

func (t Type) Fitted(
	w *gogui.Window,
	s string,
	maxW, maxH float32,
	c gogui.Color,
	bold bool,
) gogui.View {
	return t.Label(s, t.Fit(w, s, maxW, maxH, bold), c, bold)
}

func Phase(now time.Time, period time.Duration) float64 {
	return float64(now.UnixNano()%int64(period)) / float64(period)
}

func Breath(now time.Time, period time.Duration) float32 {
	return float32((1 - math.Cos(2*math.Pi*Phase(now, period))) / 2)
}

func Version(parts ...float32) uint64 {
	var v uint64 = 1469598103934665603
	for _, p := range parts {
		v ^= uint64(math.Float32bits(p))
		v *= 1099511628211
	}
	return v
}
