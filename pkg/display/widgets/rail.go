package widgets

import (
	"fmt"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/icons"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type RailEntry struct {
	Label, Glyph string
	Open         func()
}
type RailOptions struct {
	Entries []RailEntry
	Edge    schema.Edge
	Close   func()
}

func RailDepth() float32 {
	return style.Reach()*1.5 + 2*gogui.PaddingSmall.Top + gogui.PaddingMedium.Top
}

func (kit *Toolkit) Rail(w *gogui.Window, options RailOptions) gogui.View {
	t := gogui.CurrentTheme()
	vw, vh := w.WindowSize()
	close := options.Close

	mark := t.TextStyleIconLarge
	mark.Color = t.Cfg.TextStyleDef.Color
	mark.Size = style.Reach() * 0.9
	mark.Family = icons.Family
	cell := style.Reach() * 1.5
	var buttons []gogui.View
	for i, e := range options.Entries {
		open := e.Open
		cfg := gogui.ContainerCfg{
			ID:      fmt.Sprintf("rail-%d", i),
			Width:   cell,
			Height:  cell,
			Sizing:  gogui.FixedFixed,
			Padding: gogui.NoPadding,
			HAlign:  gogui.HAlignCenter,
			VAlign:  gogui.VAlignMiddle,
			Radius:  gogui.RadiusMedium,
		}
		st := t.Cfg.TextStyleDef
		if e.Glyph != "" {
			st = mark
		}
		kit.controls().Key(&cfg, &st, style.Rest)
		face := gogui.Label(e.Label, st)
		if e.Glyph != "" {
			face = gogui.Label(e.Glyph, st)
		}
		cfg.Content = []gogui.View{face}
		buttons = append(buttons, kit.Pressable(gogui.Row, cfg, func(gogui.EventCtx) {
			if close != nil {
				close()
			}
			open()
		}))
	}

	edge := options.Edge
	across := edge == schema.EdgeTop || edge == schema.EdgeBottom
	strip := kit.Panel(gogui.ContainerCfg{
		ID:      "rail",
		Padding: gogui.PaddingSmall,
		Spacing: gogui.SpacingSmall,
		Shadow:  &gogui.BoxShadow{Color: gogui.Black.WithOpacity(0.55), OffsetY: 6, BlurRadius: 24},
		OnClick: func(e gogui.EventCtx) { e.Consume() },
		Content: buttons,
	})
	bar := gogui.Column(strip)
	if across {
		bar = gogui.Row(strip)
	}

	cfg := gogui.ContainerCfg{
		ID:      "rail-away",
		Width:   float32(vw),
		Height:  float32(vh),
		Sizing:  gogui.FixedFixed,
		Padding: gogui.PaddingMedium,
		HAlign:  gogui.HAlignRight,
		VAlign:  gogui.VAlignMiddle,
		Content: []gogui.View{bar},
	}
	switch edge {
	case schema.EdgeLeft:
		cfg.HAlign = gogui.HAlignLeft
	case schema.EdgeTop:
		cfg.HAlign, cfg.VAlign = gogui.HAlignCenter, gogui.VAlignTop
	case schema.EdgeBottom:
		cfg.HAlign, cfg.VAlign = gogui.HAlignCenter, gogui.VAlignBottom
	}
	if close == nil {
		return gogui.Column(cfg)
	}
	return kit.Pressable(gogui.Column, cfg, func(gogui.EventCtx) { close() })
}
