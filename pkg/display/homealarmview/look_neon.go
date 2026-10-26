package homealarmview

import (
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/say"
)

func neonLook(f *face) (gogui.View, skin) {
	sk := f.wear(skin{
		glow: true,
		back: hex(0x0c0818), panel: hex(0x1a1430), key: hex(0x1a1430), ink: hex(0xf5ecff),
		muted: hex(0x9a90b8), calm: hex(0xff2e88), armed: hex(0x2ef0ff), warn: hex(0xffd23f),
		alarm: hex(0xff2e5a), chosen: hex(0x2ef0ff), chosenInk: hex(0x0c0818),
		edge: hex(0x2a8f9a), edgeWide: max(2, f.unit()*0.004), upper: true,
	})
	u := f.unit()
	m := u * 0.06
	tone := f.tone(sk)
	leftW := f.vw*0.62 - m

	header := []gogui.View{
		sk.label(f.stateName(), u*0.04, hex(0xff2e88), true),
		sk.label(f.now.Format("Jan 2"), u*0.04, sk.muted, false),
	}
	title := f.title()
	tsize := f.fit(sk, title, leftW, u*0.2, true)
	split := tsize * (0.015 + 0.025*f.breath(2600*time.Millisecond))
	glitch := gogui.Stack(gogui.ContainerCfg{
		Width: leftW, Height: tsize * 1.15, Sizing: gogui.FixedFixed, Padding: gogui.NoPadding,
		Content: []gogui.View{
			place(2*split, 0, leftW, tsize*1.15, sk.label(title, tsize, hex(0xff2e88), true)),
			place(0, 0, leftW, tsize*1.15, sk.label(title, tsize, hex(0x2ef0ff), true)),
			place(split, 0, leftW, tsize*1.15, sk.label(title, tsize, sk.ink, true)),
		},
	})
	left := []gogui.View{
		gogui.Row(gogui.ContainerCfg{Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(u * 0.04),
			Content: header}),
		skin{
			upper:  true,
			spaced: true,
		}.label(
			say.T("homealarm.neon.state"),
			u*0.035,
			hex(0x2ef0ff),
			false,
		),
		glitch,
	}
	if d := f.detail(); d != "" {
		left = append(left, gogui.Row(gogui.ContainerCfg{
			Color: tone, Padding: gogui.PadVH(u*0.02, u*0.035),
			Content: []gogui.View{sk.label(d, u*0.04, sk.back, true)},
		}))
	}
	views := []gogui.View{place(m, m, leftW, f.vh*0.48-m, gogui.Column(gogui.ContainerCfg{
		Sizing: gogui.FillFill, Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(u * 0.03),
		Content: left,
	}))}

	lines := f.entries(4)
	if zones := f.zones(); len(zones) > 0 {
		secure, open := f.counts()
		gridY := f.vh * 0.56
		barH := u * 0.14
		gap := u * 0.012
		bw := (leftW - float32(len(zones)-1)*gap) / float32(len(zones))
		bars := make([]gogui.View, 0, len(zones))
		for _, z := range zones {
			fill, edge, ink := hex(0x1e2a44), hex(0x2ef0ff), sk.muted
			if z.alert {
				fill, edge, ink = hex(0xff2e88), hex(0xff2e88), hex(0xff2e88)
			}
			bars = append(bars, gogui.Column(gogui.ContainerCfg{
				Width:   bw,
				Sizing:  gogui.FixedFit,
				Padding: gogui.NoPadding,
				HAlign:  gogui.HAlignCenter,
				Spacing: gogui.SpacingPx(u * 0.012),
				Content: []gogui.View{
					gogui.Column(gogui.ContainerCfg{
						Width:   bw,
						Height:  barH,
						Sizing:  gogui.FixedFixed,
						Color:   fill,
						Padding: gogui.NoPadding,
						Content: []gogui.View{gogui.Row(gogui.ContainerCfg{
							Width:   bw,
							Height:  max(2, u*0.005),
							Sizing:  gogui.FixedFixed,
							Color:   edge,
							Padding: gogui.NoPadding,
						})},
					}),
					sk.label(z.number, min(u*0.03, bw*0.45), ink, false),
				},
			}))
		}
		views = append(
			views,
			place(m, gridY, leftW, u*0.05, gogui.Row(gogui.ContainerCfg{
				Sizing: gogui.FillFill, Padding: gogui.NoPadding, VAlign: gogui.VAlignMiddle,
				Content: []gogui.View{
					skin{
						upper:  true,
						spaced: true,
					}.label(
						say.T("homealarm.zones"),
						u*0.03,
						sk.muted,
						false,
					),
					gogui.Row(gogui.ContainerCfg{Sizing: gogui.FillFit, Padding: gogui.NoPadding}),
					sk.label(
						say.F(
							"homealarm.zones.count",
							map[string]any{"Secure": secure, "Open": open},
						),
						u*0.03,
						sk.ink,
						false,
					),
				},
			})),
			place(
				m,
				gridY+u*0.07,
				leftW,
				barH+u*0.06,
				row(leftW, barH+u*0.06, gap, gogui.HAlignLeft, bars...),
			),
		)
		lines = f.troubles()
		for _, t := range f.toggles() {
			lines = append(lines, t.label)
		}
	}
	if len(lines) > 0 {
		rows := make([]gogui.View, 0, len(lines))
		for _, l := range lines {
			rows = append(rows, gogui.Row(gogui.ContainerCfg{
				Width: leftW, Sizing: gogui.FixedFit, Color: sk.panel,
				Padding: gogui.PadVH(u*0.015, u*0.03),
				Content: []gogui.View{
					gogui.Column(gogui.ContainerCfg{Width: u * 0.006, Height: u * 0.05,
						Sizing: gogui.FixedFixed, Color: hex(0x2ef0ff), Padding: gogui.NoPadding}),
					sk.label(l, u*0.034, sk.ink, false),
				},
				Spacing: gogui.SpacingPx(u * 0.02), VAlign: gogui.VAlignMiddle,
			}))
		}
		views = append(
			views,
			place(m, f.vh*0.8, leftW, f.vh*0.2-m, gogui.Column(gogui.ContainerCfg{
				Sizing:  gogui.FillFill,
				Padding: gogui.NoPadding,
				Spacing: gogui.SpacingPx(u * 0.015),
				VAlign:  gogui.VAlignBottom,
				Content: rows,
			})),
		)
	}

	cardW := f.vw*0.38 - 2*m
	cx := f.vw - m - cardW
	acts := f.actions()
	emH := u * 0.13
	avail := f.vh - 2*m - f.cornerWidth()*0.5
	if f.emergency() {
		avail -= emH + u*0.04
	}
	gap := u * 0.03
	n := max(len(acts), 1)
	ch := min((avail-float32(n-1)*gap)/float32(n), u*0.2)
	cards := make([]gogui.View, 0, len(acts))
	for i, a := range acts {
		fill, ink := sk.key, sk.ink
		if a.current || len(acts) > 1 && i == len(acts)/2 {
			fill, ink = sk.chosen, sk.chosenInk
		}
		cards = append(cards, f.key(key{
			id: a.id, w: cardW, h: ch, fill: fill, ink: ink, edge: sk.edge, edgeWide: sk.edgeWide,
			align: gogui.HAlignLeft,
			content: []gogui.View{
				icon(a.glyph, ch*0.32, ink),
				sk.label(a.label, f.fit(sk, a.label, cardW*0.6, ch*0.3, true), ink, true),
			},
			run: a.run,
		}))
	}
	views = append(
		views,
		place(cx, m+f.cornerWidth()*0.5, cardW, avail, gogui.Column(gogui.ContainerCfg{
			Sizing: gogui.FillFill, Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(gap),
			VAlign: gogui.VAlignMiddle, Content: cards,
		})),
	)
	if f.emergency() {
		pink := hex(0xff2e88)
		views = append(views, place(
			cx,
			f.vh-m-emH,
			cardW,
			emH,
			f.emergencyKey(
				skin{upper: true, family: sk.family, edge: pink, edgeWide: sk.edgeWide},
				cardW,
				emH,
				pink.WithOpacity(0.12),
				pink,
			),
		))
	}
	return gogui.Stack(gogui.ContainerCfg{
		Width:   f.vw,
		Height:  f.vh,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Content: views,
	}), sk
}

func (f *face) stateName() string {
	if f.state.Name != "" {
		return f.state.Name
	}
	return say.T("homealarm.app")
}
