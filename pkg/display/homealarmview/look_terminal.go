package homealarmview

import (
	"cmp"
	"slices"
	"strconv"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/say"
)

func phosphor(f *face) skin {
	return skin{
		glow: true, flat: true,
		back:      hex(0x041208),
		panel:     hex(0x06200f),
		key:       hex(0x041208),
		ink:       hex(0x46f07a),
		muted:     hex(0x2a9c4e),
		calm:      hex(0x46f07a),
		armed:     hex(0x46f07a),
		warn:      hex(0xc8f046),
		alarm:     hex(0xf0f046),
		chosen:    hex(0x46f07a),
		chosenInk: hex(0x041208),
		edge: hex(
			0x46f07a,
		),
		edgeWide: max(2, f.unit()*0.004),
		family:   style.MonoFamily,
		upper:    true,
	}
}

func terminalLook(f *face) (gogui.View, skin) {
	sk := f.wear(phosphor(f))
	u := f.unit()
	m := u * 0.05
	line := func(w float32) gogui.View {
		return gogui.Row(
			gogui.ContainerCfg{Width: w, Height: max(1, u*0.004), Sizing: gogui.FixedFixed,
				Color: sk.muted, Padding: gogui.NoPadding},
		)
	}
	head := u * 0.08
	views := []gogui.View{
		place(m, m*0.6, f.vw-2*m-f.cornerWidth(), head, gogui.Row(gogui.ContainerCfg{
			Sizing: gogui.FillFill, Padding: gogui.NoPadding, VAlign: gogui.VAlignMiddle,
			Content: []gogui.View{
				sk.label(
					say.T("homealarm.app")+" // "+cmp.Or(f.dev.Message, f.stateName()),
					u*0.035,
					sk.muted,
					false,
				),
				gogui.Row(gogui.ContainerCfg{Sizing: gogui.FillFit, Padding: gogui.NoPadding}),
				sk.label(f.now.Format("02-Jan-2006 15:04"), u*0.035, sk.muted, false),
			},
		})),
		place(m, m*0.6+head, f.vw-2*m, max(1, u*0.004), line(f.vw-2*m)),
	}

	leftW := f.vw*0.58 - m
	y := m*0.6 + head + u*0.05
	title := f.title()
	tsize := f.fit(sk, title, leftW*0.9, u*0.13, true)
	tone := f.tone(sk)
	left := []gogui.View{
		sk.label("> "+say.T("homealarm.terminal.status"), u*0.045, sk.ink, false),
		gogui.Row(gogui.ContainerCfg{
			Color: tone, Padding: gogui.PadVH(u*0.02, u*0.03),
			Content: []gogui.View{sk.label(title, tsize, sk.back, true)},
		}),
	}
	cursor := " █"
	if !f.blink(time.Second) {
		cursor = "  "
	}
	if d := f.detail(); d != "" {
		left = append(left, sk.label(d+cursor, u*0.04, sk.warn, false))
	}
	var pager gogui.View
	if zones := f.zones(); len(zones) > 0 {
		lineH := u * 0.05
		most := max(int((f.vh-y-u*0.16-tsize-u*0.2)/(lineH+u*0.012)), 1)
		slices.SortStableFunc(zones, func(a, b zoneLine) int {
			if a.alert != b.alert {
				if a.alert {
					return -1
				}
				return 1
			}
			return 0
		})
		if len(zones) > most {
			pages := (len(zones) + most - 1) / most
			page := int(f.v.zonePage.Load()) % pages
			from, to := page*most, min(page*most+most, len(zones))
			pager = f.zonePager(sk, u*0.085, from, to, len(zones), pages)
			zones = zones[from:to]
		}
		for _, z := range zones {
			fill, ink := sk.back, sk.ink
			if z.alert {
				fill, ink = sk.chosen, sk.chosenInk
			} else if z.muted {
				ink = sk.muted
			}
			left = append(left, gogui.Row(gogui.ContainerCfg{
				Width: leftW, Height: lineH, Sizing: gogui.FixedFixed, Color: fill,
				Padding: gogui.PadVH(0, u*0.015), VAlign: gogui.VAlignMiddle,
				Spacing: gogui.SpacingPx(u * 0.03),
				Content: []gogui.View{
					sk.label(z.number, lineH*0.6, ink, false),
					sk.label(z.name, lineH*0.6, ink, false),
					gogui.Row(gogui.ContainerCfg{Sizing: gogui.FillFit, Padding: gogui.NoPadding}),
					sk.label("[ "+z.state+" ]", lineH*0.6, ink, false),
				},
			}))
		}
	} else {
		for _, l := range f.entries(6) {
			left = append(left, sk.label(l, u*0.036, sk.muted, false))
		}
	}
	views = append(views, place(m, y, leftW, f.vh-y-u*0.16, gogui.Column(gogui.ContainerCfg{
		Sizing: gogui.FillFill, Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(u * 0.012),
		Clip: true, Content: left,
	})))

	rx := m + leftW + m
	rw := f.vw - rx - m
	cmds := []gogui.View{sk.label("> "+say.T("homealarm.terminal.command"), u*0.045, sk.ink, false)}
	rowH := u * 0.085
	for i, a := range append(f.actions(), f.toggles()...) {
		text := "[" + strconv.Itoa(i+1) + "] " + a.label
		fill, ink := sk.back, sk.ink
		if i == 0 {
			fill, ink = sk.chosen, sk.chosenInk
			text = "> " + text
		}
		cmds = append(cmds, f.key(key{
			id: a.id, w: rw, h: rowH, fill: fill, ink: ink, align: gogui.HAlignLeft,
			content: []gogui.View{sk.label(text, rowH*0.45, ink, false)},
			run:     a.run,
		}))
	}
	views = append(views,
		place(rx-m/2, y, max(1, u*0.004), f.vh-y-u*0.18, gogui.Row(gogui.ContainerCfg{
			Sizing: gogui.FillFill, Color: sk.muted, Padding: gogui.NoPadding,
		})),
		place(rx, y, rw, f.vh-y-u*0.18, gogui.Column(gogui.ContainerCfg{
			Sizing: gogui.FillFill, Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(u * 0.015),
			Content: cmds,
		})),
		place(m, f.vh-m-u*0.11, f.vw-2*m, max(1, u*0.004), line(f.vw-2*m)),
	)
	foot := u * 0.085
	if f.emergency() {
		views = append(views, place(m, f.vh-m-foot, f.vw*0.3, foot, f.emergencyKey(
			sk, f.vw*0.3, foot, sk.chosen, sk.chosenInk)))
	}
	if pager != nil {
		views = append(
			views,
			place(f.vw*0.4, f.vh-m-foot, f.vw*0.6-m, foot, gogui.Row(gogui.ContainerCfg{
				Sizing:  gogui.FillFill,
				Padding: gogui.NoPadding,
				HAlign:  gogui.HAlignRight,
				VAlign:  gogui.VAlignMiddle,
				Content: []gogui.View{pager},
			})),
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

func (f *face) zonePager(sk skin, h float32, from, to, of, pages int) gogui.View {
	turn := func(by int32) func() {
		return func() {
			f.v.zonePage.Store((f.v.zonePage.Load() + by + int32(pages)) % int32(pages))
			f.v.o.Shell.Redraw()
		}
	}
	arrow := func(id, text string, by int32) gogui.View {
		return f.key(key{
			id:       id,
			w:        h * 1.4,
			h:        h,
			fill:     sk.key,
			ink:      sk.ink,
			edge:     sk.edge,
			edgeWide: sk.edgeWide,
			radius:   sk.radius,
			align:    gogui.HAlignCenter,
			content:  []gogui.View{sk.label(text, h*0.45, sk.ink, true)},
			run:      turn(by),
		})
	}
	text := say.F("homealarm.zones.page", map[string]any{"From": from + 1, "To": to, "Of": of})
	return gogui.Row(gogui.ContainerCfg{
		Padding: gogui.NoPadding, VAlign: gogui.VAlignMiddle, Spacing: gogui.SpacingPx(h * 0.25),
		Content: []gogui.View{
			sk.label(text, h*0.42, sk.muted, false),
			arrow("zones-prev", "<", -1),
			arrow("zones-next", ">", 1),
		},
	})
}
