package homealarmview

import (
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/style"
)

func matrixLook(f *face) (gogui.View, skin) {
	sk := f.wear(skin{
		glow: true,
		back: hex(0x000000), panel: hex(0x010a03), key: hex(0x000000), ink: hex(0x3cff7a),
		muted: hex(0x1f8f45), calm: hex(0x3cff7a), armed: hex(0x3cff7a), warn: hex(0xd8ff3c),
		alarm: hex(0xff4040), chosen: hex(0x00f060), chosenInk: hex(0x000000),
		edge: hex(0x3cff7a), edgeWide: max(2, f.unit()*0.004), family: style.MonoFamily,
		upper: true, spaced: true,
	})
	u := f.unit()
	m := u * 0.04
	rainH := f.vh - u*0.24
	views := []gogui.View{}
	if zones := f.zones(); len(zones) > 0 {
		cell := f.vw / float32(len(zones))
		size := min(cell*0.5, u*0.04)
		digits := sk
		digits.spaced = false
		strip := make([]gogui.View, 0, len(zones))
		for _, z := range zones {
			ink, edge := sk.muted, gogui.RGBA(0, 0, 0, 0)
			if z.alert {
				ink, edge = hex(0xd8ffe4), sk.edge
			}
			strip = append(strip, gogui.Column(gogui.ContainerCfg{
				Width:       cell * 0.84,
				Height:      size * 1.8,
				Sizing:      gogui.FixedFixed,
				ColorBorder: edge,
				SizeBorder:  gogui.BorderPx(max(1.5, u*0.003)),
				Color: gogui.RGBA(
					0,
					0,
					0,
					0xc0,
				),
				HAlign:  gogui.HAlignCenter,
				VAlign:  gogui.VAlignMiddle,
				Padding: gogui.NoPadding,
				Content: []gogui.View{sk.label(z.number, size, ink, false)},
			}))
		}
		views = append(views, place(0, rainH-size*2.2, f.vw, size*1.8,
			row(f.vw, size*1.8, cell*0.16, gogui.HAlignCenter, strip...)))
	}
	views = append(views,
		place(
			m,
			m*0.6,
			f.vw*0.5,
			u*0.06,
			sk.label(f.now.Format("15:04:05")+"   "+f.now.Format("02.01.2006"),
				u*0.032, sk.ink, false),
		),
	)

	boxW := min(f.vw*0.55, u*1.1)
	boxH := u * 0.36
	bx, by := (f.vw-boxW)/2, (rainH-boxH)/2
	title := f.title()
	inner := []gogui.View{
		sk.label(title, f.fit(sk, title, boxW*0.88, boxH*0.34, true), f.tone(sk), true),
	}
	if d := f.detail(); d != "" {
		inner = append(inner, sk.label(d, f.fit(sk, d, boxW*0.88, boxH*0.1, false), sk.ink, false))
	}
	views = append(views, place(bx, by, boxW, boxH, gogui.Column(gogui.ContainerCfg{
		Sizing:      gogui.FillFill,
		Color:       gogui.RGBA(0, 0, 0, 0xe0),
		ColorBorder: sk.edge,
		SizeBorder: gogui.BorderPx(
			sk.edgeWide,
		),
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Spacing: gogui.SpacingPx(boxH * 0.08),
		Padding: gogui.NoPadding,
		Content: inner,
	})))

	acts := f.actions()
	n := len(acts)
	if f.emergency() {
		n++
	}
	keyH := u * 0.12
	gap := u * 0.02
	kw := (f.vw - 2*m - float32(max(n-1, 0))*gap) / float32(max(n, 1))
	keys := make([]gogui.View, 0, n)
	for i, a := range acts {
		fill, ink := sk.key, sk.ink
		if a.current || len(acts) > 1 && i == 1 {
			fill, ink = sk.chosen, sk.chosenInk
		}
		keys = append(keys, f.key(key{
			id:       a.id,
			w:        kw,
			h:        keyH,
			fill:     fill,
			ink:      ink,
			edge:     sk.edge,
			edgeWide: sk.edgeWide,
			align:    gogui.HAlignCenter,
			content: []gogui.View{
				sk.label(a.label, f.fit(sk, a.label, kw*0.85, keyH*0.34, false), ink, false),
			},
			run: a.run,
		}))
	}
	if f.emergency() {
		keys = append(
			keys,
			f.emergencyKey(
				skin{upper: true, family: sk.family, edge: sk.alarm, edgeWide: sk.edgeWide},
				kw,
				keyH,
				sk.back,
				sk.alarm,
			),
		)
	}
	views = append(views, place(m, f.vh-m-keyH, f.vw-2*m, keyH,
		row(f.vw-2*m, keyH, gap, gogui.HAlignLeft, keys...)))
	return gogui.Stack(gogui.ContainerCfg{
		Width:   f.vw,
		Height:  f.vh,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Content: views,
	}), sk
}
