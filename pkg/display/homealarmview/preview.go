package homealarmview

import (
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type lookPreview struct {
	v    *View
	look schema.HomeAlarmLook
}

func (v *View) preview(look schema.HomeAlarmLook) menu.Preview { return lookPreview{v, look} }

func (p lookPreview) View(w *gogui.Window, width, height float32) gogui.View {
	draw, ok := looks[p.look]
	if !ok {
		draw = keypadLook
	}
	f := &face{
		v:     p.v,
		w:     w,
		vw:    width,
		vh:    height,
		state: p.v.shown(),
		dev:   p.v.shownDevice(),
		still: true,
		now:   time.Now(),
	}
	body, sk := draw(f)
	return gogui.Column(gogui.ContainerCfg{
		Width:   width,
		Height:  height,
		Sizing:  gogui.FixedFixed,
		Color:   sk.back,
		Radius:  gogui.RadiusMedium,
		Padding: gogui.NoPadding,
		Clip:    true,
		Content: []gogui.View{body},
	})
}
