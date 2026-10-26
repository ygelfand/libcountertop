package homealarmview

import (
	"fmt"
	"slices"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/say"
)

func blueprintLook(f *face) (gogui.View, skin) {
	sk := f.wear(skin{
		flat:      true,
		back:      hex(0x1d4f91),
		panel:     hex(0x1d4f91),
		key:       hex(0x2a5ea3),
		ink:       hex(0xeef4ff),
		muted:     hex(0x9fbbe0),
		calm:      hex(0xeef4ff),
		armed:     hex(0xff7a6b),
		warn:      hex(0xff7a6b),
		alarm:     hex(0xff7a6b),
		chosen:    hex(0x3c6fb4),
		chosenInk: hex(0xeef4ff),
		edge: hex(
			0xc7d9f2,
		),
		edgeWide: max(1.5, f.unit()*0.003),
		family:   style.MonoFamily,
		upper:    true,
	})
	u := f.unit()
	m := u * 0.06
	line := max(1, u*0.002)
	grid := gogui.DrawCanvas(gogui.DrawCanvasCfg{
		ID: "homealarm-blueprint", Width: f.vw, Height: f.vh, Sizing: gogui.FixedFixed,
		Version: uint64(f.vw)<<16 | uint64(f.vh),
		OnDraw: func(dc *gogui.DrawContext) {
			step := u * 0.05
			faint := sk.edge.WithOpacity(0.12)
			for x := float32(0); x < f.vw; x += step {
				dc.Line(x, 0, x, f.vh, faint, line)
			}
			for y := float32(0); y < f.vh; y += step {
				dc.Line(0, y, f.vw, y, faint, line)
			}
			in := u * 0.02
			dc.Rect(in, in, f.vw-2*in, f.vh-2*in, sk.edge, line*1.5)
			dc.Rect(in*1.5, in*1.5, f.vw-3*in, f.vh-3*in, sk.edge.WithOpacity(0.6), line)
		},
	})
	scan := f.phase(7 * time.Second)
	views := []gogui.View{
		grid,
		place(0, float32(scan)*f.vh, f.vw, u*0.05, gogui.Column(gogui.ContainerCfg{
			Sizing:  gogui.FillFill,
			Padding: gogui.NoPadding,
			Gradient: &gogui.GradientDef{
				Direction: gogui.GradientToBottom,
				Stops: []gogui.GradientStop{
					{Color: sk.edge.WithOpacity(0), Pos: 0},
					{Color: sk.edge.WithOpacity(0.16), Pos: 0.9},
					{Color: sk.edge.WithOpacity(0.35), Pos: 1},
				},
			},
		})),
	}

	leftW := f.vw*0.6 - m
	title := f.title()
	thin := f.fit(skin{upper: true}, title, leftW, u*0.2, false)
	left := []gogui.View{
		sk.label("<— "+say.T("homealarm.neon.state")+" —>", u*0.032, sk.muted, false),
		skin{upper: true}.label(title, thin, sk.ink, false),
	}
	if d := f.detail(); d != "" {
		left = append(left, gogui.Row(gogui.ContainerCfg{
			Padding: gogui.NoPadding,
			Spacing: gogui.SpacingPx(u * 0.02),
			VAlign:  gogui.VAlignMiddle,
			Content: []gogui.View{
				gogui.Column(
					gogui.ContainerCfg{
						Width:  u * 0.06,
						Height: u * 0.06,
						Sizing: gogui.FixedFixed,
						Radius: gogui.RadiusPx(
							u * 0.03,
						),
						ColorBorder: sk.warn,
						SizeBorder:  gogui.BorderPx(line * 1.5),
						HAlign:      gogui.HAlignCenter,
						VAlign:      gogui.VAlignMiddle,
						Padding:     gogui.NoPadding,
						Content:     []gogui.View{sk.label("1", u*0.035, sk.warn, true)},
					},
				),
				sk.label(d, u*0.036, sk.warn, false),
			},
		}))
	}
	views = append(views, place(m, m, leftW, f.vh*0.45, gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFill,
		Padding: gogui.NoPadding,
		Spacing: gogui.SpacingPx(u * 0.025),
		Content: left,
	})))

	log := f.v.history()
	rowH := u * 0.055
	table := []gogui.View{sk.label(say.T("homealarm.blueprint.log"), u*0.034, sk.ink, true)}
	cell := func(text string, w float32, c gogui.Color, bold bool) gogui.View {
		return gogui.Row(gogui.ContainerCfg{
			Width:       w,
			Height:      rowH,
			Sizing:      gogui.FixedFixed,
			ColorBorder: sk.edge.WithOpacity(0.5),
			SizeBorder: gogui.BorderPx(
				line,
			),
			Radius:  gogui.NoRadius,
			Padding: gogui.PadVH(0, u*0.015),
			VAlign:  gogui.VAlignMiddle,
			Content: []gogui.View{sk.label(text, rowH*0.45, c, bold)},
		})
	}
	if zones := f.zones(); len(zones) > 0 {
		table[0] = sk.label(say.T("homealarm.zones"), u*0.034, sk.ink, true)
		zw := []float32{leftW * 0.1, leftW * 0.38, leftW * 0.21, leftW * 0.24}
		table = append(
			table,
			gogui.Row(gogui.ContainerCfg{Padding: gogui.NoPadding, Content: []gogui.View{
				cell("No.", zw[0], sk.muted, false),
				cell(say.T("homealarm.blueprint.location"), zw[1], sk.muted, false),
				cell(say.T("homealarm.blueprint.type"), zw[2], sk.muted, false),
				cell(say.T("homealarm.flip.state"), zw[3], sk.muted, false),
			}}),
		)
		most := max(int((f.vh*0.5-m-u*0.05)/rowH)-2, 1)
		slices.SortStableFunc(zones, func(a, b zoneLine) int {
			if a.alert != b.alert {
				if a.alert {
					return -1
				}
				return 1
			}
			return 0
		})
		for i, z := range zones {
			if i >= most {
				break
			}
			c := sk.ink
			if z.alert {
				c = sk.warn
			} else if z.muted {
				c = sk.muted
			}
			table = append(
				table,
				gogui.Row(gogui.ContainerCfg{Padding: gogui.NoPadding, Content: []gogui.View{
					cell(z.number, zw[0], c, false),
					cell(z.name, zw[1], c, false),
					cell(z.kind, zw[2], c, false),
					cell(z.state, zw[3], c, true),
				}}),
			)
		}
		log = nil
	}
	cw := []float32{leftW * 0.12, leftW * 0.24, leftW * 0.57}
	if len(log) > 0 {
		table = append(
			table,
			gogui.Row(gogui.ContainerCfg{Padding: gogui.NoPadding, Content: []gogui.View{
				cell("No.", cw[0], sk.muted, false),
				cell(say.T("homealarm.flip.time"), cw[1], sk.muted, false),
				cell(say.T("homealarm.flip.state"), cw[2], sk.muted, false),
			}}),
		)
	}
	for i := len(log) - 1; i >= 0 && len(log)-i <= 5; i-- {
		e := log[i]
		c := sk.ink
		if e.status.Urgent() {
			c = sk.warn
		}
		table = append(
			table,
			gogui.Row(gogui.ContainerCfg{Padding: gogui.NoPadding, Content: []gogui.View{
				cell(
					twoDigits(len(log)-i),
					cw[0],
					c,
					false,
				),
				cell(e.at.Format("15:04"), cw[1], c, false),
				cell(say.T("homealarm.status."+string(e.status)), cw[2], c, true),
			}}),
		)
	}
	views = append(views, place(m, f.vh*0.5, leftW, f.vh*0.5-m, gogui.Column(gogui.ContainerCfg{
		Sizing: gogui.FillFill, Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(u * 0.015),
		VAlign: gogui.VAlignBottom, Content: table,
	})))

	rx := m + leftW + m*0.6
	rw := f.vw - rx - m
	acts := f.actions()
	infoH := u * 0.075
	gap := u * 0.025
	top := m + f.cornerWidth()*0.45
	avail := f.vh - m - infoH - gap - top
	emH := float32(0)
	if f.emergency() {
		emH = min(u*0.11, avail*0.24)
		avail -= emH + gap
	}
	n := float32(max(len(acts), 1))
	boxH := min(u*0.15, (avail-(n-1)*gap)/n)
	badge := boxH * 0.62
	y := top
	for i, a := range acts {
		fill := gogui.RGBA(0, 0, 0, 0)
		if a.current || len(acts) > 1 && i == len(acts)/2 {
			fill = sk.chosen
		}
		letter := string(rune('A' + i))
		views = append(views,
			place(rx, y+(boxH-badge)/2, badge, badge, gogui.Column(gogui.ContainerCfg{
				Width:       badge,
				Height:      badge,
				Sizing:      gogui.FixedFixed,
				Radius:      gogui.RadiusPx(badge / 2),
				ColorBorder: sk.edge,
				SizeBorder:  gogui.BorderPx(line * 1.5),
				HAlign:      gogui.HAlignCenter,
				VAlign:      gogui.VAlignMiddle,
				Padding:     gogui.NoPadding,
				Content:     []gogui.View{sk.label(letter, badge*0.42, sk.ink, true)},
			})),
			place(rx+badge+u*0.025, y, rw-badge-u*0.025, boxH, f.key(key{
				id: a.id, w: rw - badge - u*0.025, h: boxH, fill: fill, ink: sk.ink, edge: sk.edge,
				edgeWide: sk.edgeWide, align: gogui.HAlignLeft,
				content: []gogui.View{
					icon(a.glyph, boxH*0.34, sk.ink),
					sk.label(a.label, f.fit(sk, a.label, rw*0.5, boxH*0.3, false), sk.ink, false),
				},
				run: a.run,
			})),
		)
		y += boxH + gap
	}
	if f.emergency() {
		ey := f.vh - m - infoH - gap - emH
		views = append(
			views,
			place(rx, ey, rw, emH, f.hatch(rw, emH, sk.warn)),
			place(rx, ey, rw, emH, f.emergencyKey(sk, rw, emH, gogui.RGBA(0, 0, 0, 0), sk.warn)),
		)
	}

	infoW := rw / 3
	info := func(label, value string) gogui.View {
		return gogui.Column(gogui.ContainerCfg{
			Width:       infoW,
			Height:      infoH,
			Sizing:      gogui.FixedFixed,
			ColorBorder: sk.edge.WithOpacity(0.6),
			SizeBorder:  gogui.BorderPx(line),
			Padding:     gogui.PadVH(u*0.006, u*0.012),
			Content: []gogui.View{
				sk.label(label, infoH*0.26, sk.muted, false),
				sk.label(value, infoH*0.36, sk.ink, true),
			},
		})
	}
	link := say.T("homealarm.plate.lost")
	if f.state.Known() {
		link = say.T("homealarm.plate.normal")
	}
	views = append(views, place(rx, f.vh-m-infoH, rw, infoH, gogui.Row(gogui.ContainerCfg{
		Padding: gogui.NoPadding,
		Content: []gogui.View{
			info(say.T("homealarm.blueprint.date"), f.now.Format("02.01.06")),
			info(say.T("homealarm.blueprint.time"), f.now.Format("15:04")),
			info(say.T("homealarm.blueprint.link"), link),
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

func (f *face) hatch(w, h float32, c gogui.Color) gogui.View {
	return gogui.DrawCanvas(gogui.DrawCanvasCfg{
		ID: "homealarm-hatch", Width: w, Height: h, Sizing: gogui.FixedFixed,
		Version: uint64(w)<<16 | uint64(h),
		OnDraw: func(dc *gogui.DrawContext) {
			thick := max(1.5, f.unit()*0.004)
			dc.Rect(0, 0, w, h, c, thick)
			step := h * 0.25
			for x := -h; x < w; x += step {
				x0, x1 := max(x, 0), min(x+h, w)
				dc.Line(x0, h-(x0-x), x1, h-(x1-x), c.WithOpacity(0.35), max(1, thick*0.5))
			}
		},
	})
}

func twoDigits(n int) string { return fmt.Sprintf("%02d", n) }
