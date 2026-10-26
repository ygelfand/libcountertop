package widgets

import (
	"fmt"
	"strings"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/icons"
	"github.com/ygelfand/libcountertop/pkg/display/interaction"
	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/style"
)

type RowRenderer struct {
	Field   func(string, menu.Row) gogui.View
	Preview func(menu.Row) gogui.View
	Grip    func(gogui.View) gogui.View
}

func icon(glyph string, c gogui.Color) gogui.View {
	st := gogui.CurrentTheme().TextStyleIconMedium
	st.Color = c
	if name, ok := strings.CutPrefix(glyph, "mdi:"); ok {
		st.Family = icons.Family
		glyph = icons.Glyph(icons.Named(name))
	}
	return gogui.Label(glyph, st)
}

func (kit *Toolkit) Row(
	id, page string,
	r menu.Row,
	tap func(int),
	glyphs bool,
	render RowRenderer,
) gogui.View {
	label := []gogui.View{gogui.Label(r.Label, gogui.TextStyle{})}
	if r.Hint != "" {
		label = append(label, gogui.Label(r.Hint, Secondary()))
	}
	head := gogui.Column(
		gogui.ContainerCfg{Sizing: gogui.FillFit, Padding: gogui.NoPadding, Content: label},
	)

	content := []gogui.View{head}
	if glyphs {
		var mark []gogui.View
		if r.Glyph != "" {
			mark = []gogui.View{icon(r.Glyph, gogui.CurrentTheme().Cfg.ColorTextSecondary)}
		}
		content = []gogui.View{gogui.Row(gogui.ContainerCfg{
			Width:   style.Reach(),
			Sizing:  gogui.FixedFit,
			Padding: gogui.NoPadding,
			HAlign:  gogui.HAlignCenter,
			Content: mark,
		}), head}
	}
	var onClick func(gogui.EventCtx)
	switch r.Kind {
	case menu.Toggle:
		content = append(content, kit.controls().Toggle(id+"-switch", r.On))
		onClick = interaction.Tapped(tap, 0)
	case menu.Slider:
		snap := r.Snap
		says := r.Value
		if says == "" {
			says = fmt.Sprintf("%d%%", r.Level)
		}
		content = append(content, gogui.Row(gogui.ContainerCfg{
			Padding: gogui.NewPadding(0, style.Reach()*0.4, 0, 0),
			Content: []gogui.View{gogui.Label(says, Secondary())},
		}))
		level := gogui.SliderCfg{
			ID:     id + "-slider",
			Value:  float32(r.Level),
			Min:    0,
			Max:    100,
			Sizing: gogui.FillFit,
			Height: style.Reach(),
			OnChange: func(v float32, e gogui.EventCtx) {
				level := int(v + 0.5)
				if snap != nil {
					level = snap(level)
				}
				if tap != nil {
					tap(level)
				}
				e.Window.InvalidateLayout()
			},
		}
		content = append(content, render.Grip(kit.Slider(level)))
	case menu.Field, menu.Password:
		content = append(content, render.Field(page+"/"+id, r))
	case menu.Segments:
		content = append(content, kit.segments(id, r, tap))
	case menu.Chevron:
		if r.Value != "" {
			content = append(content, gogui.Label(r.Value, Secondary()))
		}
		content = append(
			content,
			icon(gogui.IconArrowRight, gogui.CurrentTheme().Cfg.ColorTextSecondary),
		)
		onClick = interaction.Tapped(tap, 0)
	default:
		if r.Value != "" {
			content = append(content, gogui.Label(r.Value, Secondary()))
		}
		if r.Preview != nil {
			content = append(content, render.Preview(r))
		}
		if r.Chosen {
			content = append(content, icon(gogui.IconCheck, gogui.CurrentTheme().Cfg.ColorAccent))
		}
		onClick = interaction.Tapped(tap, 0)
	}
	cfg := gogui.ContainerCfg{
		ID:       id,
		Sizing:   gogui.FillFit,
		VAlign:   gogui.VAlignMiddle,
		Padding:  gogui.PaddingMedium,
		Spacing:  gogui.SpacingMedium,
		Radius:   gogui.RadiusMedium,
		Disabled: r.Dim,
		Content:  content,
	}
	kit.ListRow(&cfg, Chosen(r.Kind == menu.Toggle && r.Chosen))
	return kit.Pressable(gogui.Row, cfg, onClick)
}

