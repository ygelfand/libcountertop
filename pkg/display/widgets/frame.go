package widgets

import (
	"fmt"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/icons"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type FrameOptions struct {
	Title    string
	Fixed    bool
	Build    func(*gogui.Window) gogui.View
	Foot     func(*gogui.Window) gogui.View
	Back     func()
	Settings func()
	Gesture  func(gogui.EventCtx)
	Room     []gogui.View
}

func Placed(at ui.Rect, v gogui.View) gogui.View {
	return gogui.Column(gogui.ContainerCfg{
		Float:        true,
		FloatOffsetX: float32(at.X),
		FloatOffsetY: float32(at.Y),
		Width:        float32(at.W),
		Height:       float32(at.H),
		Sizing:       gogui.FixedFixed,
		Padding:      gogui.NoPadding,
		Content:      []gogui.View{v},
	})
}

func HAligned(a schema.Align) gogui.HorizontalAlign {
	switch a {
	case schema.AlignLeft:
		return gogui.HAlignLeft
	case schema.AlignRight:
		return gogui.HAlignRight
	}
	return gogui.HAlignCenter
}

func Aligned(p schema.Placement, cfg gogui.ContainerCfg) gogui.ContainerCfg {
	cfg.HAlign, cfg.VAlign = HAligned(p.Align), gogui.VAlignMiddle
	switch p.Position {
	case schema.PositionTop:
		cfg.VAlign = gogui.VAlignTop
	case schema.PositionBottom:
		cfg.VAlign = gogui.VAlignBottom
	}
	return cfg
}

func Floating(z int, v gogui.View) gogui.View {
	return gogui.Column(
		gogui.ContainerCfg{
			Float:       true,
			FloatZIndex: z + 1,
			Sizing:      gogui.FillFill,
			Padding:     gogui.NoPadding,
			Content:     []gogui.View{v},
		},
	)
}

func (kit *Toolkit) PageFrame(w *gogui.Window, p FrameOptions) gogui.View {
	t := gogui.CurrentTheme()
	back := t.TextStyleIconMedium
	back.Color = t.TextStyleDisplay.Color
	back.Size = t.TextStyleDisplay.Size * 0.6
	var foot []gogui.View
	if p.Foot != nil {
		foot = append(foot, p.Foot(w))
	}
	head := []gogui.View{
		kit.KeyButton(
			"back",
			gogui.IconArrowLeft,
			back,
			style.Rest,
			func(e gogui.EventCtx) {
				p.Back()
				e.Window.InvalidateLayout()
			},
		),
		gogui.Label(p.Title, t.TextStyleDisplay),
	}
	if p.Settings != nil {
		gear := back
		gear.Family = icons.Family
		head = append(head,
			gogui.Row(gogui.ContainerCfg{Sizing: gogui.FillFit, Padding: gogui.NoPadding}),
			kit.KeyButton(
				"settings",
				icons.Glyph(icons.Named("mdi:cog")),
				gear,
				style.Rest,
				func(e gogui.EventCtx) {
					p.Settings()
					e.Window.InvalidateLayout()
				},
			),
		)
	}
	return gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFill,
		Padding: gogui.PaddingLarge,
		Spacing: gogui.SpacingMedium,
		Content: append([]gogui.View{
			gogui.Row(gogui.ContainerCfg{
				Sizing:  gogui.FillFit,
				VAlign:  gogui.VAlignMiddle,
				Spacing: gogui.SpacingMedium,
				Content: head,
			}),
			gogui.Column(
				gogui.ContainerCfg{
					ID:         "page",
					Sizing:     gogui.FillFill,
					Scrollable: !p.Fixed,
					OnGesture:  p.Gesture,
					Content:    append([]gogui.View{p.Build(w)}, p.Room...),
				},
			),
		}, foot...),
	})
}

func (kit *Toolkit) ActionBar(w *gogui.Window, actions []shell.Action, pal theme.Theme) gogui.View {
	t := gogui.CurrentTheme()
	tall := style.Reach()
	content := make([]gogui.View, 0, 2*len(actions))
	for i, a := range actions {
		if i == len(actions)-1 {
			content = append(
				content,
				gogui.Row(gogui.ContainerCfg{Sizing: gogui.FillFit, Padding: gogui.NoPadding}),
			)
		}
		st := t.TextStyleTitle
		cfg := gogui.ContainerCfg{
			ID:      fmt.Sprintf("action-%d", i),
			Width:   KeyWidth(w, a.Label, st, tall) + tall,
			Height:  tall,
			Sizing:  gogui.FixedFixed,
			Radius:  gogui.RadiusLarge,
			HAlign:  gogui.HAlignCenter,
			VAlign:  gogui.VAlignMiddle,
			Padding: gogui.NoPadding,
			Spacing: gogui.SpacingSmall,
		}
		fill := pal.Accent
		switch a.Role {
		case shell.Plain:
			fill = pal.Background.Blend(pal.Text, 0.18)
		case shell.Danger:
			fill = pal.Danger
		}
		cfg.Color, st.Color = Color(fill), Color(pal.Contrast(fill))
		if a.Glyph != "" {
			mark := IconStyle(st.Color)
			mark.Family = icons.Family
			cfg.Content = append(cfg.Content, gogui.Label(icons.Glyph(icons.Named(a.Glyph)), mark))
		}
		cfg.Content = append(cfg.Content, gogui.Label(a.Label, st))
		do := a.Do
		content = append(content, kit.Pressable(gogui.Row, cfg, func(e gogui.EventCtx) {
			do()
			e.Window.InvalidateLayout()
		}))
	}
	return gogui.Row(gogui.ContainerCfg{
		Sizing:  gogui.FillFit,
		VAlign:  gogui.VAlignMiddle,
		Padding: gogui.NoPadding,
		Content: content,
	})
}

func (e *Editor) Room(w *gogui.Window) []gogui.View {
	_, vh := w.WindowSize()
	if !e.Typing(w) || e.Top <= 0 {
		return nil
	}
	return []gogui.View{
		gogui.Column(
			gogui.ContainerCfg{
				Height:  float32(vh) - e.Top,
				Sizing:  gogui.FillFixed,
				Padding: gogui.NoPadding,
			},
		),
	}
}
