package clockview

import (
	"fmt"
	"hash/fnv"
	"math"

	"github.com/go-gui-org/go-glyph"
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
)

type View func(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View

func Lettering(weight glyph.Typeface, size float32, c theme.Color) gogui.TextStyle {
	st := gogui.CurrentTheme().Cfg.TextStyleDef
	st.Typeface, st.Size, st.Color = weight, max(size, 1), widgets.Color(c)
	return st
}

func Fitted(w *gogui.Window, text string, st gogui.TextStyle, maxW, maxH float32) float32 {
	size := max(maxH, 1)
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

func centred(content ...gogui.View) gogui.ContainerCfg {
	return gogui.ContainerCfg{
		Sizing:  gogui.FillFill,
		Padding: gogui.NoPadding,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Content: content,
	}
}

func dateLine(r Reading, size float32, ink theme.Theme) []gogui.View {
	if !r.Dated() {
		return nil
	}
	return []gogui.View{gogui.Label(r.Date, Lettering(glyph.TypefaceRegular, size, ink.Muted))}
}

func Plain(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	big := Lettering(glyph.TypefaceBold, 0, ink.Text)
	size := Fitted(w, r.Time+r.Suffix, big, float32(box.W)*0.86, float32(box.H)*0.34)
	big.Size = size

	line := []gogui.View{gogui.Label(r.Time, big)}
	if r.Suffix != "" {
		line = append(
			line,
			gogui.Label(r.Suffix, Lettering(glyph.TypefaceBold, size*0.24, ink.Muted)),
		)
	}
	cfg := centred(append([]gogui.View{
		gogui.Row(
			gogui.ContainerCfg{
				Padding: gogui.NoPadding,
				Spacing: gogui.SpacingPx(size * 0.08),
				Content: line,
			},
		),
	}, dateLine(r, size*0.20, ink)...)...)
	cfg.Spacing = gogui.SpacingPx(size * 0.1)
	return gogui.Column(cfg)
}

func Stack(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	hour, minute, ok := split(r)
	if !ok {
		return Plain(w, box, r, ink)
	}
	big := Lettering(glyph.TypefaceBold, 0, ink.Text)
	size := Fitted(w, hour, big, float32(box.W)*0.80, float32(box.H)*0.34)
	big.Size = size
	tight := gogui.SpacingPx(-size * 0.22)

	top := []gogui.View{gogui.Label(hour, big)}
	if r.Suffix != "" {
		top = append(
			top,
			gogui.Label(r.Suffix, Lettering(glyph.TypefaceBold, size*0.22, ink.Muted)),
		)
	}
	digits := gogui.Column(
		gogui.ContainerCfg{Padding: gogui.NoPadding, Spacing: tight, Content: []gogui.View{
			gogui.Row(
				gogui.ContainerCfg{
					Padding: gogui.NoPadding,
					Spacing: gogui.SpacingPx(size * 0.06),
					Content: top,
				},
			),
			gogui.Label(minute, big),
		}},
	)
	block := append([]gogui.View{digits}, dateLine(r, size*0.13, ink)...)
	return gogui.Column(
		centred(
			gogui.Column(
				gogui.ContainerCfg{
					Padding: gogui.NoPadding,
					Spacing: gogui.SpacingPx(size * 0.08),
					Content: block,
				},
			),
		),
	)
}

func Cards(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	hour, minute, ok := split(r)
	if !ok {
		return Plain(w, box, r, ink)
	}
	across := box.W >= box.H
	long, short := float32(box.W)*0.88, float32(box.H)*0.74
	if !across {
		long, short = float32(box.H)*0.74, float32(box.W)*0.88
	}
	side := min(short, long/2.055)
	gap := side * 0.055

	card := func(digits string, corner string) gogui.View {
		st := Lettering(glyph.TypefaceBold, 0, ink.Text)
		st.Size = Fitted(w, "00", st, side*0.74, side*0.54)
		content := []gogui.View{gogui.Label(digits, st)}
		if corner != "" {
			content = append(content, gogui.Column(gogui.ContainerCfg{
				Float:        true,
				FloatAnchor:  gogui.FloatTopRight,
				FloatTieOff:  gogui.FloatTopRight,
				FloatOffsetX: -side * 0.09,
				FloatOffsetY: side * 0.09,
				Padding:      gogui.NoPadding,
				Content: []gogui.View{
					gogui.Label(corner, Lettering(glyph.TypefaceBold, side*0.15, ink.Muted)),
				},
			}))
		}
		return gogui.Column(gogui.ContainerCfg{
			Width:  side,
			Height: side,
			Sizing: gogui.FixedFixed,
			HAlign: gogui.HAlignCenter,
			VAlign: gogui.VAlignMiddle,
			Color: widgets.Color(
				ink.Surface),

			Radius:  gogui.RadiusPx(side * 0.11),
			Padding: gogui.NoPadding,
			Content: content,
		})
	}
	pair := []gogui.View{card(hour, r.Suffix), card(minute, "")}
	var block gogui.View
	if across {
		block = gogui.Row(
			gogui.ContainerCfg{
				Padding: gogui.NoPadding,
				Spacing: gogui.SpacingPx(gap),
				Content: pair,
			},
		)
	} else {
		block = gogui.Column(
			gogui.ContainerCfg{
				Padding: gogui.NoPadding,
				Spacing: gogui.SpacingPx(gap),
				Content: pair,
			},
		)
	}
	cfg := centred(append([]gogui.View{block}, dateLine(r, side*0.085, ink)...)...)
	cfg.Spacing = gogui.SpacingPx(side * 0.16)
	return gogui.Column(cfg)
}

func heading(deg float64) (dx, dy float32) {
	rad := deg * math.Pi / 180
	return float32(math.Sin(rad)), float32(-math.Cos(rad))
}

func Words(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	hour, minute, ok := clockHands(r)
	if !ok {
		return Plain(w, box, r, ink)
	}
	said := Say(hour, minute)
	big := Lettering(glyph.TypefaceBold, 0, ink.Text)
	longest := ""
	for _, line := range said {
		if len(line) > len(longest) {
			longest = line
		}
	}
	size := Fitted(w, longest, big, float32(box.W)*0.86, float32(box.H)*0.26)
	big.Size = size

	var lines []gogui.View
	for _, line := range said {
		lines = append(lines, gogui.Label(line, big))
	}
	block := gogui.Column(
		gogui.ContainerCfg{
			Padding: gogui.NoPadding,
			Spacing: gogui.SpacingPx(-size * 0.1),
			Content: lines,
		},
	)
	cfg := centred(append([]gogui.View{block}, dateLine(r, size*0.26, ink)...)...)
	cfg.Spacing = gogui.SpacingPx(size * 0.3)
	return gogui.Column(cfg)
}

func Overlap(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	hour, minute, ok := split(r)
	if !ok {
		return Plain(w, box, r, ink)
	}
	big := Lettering(glyph.TypefaceBold, 0, ink.Text)
	size := Fitted(w, hour, big, float32(box.W)*0.78/1.18, float32(box.H)*0.46)
	big.Size = size
	faint := big
	faint.Color = widgets.Color(ink.Muted)

	top := []gogui.View{gogui.Label(hour, faint)}
	if r.Suffix != "" {
		top = append(top, gogui.Label(r.Suffix, Lettering(glyph.TypefaceBold, size*0.20, ink.Text)))
	}
	pair := gogui.Column(
		gogui.ContainerCfg{
			Padding: gogui.NoPadding,
			Spacing: gogui.SpacingPx(-size * 0.45),
			Content: []gogui.View{
				gogui.Row(gogui.ContainerCfg{Padding: gogui.NoPadding, Content: top}),
				gogui.Row(
					gogui.ContainerCfg{
						Padding: gogui.NewPadding(0, 0, 0, size*0.18),
						Content: []gogui.View{gogui.Label(minute, big)},
					},
				),
			},
		},
	)
	cfg := centred(append([]gogui.View{pair}, dateLine(r, size*0.13, ink)...)...)
	cfg.Spacing = gogui.SpacingPx(size * 0.1)
	return gogui.Column(cfg)
}

func canvasVersion(box ui.Rect, r Reading, ink theme.Theme, seconds bool) uint64 {
	second := 0
	if seconds {
		second = r.Second
	}
	h := fnv.New64a()
	fmt.Fprintf(
		h,
		"%s|%s|%d|%dx%d|%v|%v",
		r.Time,
		r.Date,
		second,
		box.W,
		box.H,
		ink,
		gogui.CurrentTheme().Cfg.TextStyleDef,
	)
	return h.Sum64()
}