func (kit *Toolkit) Tile(
	id, label string,
	selected bool,
	content []gogui.View,
	tap func(int),
	edit func(),
) gogui.View {
	t := gogui.CurrentTheme().Cfg
	content = append(content, kit.tileLabel(id, label, edit))
	cfg := gogui.ContainerCfg{
		ID:         id,
		HAlign:     gogui.HAlignCenter,
		Padding:    gogui.PaddingSmall,
		Spacing:    gogui.SpacingSmall,
		Radius:     gogui.RadiusMedium,
		SizeBorder: gogui.BorderPx(3),
		Clip:       true,
		Content:    content,
	}
	if selected {
		cfg.ColorBorder = t.ColorAccent
	}
	rest := cfg
	kit.controls().Tile(&rest, Chosen(false))
	kit.controls().Tile(&cfg, Chosen(selected))
	if grow := cfg.SizeBorder.Or(0) - rest.SizeBorder.Or(0); grow > 0 {
		p := cfg.Padding
		cfg.Padding = gogui.NewPadding(p.Top-grow, p.Right-grow, p.Bottom-grow, p.Left-grow)
	}
	return kit.Pressable(gogui.Column, cfg, interaction.Tapped(tap, 0))
}

func (kit *Toolkit) tileLabel(id, label string, edit func()) gogui.View {
	if edit == nil {
		return gogui.Label(label, gogui.TextStyle{})
	}
	return gogui.Row(gogui.ContainerCfg{
		Sizing:  gogui.FillFit,
		Padding: gogui.NoPadding,
		VAlign:  gogui.VAlignMiddle,
		Content: []gogui.View{
			gogui.Label(label, gogui.TextStyle{}),
			gogui.Row(gogui.ContainerCfg{Sizing: gogui.FillFit, Padding: gogui.NoPadding}),
			kit.KeyButton(
				id+"-edit",
				gogui.IconEdit,
				gogui.CurrentTheme().Cfg.TextStyleDef,
				Chosen(false),
				func(gogui.EventCtx) { edit() },
			),
		},
	})
}

func (kit *Toolkit) segments(id string, r menu.Row, tap func(int)) gogui.View {
	var chips []gogui.View
	for i, option := range r.Options {
		st := gogui.CurrentTheme().Cfg.TextStyleDef
		cfg := gogui.ContainerCfg{
			ID:      fmt.Sprintf("%s-segment-%d", id, i),
			Padding: gogui.PaddingMedium,
			VAlign:  gogui.VAlignMiddle,
		}
		kit.Tab(&cfg, &st, Chosen(i == r.Level))
		cfg.Content = []gogui.View{gogui.Label(option, st)}
		if i < len(r.Marks) {
			if mark := markView(
				fmt.Sprintf("%s-mark-%d", id, i),
				r.Marks[i],
				st.Size*1.1,
			); mark != nil {
				cfg.Spacing = gogui.SpacingSmall
				cfg.Content = append([]gogui.View{mark}, cfg.Content...)
			}
		}
		chips = append(chips, kit.Pressable(gogui.Row, cfg, func(e gogui.EventCtx) {
			if tap != nil && i != r.Level {
				tap(i)
			}
			e.Consume()
			e.Window.InvalidateLayout()
		}))
	}
	return gogui.Row(gogui.ContainerCfg{
		Padding: gogui.NoPadding,
		Spacing: gogui.SpacingSmall,
		Content: chips,
	})
}

func markView(id string, m menu.Mark, side float32) gogui.View {
	switch {
	case m.SVG != "":
		return gogui.Svg(gogui.SvgCfg{ID: id, SvgData: m.SVG, Width: side, Height: side})
	case m.Image != nil:
		b := m.Image.Bounds()
		width := side * float32(b.Dx()) / float32(b.Dy())
		src := ImageSource(fmt.Sprintf("mark/%p", m.Image), m.Image)
		return gogui.Image(gogui.ImageCfg{ID: id, Src: src, Width: width, Height: side})
	}
	return nil
}
