package widgets

import (
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/icons"
	"github.com/ygelfand/libcountertop/pkg/display/interaction"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
)

func CloseCorner(p *interaction.Presses, id string, level float32, tap func()) gogui.View {
	return CloseCornerAt(p, id, int(gogui.PadLarge), level, tap)
}

func CloseCornerAt(p *interaction.Presses, id string, x int, level float32, tap func()) gogui.View {
	return corner(p, id, gogui.IconClose, cornerStyle(), x, level, tap)
}

func SettingsCorner(
	p *interaction.Presses,
	id string,
	vw int,
	level float32,
	tap func(),
) gogui.View {
	return SettingsCornerAt(p, id, vw-int(gogui.PadLarge)-CornerSide(), level, tap)
}

func SettingsCornerAt(
	p *interaction.Presses,
	id string,
	x int,
	level float32,
	tap func(),
) gogui.View {
	st := cornerStyle()
	st.Family = icons.Family
	return corner(p, id, icons.Glyph(icons.Named("mdi:cog")), st, x, level, tap)
}

func CornerSide() int { return int(cornerStyle().Size * 2) }

func cornerStyle() gogui.TextStyle {
	st := gogui.CurrentTheme().TextStyleIconMedium
	st.Size = style.Reach() * 0.75
	st.Color = gogui.RGB(0xff, 0xff, 0xff)
	return st
}

func corner(
	p *interaction.Presses,
	id, glyph string,
	st gogui.TextStyle,
	x int,
	level float32,
	tap func(),
) gogui.View {
	if level <= 0 {
		return Placed(ui.Rect{}, gogui.Column(gogui.ContainerCfg{Padding: gogui.NoPadding}))
	}
	side := int(st.Size * 2)
	pad := int(gogui.PadLarge)
	st.Color = st.Color.WithOpacity(min(level, 1))
	return Placed(
		ui.Rect{X: x, Y: pad, W: side, H: side},
		p.Button(
			id,
			glyph,
			st,
			gogui.RGBA(0, 0, 0, uint8(0x99*min(level, 1))),
			func(gogui.EventCtx) { tap() },
		),
	)
}
