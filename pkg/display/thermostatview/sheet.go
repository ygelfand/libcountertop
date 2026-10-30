package thermostatview

import (
	gogui "github.com/go-gui-org/go-gui/gui"

	kit "github.com/ygelfand/libcountertop/pkg/display/panelkit"
)

type sheet struct {
	shade, panel, ink, muted, accent, chosen gogui.Color
	family                                   string
	upper                                    bool
	radius                                   float32
	edge                                     gogui.Color
}

func (v *View) sheetView(w *gogui.Window, vw, vh float32, lk look) gogui.View {
	c := v.chosen()
	if c == nil {
		return nil
	}
	st, ok := v.find(c.entity)
	if !ok {
		return nil
	}
	opts := options(st, c.kind)
	if len(opts) == 0 {
		return nil
	}
	sk := lk.sheet
	u := min(vw, vh)
	cols := 1
	if len(opts) > 4 {
		cols = 2
	}
	rows := (len(opts) + cols - 1) / cols
	rowH := min(u*0.13, (vh*0.8-u*0.16)/float32(rows))
	gap := rowH * 0.18
	pw := min(vw*0.9, u*0.62*float32(cols))
	ph := u*0.16 + float32(rows)*rowH + float32(rows-1)*gap
	t := kit.Type{Family: sk.family, Upper: sk.upper}
	title := t.Label(c.kind.title(), u*0.045, sk.muted, false)
	cw := (pw - u*0.08 - float32(cols-1)*gap) / float32(cols)
	lines := make([]gogui.View, 0, rows)
	for r := range rows {
		cells := make([]gogui.View, 0, cols)
		for i := r * cols; i < min((r+1)*cols, len(opts)); i++ {
			o := opts[i]
			fill, ink := sk.panel, sk.ink
			if o.current {
				fill, ink = sk.chosen, sk.accent
			}
			cfg := gogui.ContainerCfg{
				ID:          "thermostat-choice-" + o.value,
				Width:       cw,
				Height:      rowH,
				Sizing:      gogui.FixedFixed,
				Color:       fill,
				ColorBorder: sk.edge,
				SizeBorder:  gogui.BorderPx(max(1, u*0.002)),
				Radius:      gogui.RadiusPx(sk.radius * u),
				HAlign:      gogui.HAlignCenter,
				VAlign:      gogui.VAlignMiddle,
				Padding:     gogui.NoPadding,
				Content:     []gogui.View{t.Fitted(w, o.label, cw*0.86, rowH*0.42, ink, o.current)},
			}
			chosen, value := *c, o.value
			cells = append(cells, v.o.UI.Presses.Pressable(gogui.Row, cfg, func(gogui.EventCtx) {
				v.apply(chosen, value)
			}))
		}
		lines = append(lines, gogui.Row(gogui.ContainerCfg{
			Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(gap), Content: cells,
		}))
	}
	panel := gogui.Column(gogui.ContainerCfg{
		ID:          "thermostat-sheet",
		Width:       pw,
		Height:      ph,
		Sizing:      gogui.FixedFixed,
		Color:       sk.panel,
		ColorBorder: sk.edge,
		SizeBorder:  gogui.BorderPx(max(1, u*0.002)),
		Radius:      gogui.RadiusPx(sk.radius * u * 1.4),
		Padding:     gogui.PadAll(u * 0.04),
		Spacing:     gogui.SpacingPx(gap),
		Content:     append([]gogui.View{title}, lines...),
	})
	shade := v.o.UI.Presses.Pressable(gogui.Column, gogui.ContainerCfg{
		ID:      "thermostat-sheet-shade",
		Width:   vw,
		Height:  vh,
		Sizing:  gogui.FixedFixed,
		Color:   sk.shade,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Padding: gogui.NoPadding,
		Content: []gogui.View{panel},
	}, func(gogui.EventCtx) { v.close() })
	return kit.Place(0, 0, vw, vh, shade)
}
