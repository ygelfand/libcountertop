package previews

import (
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/clockview"
	"github.com/ygelfand/libcountertop/pkg/display/dashboard"
	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type clockFace struct {
	kit   Kit
	face  schema.Face
	ink   func() theme.Theme
	place func(w, h int) ui.Rect
}

func whole(w, h int) ui.Rect { return ui.Rect{W: w, H: h} }

func (k Kit) ClockFace(f schema.Face) menu.Preview { return clockFace{k, f, k.ink, whole} }

func (k Kit) ClockInk(f schema.Face, ink schema.Ink) menu.Preview {
	return clockFace{k, f, func() theme.Theme { return ink.Over(k.ui.Palette()) }, whole}
}

func (k Kit) ClockPlaced(f schema.Face, at schema.Placement) menu.Preview {
	return clockFace{k, f, k.ink, func(w, h int) ui.Rect {
		return dashboard.Place(at.Position, at.Align, at.Size, w, h)
	}}
}

func (c clockFace) View(w *gogui.Window, width, height float32) gogui.View {
	pal := c.kit.ui.Palette()
	tile := gogui.ContainerCfg{
		Width:   width,
		Height:  height,
		Sizing:  gogui.FixedFixed,
		Color:   widgets.Color(pal.Surface),
		Radius:  gogui.RadiusMedium,
		Padding: gogui.NoPadding,
		Clip:    true,
	}
	f, ok := clockview.Of(c.face)
	if !ok {
		return gogui.Column(tile)
	}
	at := c.place(int(width), int(height))
	tile.Padding = gogui.NewPadding(float32(at.Y), 0, 0, float32(at.X))
	r := clockview.Read(time.Now(), c.kit.hours()).Undated()
	tile.Content = []gogui.View{gogui.Column(gogui.ContainerCfg{
		Width:   float32(at.W),
		Height:  float32(at.H),
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Content: []gogui.View{f.Draw(w, ui.Rect{W: at.W, H: at.H}, r, c.ink())},
	})}
	return gogui.Column(tile)
}
