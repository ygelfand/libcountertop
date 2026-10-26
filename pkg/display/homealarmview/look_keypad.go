package homealarmview

import (
	"slices"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/homealarm"
	"github.com/ygelfand/libcountertop/pkg/say"
)

func keypadLook(f *face) (gogui.View, skin) {
	sk := f.wear(skin{
		back: hex(0x23262d), panel: hex(0x2c3038), key: hex(0x3a3f49), ink: hex(0xf2f3f5),
		muted: hex(0x9aa0aa), calm: hex(0x4ade80), armed: hex(0xef4444), warn: hex(0xfacc15),
		alarm: hex(0xef4444), chosen: hex(0x4a5a78), chosenInk: hex(0xffffff),
		radius: f.unit() * 0.018,
	})
	u := f.unit()
	m := u * 0.045
	top := u * 0.1
	tone := f.tone(sk)

	chip := func(label string, lit bool, c gogui.Color) gogui.View {
		dot := sk.muted.WithOpacity(0.4)
		if lit {
			dot = c
		}
		return gogui.Row(gogui.ContainerCfg{
			Padding: gogui.NoPadding,
			Spacing: gogui.SpacingPx(u * 0.012),
			VAlign:  gogui.VAlignMiddle,
			Content: []gogui.View{
				gogui.Column(gogui.ContainerCfg{
					Width: u * 0.028, Height: u * 0.028, Sizing: gogui.FixedFixed,
					Radius: gogui.RadiusPx(u * 0.014), Color: dot, Padding: gogui.NoPadding,
				}),
				sk.label(label, u*0.032, sk.ink, false),
			},
		})
	}
	s := f.state.Status
	bar := place(m, 0, f.vw-2*m-f.cornerWidth(), top, gogui.Row(gogui.ContainerCfg{
		Sizing: gogui.FillFill, Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(u * 0.04),
		VAlign: gogui.VAlignMiddle,
		Content: []gogui.View{
			chip(say.T("homealarm.chip.ready"), s == homealarm.Disarmed, sk.calm),
			chip(say.T("homealarm.chip.armed"), s.Armed(), sk.armed),
			chip(
				say.T("homealarm.chip.delay"),
				s == homealarm.Arming || s == homealarm.Pending,
				sk.warn,
			),
			chip(say.T("homealarm.chip.alarm"), s == homealarm.Triggered, sk.alarm),
			gogui.Row(gogui.ContainerCfg{Sizing: gogui.FillFit, Padding: gogui.NoPadding}),
			sk.label(f.clock()+" "+f.meridiem(), u*0.045, sk.ink, true),
		},
	}))

	rightW := f.vw * 0.36
	rx := f.vw - m - rightW
	leftW := rx - 2*m
	body := f.vh - top - m

	title := f.title()
	tsize := f.fit(sk, title, leftW, u*0.13, true)
	left := []gogui.View{
		row(leftW, tsize*1.25, u*0.02, gogui.HAlignLeft,
			icon(f.glyph(), tsize*0.9, tone),
			sk.label(title, tsize, sk.ink, true)),
	}
	if d := f.detail(); d != "" {
		left = append(left, sk.label(d, u*0.04, sk.warn, true))
	}
	head := tsize*1.25 + u*0.07
	foot := u * 0.12
	views := []gogui.View{bar,
		place(m, top, leftW, head, gogui.Column(gogui.ContainerCfg{
			Sizing: gogui.FillFill, Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(u * 0.01),
			Content: left,
		})),
	}
	gridH := body - head - foot - u*0.03
	if zones := f.zones(); len(zones) > 0 {
		views = append(
			views,
			place(m, top+head, leftW, gridH, keypadZones(f, sk, zones, leftW, gridH)),
		)
	} else if lines := f.entries(6); len(lines) > 0 {
		log := []gogui.View{
			widgets.Divider(),
			sk.label(say.T("homealarm.recent"), u*0.03, sk.muted, true),
		}
		for _, l := range lines {
			log = append(log, sk.label(l, u*0.034, sk.ink, false))
		}
		views = append(views, place(m, top+head, leftW, gridH, gogui.Column(gogui.ContainerCfg{
			Sizing: gogui.FillFill, Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(u * 0.02),
			Content: log,
		})))
	}
	bottom := []gogui.View{}
	for _, t := range f.toggles() {
		w := min(leftW*0.25, u*0.3)
		fill := sk.key
		if t.current {
			fill = sk.chosen
		}
		bottom = append(bottom, f.key(key{
			id:     t.id,
			w:      w,
			h:      foot,
			fill:   fill,
			ink:    sk.ink,
			radius: sk.radius,
			align:  gogui.HAlignCenter,
			content: []gogui.View{
				sk.label(t.label, f.fit(sk, t.label, w*0.85, foot*0.3, true), sk.ink, true),
			},
			run: t.run,
		}))
	}
	if f.emergency() {
		w := leftW * 0.55
		bottom = append(bottom, f.emergencyKey(sk, w, foot, hex(0xe5484d), hex(0xffffff)))
	}
	views = append(views, place(m, f.vh-m-foot, leftW, foot,
		row(leftW, foot, u*0.02, gogui.HAlignLeft, bottom...)))

	inner := rightW - 2*m*0.6
	display := u * 0.11
	acts := f.actions()
	actH := u * 0.1
	padH := body - 2*m*0.6 - display - actH - 2*u*0.025
	actW := inner
	if len(acts) > 0 {
		actW = (inner - float32(len(acts)-1)*u*0.02) / float32(len(acts))
	}
	keys := make([]gogui.View, 0, len(acts))
	for _, a := range acts {
		keys = append(keys, f.key(key{
			id: a.id, w: actW, h: actH, fill: sk.chosen, ink: sk.ink, radius: sk.radius,
			align: gogui.HAlignCenter,
			content: []gogui.View{sk.label(a.label, f.fit(sk, a.label, actW*0.85, actH*0.34, true),
				sk.ink, true)},
			run: a.run,
		}))
	}
	views = append(views, place(rx, top, rightW, body, gogui.Column(gogui.ContainerCfg{
		Sizing: gogui.FillFill, Color: sk.panel, Radius: gogui.RadiusPx(sk.radius * 1.5),
		Padding: gogui.PadAll(m * 0.6), Spacing: gogui.SpacingPx(u * 0.025),
		HAlign: gogui.HAlignCenter,
		Content: []gogui.View{
			gogui.Row(gogui.ContainerCfg{
				Width: inner, Height: display, Sizing: gogui.FixedFixed, Color: hex(0x15171b),
				Radius: gogui.RadiusPx(sk.radius), HAlign: gogui.HAlignCenter,
				VAlign: gogui.VAlignMiddle, Padding: gogui.NoPadding,
				Content: []gogui.View{sk.label(f.dots(sk), display*0.36, sk.muted, true)},
			}),
			row(inner, actH, u*0.02, gogui.HAlignCenter, keys...),
			f.padGrid(sk, inner, padH),
		},
	})))
	return gogui.Stack(gogui.ContainerCfg{
		Width:   f.vw,
		Height:  f.vh,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Content: views,
	}), sk
}

