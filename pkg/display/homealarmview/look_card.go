package homealarmview

import (
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/homealarm"
	"github.com/ygelfand/libcountertop/pkg/say"
)

func cardLook(f *face) (gogui.View, skin) {
	sk := f.wear(skin{
		back: hex(0xffffff), panel: hex(0xf3f4f8), key: hex(0xffffff), ink: hex(0x1b2440),
		muted: hex(0x6b7280), calm: hex(0x1f8a4c), armed: hex(0xc2410c), warn: hex(0xb7791f),
		alarm: hex(0xdc2626), chosen: hex(0x1b2440), chosenInk: hex(0xffffff),
		edge: hex(0x1b2440), edgeWide: max(2, f.unit()*0.004), radius: f.unit() * 0.035,
	})
	u := f.unit()
	m := u * 0.05
	sideW := f.vw * 0.3
	mainW := f.vw - sideW
	tone := f.tone(sk)

	views := []gogui.View{
		place(mainW, 0, sideW, f.vh, gogui.Column(gogui.ContainerCfg{
			Sizing: gogui.FillFill, Color: sk.panel, Padding: gogui.NoPadding,
		})),
		place(m, m, mainW-2*m, u*0.09, gogui.Row(gogui.ContainerCfg{
			Sizing: gogui.FillFill, Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(u * 0.025),
			VAlign: gogui.VAlignBottom,
			Content: []gogui.View{
				sk.label(f.clock()+" "+f.meridiem(), u*0.065, sk.ink, true),
				sk.label(f.date(), u*0.04, sk.muted, false),
			},
		})),
	}
	disc := u * 0.34
	title := f.title()
	tsize := f.fit(sk, title, mainW-disc-3*m, u*0.15, true)
	mid := []gogui.View{sk.label(title, tsize, sk.ink, true)}
	if d := f.detail(); d != "" {
		mid = append(mid, gogui.Row(gogui.ContainerCfg{
			Color: hex(
				0xfdecd6,
			),
			Radius:  gogui.RadiusPx(u * 0.04),
			Padding: gogui.PadVH(u*0.015, u*0.03),
			Content: []gogui.View{sk.label(d, u*0.04, hex(0xb45309), true)},
		}))
	}
	views = append(views, place(m, m+u*0.12, mainW-2*m, disc+u*0.04, gogui.Row(gogui.ContainerCfg{
		Sizing: gogui.FillFill, Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(m),
		VAlign: gogui.VAlignMiddle,
		Content: []gogui.View{
			gogui.Column(gogui.ContainerCfg{
				Width:  disc,
				Height: disc,
				Sizing: gogui.FixedFixed,
				Radius: gogui.RadiusPx(disc / 2),
				Color: tone.WithOpacity(
					0.14,
				),
				HAlign:  gogui.HAlignCenter,
				VAlign:  gogui.VAlignMiddle,
				Padding: gogui.NoPadding,
				Content: []gogui.View{icon(f.glyph(), disc*0.45, tone)},
			}),
			gogui.Column(gogui.ContainerCfg{
				Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(u * 0.02), Content: mid,
			}),
		},
	})))

	acts := f.actions()
	ch := u * 0.24
	gap := u * 0.03
	n := max(len(acts), 1)
	cw := min((mainW-2*m-float32(n-1)*gap)/float32(n), u*0.55)
	cards := make([]gogui.View, 0, len(acts))
	for i, a := range acts {
		fill, ink := sk.key, sk.ink
		if i == len(acts)/2 && len(acts) > 1 || a.status == homealarm.Disarmed {
			fill, ink = sk.chosen, sk.chosenInk
		}
		cards = append(cards, f.key(key{
			id: a.id, w: cw, h: ch, fill: fill, ink: ink, edge: sk.edge, edgeWide: sk.edgeWide,
			radius: sk.radius, align: gogui.HAlignLeft,
			content: []gogui.View{gogui.Column(gogui.ContainerCfg{
				Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(ch * 0.06),
				Content: []gogui.View{
					icon(a.glyph, ch*0.25, ink),
					sk.label(a.label, f.fit(sk, a.label, cw*0.8, ch*0.2, true), ink, true),
				},
			})},
			run: a.run,
		}))
	}
	views = append(views, place(m, f.vh-m-ch, mainW-2*m, ch,
		row(mainW-2*m, ch, gap, gogui.HAlignLeft, cards...)))

	sx := mainW + m
	sw := sideW - 2*m
	rowH := u * 0.11
	pair := func(id, glyph, label, value string, alert bool, run func()) gogui.View {
		ink := sk.ink
		if alert {
			ink = sk.warn
		}
		return f.key(key{
			id: id, w: sw, h: rowH, fill: sk.panel, ink: ink, align: gogui.HAlignLeft,
			content: []gogui.View{
				gogui.Row(gogui.ContainerCfg{
					Width:  rowH * 0.62,
					Height: rowH * 0.62,
					Sizing: gogui.FixedFixed,
					Color:  hex(0xe6e8ef),
					Radius: gogui.RadiusPx(
						rowH * 0.16,
					),
					HAlign:  gogui.HAlignCenter,
					VAlign:  gogui.VAlignMiddle,
					Padding: gogui.NoPadding,
					Content: []gogui.View{icon(glyph, rowH*0.34, ink)},
				}),
				sk.label(f.short(sk, label, sw*0.4, rowH*0.24, false), rowH*0.24, ink, false),
				gogui.Row(gogui.ContainerCfg{Sizing: gogui.FillFit, Padding: gogui.NoPadding}),
				sk.label(f.short(sk, value, sw*0.32, rowH*0.24, true), rowH*0.24, ink, true),
			},
			run: run,
		})
	}
	var side []gogui.View
	if len(f.dev.Zones) > 0 {
		secure, open := f.counts()
		side = append(side, pair(
			"zones",
			"mdi:door-closed",
			say.T("homealarm.zones"),
			say.F(
				"homealarm.zones.count",
				map[string]any{"Secure": secure, "Open": open},
			),
			open > 0,
			nil,
		))
	}
	for _, t := range f.dev.Toggles {
		value := say.T("homealarm.off")
		if t.On {
			value = say.T("homealarm.on")
		}
		side = append(
			side,
			pair(
				"toggle-"+t.Entity,
				"mdi:bell-outline",
				t.Name,
				value,
				false,
				f.v.toggle(t.Entity),
			),
		)
	}
	for _, i := range f.dev.Indicators {
		if i.Class == "" {
			continue
		}
		value := say.T("homealarm.ok")
		if i.Bad() {
			value = say.T("homealarm.zone.trouble")
		}
		side = append(
			side,
			pair("indicator-"+i.Entity, "mdi:information-outline", i.Name, value, i.Bad(), nil),
		)
	}
	if len(side) == 0 {
		side = append(side, sk.label(say.T("homealarm.recent"), u*0.035, sk.muted, true))
		for _, l := range f.entries(6) {
			side = append(side, sk.label(l, u*0.034, sk.ink, false), widgets.Divider())
		}
	}
	views = append(views, place(sx, m+style.Reach()*1.4, sw, f.vh-2*m-style.Reach()*1.4-u*0.16,
		gogui.Column(gogui.ContainerCfg{
			Sizing: gogui.FillFill, Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(u * 0.02),
			Clip: true, Content: side,
		})))
	if f.emergency() {
		views = append(views, place(sx, f.vh-m-u*0.13, sw, u*0.13,
			f.emergencyKey(sk, sw, u*0.13, hex(0xd9363e), hex(0xffffff))))
	}
	return gogui.Stack(gogui.ContainerCfg{
		Width:   f.vw,
		Height:  f.vh,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Content: views,
	}), sk
}
