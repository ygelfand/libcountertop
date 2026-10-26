package homealarmview

import (
	"math"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/say"
)

const (
	dialTicks = 72
	dialSpan  = 0.78
)

func dialLook(f *face) (gogui.View, skin) {
	sk := f.wear(skin{
		back: hex(0x0d0a1a), panel: hex(0x161127), key: hex(0x1a2240), ink: hex(0xf3ecff),
		muted: hex(0x8f87a8), calm: hex(0xff2e88), armed: hex(0x2ef0ff), warn: hex(0xffd23f),
		alarm: hex(0xff2e5a), chosen: hex(0x2ef0ff), chosenInk: hex(0x0d0a1a),
		edge: hex(0x2ef0ff), edgeWide: max(2, f.unit()*0.004), radius: 0,
		upper: true,
	})
	u := f.unit()
	m := u * 0.06
	tone := f.tone(sk)
	acts := f.actions()
	trigger := f.emergency()

	d := min(f.vh*0.9, f.vw*0.5)
	cx, cy := f.vw/2, f.vh/2
	rIn, rOut := d*0.33, d*0.45
	core := d * 0.29

	type seg struct {
		mid, sweep float64
		label      string
		run        func()
		fill, ink  gogui.Color
		hold       bool
	}
	var segs []seg
	n := len(acts)
	span := 2 * math.Pi * dialSpan
	if !trigger {
		span = 2 * math.Pi * 0.98
	}
	if n > 0 {
		each := span / float64(n)
		start := -math.Pi/2 - span/2
		for i, a := range acts {
			fill, ink := sk.key, sk.chosen
			if a.current || n == 1 || i == n/2 {
				fill, ink = sk.chosen, sk.chosenInk
			}
			segs = append(segs, seg{
				mid: start + each*(float64(i)+0.5), sweep: each * 0.9, label: a.label, run: a.run,
				fill: fill, ink: ink,
			})
		}
	}
	if trigger {
		gapSpan := 2*math.Pi - span
		segs = append(segs, seg{
			mid:   math.Pi / 2,
			sweep: gapSpan * 0.85,
			label: say.T("homealarm.panic"),
			run:   f.v.trigger,
			fill:  hex(0x3a0f2a),
			ink:   sk.alarm,
			hold:  true,
		})
	}

	ver := uint64(
		len(segs),
	)<<40 | uint64(
		tone.R,
	)<<16 | uint64(
		tone.G,
	)<<8 | uint64(
		tone.B,
	) | uint64(
		d,
	)<<24
	views := []gogui.View{
		place(m, m, f.vw*0.3, u*0.3, gogui.Column(gogui.ContainerCfg{
			Sizing: gogui.FillFill, Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(u * 0.01),
			Content: []gogui.View{
				sk.label(f.clock(), u*0.15, sk.ink, true),
				skin{
					upper:  true,
					spaced: false,
				}.label(
					f.now.Format("Mon 2 Jan"),
					u*0.045,
					sk.muted,
					false,
				),
			},
		})),
		place(cx-d/2, cy-d/2, d, d, gogui.DrawCanvas(gogui.DrawCanvasCfg{
			ID: "homealarm-dial", Width: d, Height: d, Sizing: gogui.FixedFixed, Version: ver,
			OnDraw: func(dc *gogui.DrawContext) {
				c := d / 2
				for i := range dialTicks {
					a := float64(i) / dialTicks * 2 * math.Pi
					long := i%6 == 0
					r0 := d * 0.47
					r1 := d * 0.485
					if long {
						r1 = d * 0.5
					}
					ca, sa := float32(math.Cos(a)), float32(math.Sin(a))
					dc.Line(
						c+ca*r0,
						c+sa*r0,
						c+ca*r1,
						c+sa*r1,
						sk.edge.WithOpacity(0.6),
						max(1, d*0.003),
					)
				}
				for _, s := range segs {
					start := float32(s.mid - s.sweep/2)
					dc.FilledArc(c, c, rOut, rOut, start, float32(s.sweep), s.fill)
					dc.FilledArc(c, c, rIn, rIn, start, float32(s.sweep), sk.back)
					dc.Arc(c, c, rOut, rOut, start, float32(s.sweep), s.ink, max(2, d*0.006))
				}
				dc.FilledCircle(c, c, core, sk.back)
				dc.Circle(c, c, core, tone, max(2, d*0.008))
			},
		})),
	}
	for i, s := range segs {
		rm := float64(rIn+rOut) / 2
		sx := cx + float32(math.Cos(s.mid)*rm)
		sy := cy + float32(math.Sin(s.mid)*rm)
		bw := float32(float64(rm)*s.sweep) * 0.8
		bh := (rOut - rIn) * 0.9
		k := key{
			id:    "dial-" + string(rune('a'+i)),
			w:     bw,
			h:     bh,
			align: gogui.HAlignCenter,
			content: []gogui.View{
				sk.label(s.label, f.fit(sk, s.label, bw*0.9, bh*0.34, true), s.ink, true),
			},
			run: s.run,
		}
		var v gogui.View
		if s.hold {
			v = f.holdKey(k)
		} else {
			v = f.key(k)
		}
		views = append(views, place(sx-bw/2, sy-bh/2, bw, bh, v))
	}
	title := f.title()
	inner := core * 1.6
	mid := []gogui.View{
		icon(f.glyph(), core*0.3, tone),
		sk.label(title, f.fit(sk, title, inner, core*0.26, true), sk.ink, true),
	}
	if det := f.detail(); det != "" {
		mid = append(mid, sk.label(det, f.fit(sk, det, inner, core*0.11, false), sk.warn, false))
	}
	views = append(
		views,
		place(cx-inner/2, cy-inner/2, inner, inner, gogui.Column(gogui.ContainerCfg{
			Sizing: gogui.FillFill, Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(core * 0.05),
			HAlign: gogui.HAlignCenter, VAlign: gogui.VAlignMiddle, Content: mid,
		})),
	)
	lines, heading := f.entries(3), say.T("homealarm.recent")
	if len(f.dev.Zones) > 0 {
		lines, heading = nil, say.T("homealarm.faults")
		for _, z := range f.zones() {
			if z.alert {
				lines = append(lines, "Z"+z.number+" "+z.name+" "+z.state)
			}
		}
		lines = append(lines, f.troubles()...)
		if len(lines) > 3 {
			lines = lines[:3]
		}
	}
	if len(lines) > 0 {
		log := []gogui.View{sk.label(heading, u*0.03, sk.muted, true)}
		for _, l := range lines {
			log = append(log, gogui.Row(gogui.ContainerCfg{
				ColorBorder: sk.warn.WithOpacity(0.6), SizeBorder: gogui.BorderPx(max(1, u*0.003)),
				Padding: gogui.PadVH(u*0.012, u*0.02),
				Content: []gogui.View{sk.label(l, u*0.032, sk.warn, false)},
			}))
		}
		views = append(
			views,
			place(m, f.vh-m-u*0.36, f.vw*0.26, u*0.36, gogui.Column(gogui.ContainerCfg{
				Sizing:  gogui.FillFill,
				Padding: gogui.NoPadding,
				Spacing: gogui.SpacingPx(u * 0.015),
				VAlign:  gogui.VAlignBottom,
				Content: log,
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