func keypadZones(f *face, sk skin, zones []zoneLine, w, h float32) gogui.View {
	u := f.unit()
	gap := u * 0.015
	cols := 4
	if len(zones) <= 6 {
		cols = 3
	}
	slices.SortStableFunc(zones, func(a, b zoneLine) int {
		switch {
		case a.alert == b.alert:
			return 0
		case a.alert:
			return -1
		}
		return 1
	})
	least := u * 0.12
	var pager gogui.View
	if fit := int((h+gap)/(least+gap)) * cols; len(zones) > fit {
		pagerH := u * 0.075
		per := max(int((h-pagerH-gap+gap)/(least+gap)), 1) * cols
		pages := (len(zones) + per - 1) / per
		page := int(f.v.zonePage.Load()) % pages
		from, to := page*per, min(page*per+per, len(zones))
		pager = row(
			w,
			pagerH,
			0,
			gogui.HAlignRight,
			f.zonePager(sk, pagerH, from, to, len(zones), pages),
		)
		zones = zones[from:to]
		h -= pagerH + gap
	}
	rows := (len(zones) + cols - 1) / cols
	tw := (w - float32(cols-1)*gap) / float32(cols)
	th := min((h-float32(rows-1)*gap)/float32(rows), u*0.16)
	tiles := make([]gogui.View, 0, len(zones))
	for _, z := range zones {
		fill, ink, sub := sk.panel, sk.ink, sk.muted
		switch {
		case z.alert:
			fill, ink, sub = hex(0xf5c518), hex(0x1a1a1a), hex(0x3a3000)
		case z.muted:
			fill, ink = sk.back, sk.muted
		}
		tiles = append(tiles, gogui.Column(gogui.ContainerCfg{
			Width: tw, Height: th, Sizing: gogui.FixedFixed, Color: fill,
			Radius: gogui.RadiusPx(sk.radius), Padding: gogui.PadVH(th*0.07, th*0.1), Clip: true,
			Content: []gogui.View{
				gogui.Row(gogui.ContainerCfg{
					Sizing: gogui.FillFit, Padding: gogui.NoPadding,
					Content: []gogui.View{
						sk.label(z.number, th*0.26, ink, true),
						gogui.Row(
							gogui.ContainerCfg{Sizing: gogui.FillFit, Padding: gogui.NoPadding},
						),
						sk.label(z.state, th*0.17, sub, true),
					},
				}),
				gogui.Row(gogui.ContainerCfg{Sizing: gogui.FillFill, Padding: gogui.NoPadding}),
				sk.label(z.name, f.fit(sk, z.name, tw*0.85, th*0.19, false), ink, false),
			},
		}))
	}
	var lines []gogui.View
	for i := 0; i < len(tiles); i += cols {
		lines = append(
			lines,
			row(w, th, gap, gogui.HAlignLeft, tiles[i:min(i+cols, len(tiles))]...),
		)
	}
	if pager != nil {
		lines = append(
			lines,
			gogui.Column(gogui.ContainerCfg{Sizing: gogui.FillFill, Padding: gogui.NoPadding}),
			pager,
		)
	}
	return gogui.Column(gogui.ContainerCfg{
		Sizing: gogui.FillFill, Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(gap), Clip: true,
		Content: lines,
	})
}
