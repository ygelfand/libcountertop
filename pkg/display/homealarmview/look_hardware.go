package homealarmview

import (
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/homeassistant/homealarm"
	"github.com/ygelfand/libcountertop/pkg/say"
)

func hardwareLook(f *face) (gogui.View, skin) {
	sk := f.wear(skin{
		back: hex(0xd8d0bb), panel: hex(0xc9c0a8), key: hex(0x4a4640), keyInk: hex(0xd8d0bb),
		ink:   hex(0x2e2a24),
		muted: hex(0x5e574c), calm: hex(0x7ee08f), armed: hex(0xff7a6b), warn: hex(0xffc04d),
		alarm: hex(0xff4d3d), chosen: hex(0x173f22), chosenInk: hex(0x7ee08f),
		edge: hex(0x8f8774), edgeWide: max(2, f.unit()*0.005), radius: f.unit() * 0.012,
		upper: true, spaced: true,
	})
	u := f.unit()
	m := u * 0.05
	plate := func(text string, w, h float32) gogui.View {
		return gogui.Row(gogui.ContainerCfg{
			Width: w, Height: h, Sizing: gogui.FixedFixed, Color: hex(0xece6d6),
			ColorBorder: sk.edge, SizeBorder: gogui.BorderPx(max(1, u*0.003)),
			HAlign: gogui.HAlignCenter, VAlign: gogui.VAlignMiddle, Padding: gogui.NoPadding,
			Content: []gogui.View{sk.label(text, h*0.42, sk.ink, true)},
		})
	}

	nixieW := f.vw * 0.26
	nixieH := u * 0.2
	digits := f.clock()
	tubeSt := sk.text(nixieH*0.6, hex(0xff8a2a), false)
	colon := f.blink(time.Second)
	dimSt := tubeSt
	dimSt.Color = hex(0x5a3a1c)
	nixie := gogui.DrawCanvas(gogui.DrawCanvasCfg{
		ID: "homealarm-nixie", Width: nixieW, Height: nixieH, Sizing: gogui.FixedFixed,
		Version: uint64(f.now.Unix()/60)<<17 | uint64(nixieW)<<1 | bit(colon),
		OnDraw: func(dc *gogui.DrawContext) {
			dc.FilledRoundedRect(0, 0, nixieW, nixieH, sk.radius*2, hex(0x2b2b2b))
			n := float32(len([]rune(digits)))
			gap := nixieW * 0.02
			tw := (nixieW - gap*(n+1)) / n
			for i, r := range []rune(digits) {
				x := gap + float32(i)*(tw+gap)
				y := nixieH * 0.08
				h := nixieH * 0.84
				dc.FilledRoundedRect(x, y, tw, h, tw*0.3, hex(0x3a2a1c))
				st := tubeSt
				if r == ':' && !colon {
					st = dimSt
				}
				glyphW := dc.TextWidth(string(r), st)
				dc.Text(x+(tw-glyphW)/2, y+(h-dc.FontHeight(st))/2, string(r), st)
			}
		},
	})
	views := []gogui.View{
		place(m, m, nixieW, nixieH, nixie),
		place(
			m,
			m+nixieH+u*0.015,
			nixieW,
			u*0.055,
			plate(say.T("homealarm.plate.time"), nixieW, u*0.055),
		),
	}

	lamps := []struct {
		label string
		lit   bool
		c     gogui.Color
	}{
		{say.T("homealarm.chip.ready"), f.state.Status == homealarm.Disarmed, sk.calm},
		{say.T("homealarm.chip.armed"), f.state.Status.Armed(), sk.armed},
		{
			say.T("homealarm.chip.delay"),
			f.state.Status == homealarm.Arming || f.state.Status == homealarm.Pending,
			sk.warn,
		},
		{say.T("homealarm.chip.alarm"), f.state.Status == homealarm.Triggered, sk.alarm},
	}
	stripX := m + nixieW + m
	stripW := f.vw - stripX - m - f.cornerWidth()
	plateText := f.title()
	if zones := f.zones(); len(zones) > 0 {
		lamps = lamps[:0]
		for _, z := range zones {
			lamps = append(lamps, struct {
				label string
				lit   bool
				c     gogui.Color
			}{z.number, z.alert, sk.warn})
		}
		plateText = say.T("homealarm.zones")
		if r := f.ready(); r != "" {
			plateText += " · " + r
		}
	}
	lampViews := make([]gogui.View, 0, len(lamps))
	lampD := min(u*0.065, stripW/float32(len(lamps))*0.5)
	for _, l := range lamps {
		c := hex(0x3f5a40)
		if l.lit {
			c = l.c
		}
		lampViews = append(lampViews, gogui.Column(gogui.ContainerCfg{
			Padding: gogui.NoPadding,
			Spacing: gogui.SpacingPx(u * 0.01),
			HAlign:  gogui.HAlignCenter,
			Content: []gogui.View{
				gogui.Column(
					gogui.ContainerCfg{Width: lampD, Height: lampD, Sizing: gogui.FixedFixed,
						Radius: gogui.RadiusPx(lampD / 2), Color: c, ColorBorder: sk.edge,
						SizeBorder: gogui.BorderPx(max(1, u*0.004)), Padding: gogui.NoPadding},
				),
				sk.label(l.label, u*0.026, sk.ink, true),
			},
		}))
	}
	views = append(views,
		place(stripX, m, stripW, nixieH, gogui.Row(gogui.ContainerCfg{
			Sizing:  gogui.FillFill,
			Color:   sk.panel,
			Radius:  gogui.RadiusPx(sk.radius * 2),
			HAlign:  gogui.HAlignCenter,
			VAlign:  gogui.VAlignMiddle,
			Spacing: gogui.SpacingPx(min(u*0.07, stripW/float32(len(lamps))*0.4)),
			Padding: gogui.NoPadding,
			Content: lampViews,
		})),
		place(stripX, m+nixieH+u*0.015, stripW, u*0.055, plate(plateText, stripW, u*0.055)),
	)

	gridY := m + nixieH + u*0.1
	panicW := f.vw * 0.3
	gridW := f.vw - 3*m - panicW
	if !f.emergency() {
		gridW = f.vw - 2*m
	}
	gridH := f.vh - gridY - m - u*0.07
	gap := u * 0.03
	tileH := (gridH - gap) / 2
	tile := func(id, big, small string, w float32, fill, ink gogui.Color, run func()) gogui.View {
		return f.key(key{
			id: id, w: w, h: tileH, fill: fill, ink: ink, edge: sk.edge, edgeWide: sk.edgeWide * 2,
			radius: sk.radius, align: gogui.HAlignCenter,
			content: []gogui.View{gogui.Column(gogui.ContainerCfg{
				Padding: gogui.NoPadding,
				HAlign:  gogui.HAlignCenter,
				Spacing: gogui.SpacingPx(tileH * 0.04),
				Content: []gogui.View{
					sk.label(big, f.fit(sk, big, w*0.85, tileH*0.16, true), ink, true),
					sk.label(small, tileH*0.08, ink.WithOpacity(0.8), false),
				},
			})},
			run: run,
		})
	}
	tone := f.tone(sk)
	tw := (gridW - 2*gap) / 3
	link, linkSmall := say.T("homealarm.plate.link"), say.T("homealarm.plate.lost")
	linkFill := hex(0x4a2a24)
	if f.state.Known() {
		linkSmall, linkFill = say.T("homealarm.plate.normal"), hex(0x57514a)
	}
	delay := say.T("homealarm.chip.delay")
	delayFill, delayNote := hex(0x4a4640), say.T("homealarm.plate.idle")
	switch f.state.Status {
	case homealarm.Arming:
		delayFill, delayNote = hex(0x3d2a00), say.T("homealarm.plate.exit")
	case homealarm.Pending:
		delayFill, delayNote = hex(0x3d2a00), say.T("homealarm.plate.entry")
	}
	top := []gogui.View{
		tile(
			"lamp-state",
			f.title(),
			say.T("homealarm.plate.system"),
			tw,
			mix(tone, hex(0x000000), 0.7),
			tone,
			nil,
		),
		tile("lamp-delay", delay, delayNote, tw, delayFill, sk.warn, nil),
		tile("lamp-link", link, linkSmall, tw, linkFill, hex(0xffffff), nil),
	}
	acts := f.actions()
	bw := gridW
	if len(acts) > 0 {
		bw = (gridW - float32(len(acts)-1)*gap) / float32(len(acts))
	}
	bottom := make([]gogui.View, 0, len(acts))
	for _, a := range acts {
		big, small := say.T("homealarm.arm"), a.label
		if a.status == homealarm.Disarmed {
			big, small = a.label, ""
		}
		bottom = append(bottom, tile(a.id, big, small, bw, sk.key, sk.keyInk, a.run))
	}
	views = append(
		views,
		place(m, gridY, gridW, tileH, row(gridW, tileH, gap, gogui.HAlignLeft, top...)),
		place(
			m,
			gridY+tileH+gap,
			gridW,
			tileH,
			row(gridW, tileH, gap, gogui.HAlignLeft, bottom...),
		),
		place(
			m,
			f.vh-m-u*0.055,
			gridW,
			u*0.055,
			plate(say.T("homealarm.plate.mode"), gridW, u*0.055),
		),
	)
	if f.emergency() {
		px := f.vw - m - panicW
		views = append(
			views,
			place(px, gridY, panicW, gridH,
				f.emergencyKey(skin{
					upper: true, spaced: true, radius: sk.radius, family: sk.family,
					edge: hex(0xf2c200), edgeWide: max(3, u*0.008),
				}, panicW, gridH, hex(0xb3261e), hex(0xffffff))),
			place(
				px,
				f.vh-m-u*0.055,
				panicW,
				u*0.055,
				plate(say.T("homealarm.hold"), panicW, u*0.055),
			),
		)
	}
	return gogui.Stack(gogui.ContainerCfg{
		Width:   f.vw,
		Height:  f.vh,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Content: views,
	}), sk
}
