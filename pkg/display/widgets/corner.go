package widgets

import (
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/icons"
	"github.com/ygelfand/libcountertop/pkg/display/interaction"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
)

func CloseCorner(p *interaction.Presses, id string, tap func()) gogui.View {
	st := cornerStyle()
	pad := int(gogui.PadLarge)
	return corner(p, id, gogui.IconClose, st, pad, tap)
}

func SettingsCorner(p *interaction.Presses, id string, vw int, tap func()) gogui.View {
	st := cornerStyle()
	st.Family = icons.Family
	pad := int(gogui.PadLarge)
	return corner(p, id, icons.Glyph(icons.Named("mdi:cog")), st, vw-pad-int(st.Size*2), tap)
}

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
	tap func(),
) gogui.View {
	side := int(st.Size * 2)
	pad := int(gogui.PadLarge)
	return Placed(
		ui.Rect{X: x, Y: pad, W: side, H: side},
		p.Button(id, glyph, st, gogui.RGBA(0, 0, 0, 0x99), func(gogui.EventCtx) { tap() }),
	)
}
