package thermostatview

import (
	"math"
	"strings"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	kit "github.com/ygelfand/libcountertop/pkg/display/panelkit"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/say"
)

var none = gogui.RGBA(0, 0, 0, 0)

func outlined(sk skin, c choice, r rect, on, off gogui.Color, f *face, glyph bool) btn {
	edge := ink(on, c.current, off)
	b := btn{
		fill: ink(on.WithOpacity(0.14), c.current, none), edge: edge,
		edgeWide: sk.edgeWide, radius: sk.radius,
		content: f.modeContent(sk, c, r, edge, glyph),
	}
	if c.current {
		b.glow = on.WithOpacity(0.45)
	}
	return b
}

func hudLook(f *face) (gogui.View, skin) {
	cyan, dim := kit.Hex(0x5ff2ff), kit.Hex(0x2a7f8c)
	sk := f.wear(skin{
		back: kit.Hex(0x02080c), ink: cyan, muted: dim, accent: cyan, accentInk: kit.Hex(0x02080c),
		heat: kit.Hex(0xffa25f), cool: cyan, edge: cyan, edgeWide: max(1.5, f.u()*0.004),
		text: kit.Type{Upper: true, Thin: true}, digits: kit.Type{Thin: true}, glow: true,
	})
	s := f.spots(plan{arrows: sides, head: 0.07, modes: 0.15, arrow: 0.13})
	u := f.u()
	ring := min(s.body.w, s.body.h) * 0.98
	cx, cy := f.vw/2, s.body.cy()
	views := []gogui.View{
		f.canvas("hud-rings", rect{cx - ring/2, cy - ring/2, ring, ring},
			kit.Version(ring, float32(f.tone(sk).R)), func(dc *gogui.DrawContext) {
				c := ring / 2
				dc.Circle(c, c, c*0.95, dim.WithOpacity(0.6), max(1, ring*0.003))
				dc.Arc(c, c, c*0.86, c*0.86, -2.4, 1.6, cyan.WithOpacity(0.8), max(1.5, ring*0.006))
				dc.Arc(c, c, c*0.86, c*0.86, 0.75, 1.6, cyan.WithOpacity(0.8), max(1.5, ring*0.006))
				dc.Circle(c, c, c*0.78, dim.WithOpacity(0.35), max(1, ring*0.002))
			}),
		f.centered(sk, rect{s.body.x, s.head.y, s.body.w, s.head.h}, f.name(), dim, false),
		s.number.place(f.reading(sk, rect{s.number.x, s.number.y, s.number.w, s.number.h},
			digits{ink: cyan, letter: true})),
		f.centered(
			sk,
			s.facts,
			strings.TrimSpace(say.T("thermostat.humidity")+": "+f.humidity()),
			cyan,
			false,
		),
	}
	half := rect{s.up.x, s.up.y, s.up.w, s.up.h}
	for _, a := range []struct {
		id string
		r  rect
		up bool
	}{
		{"up", rect{half.x, half.y - half.h*0.6, half.w, half.h}, true},
		{"down", rect{half.x, half.y + half.h*0.6, half.w, half.h}, false},
		{"up2", rect{s.down.x, s.down.y - s.down.h*0.6, s.down.w, s.down.h}, true},
		{"down2", rect{s.down.x, s.down.y + s.down.h*0.6, s.down.w, s.down.h}, false},
	} {
		glyph := "mdi:triangle-outline"
		if !a.up {
			glyph = "mdi:triangle-down-outline"
		}
		views = append(views, a.r.place(f.arrow(sk, a.id, a.r, a.up, glyph, btn{ink: cyan})))
	}
	bar := f.modeBar(0.84, 0.15)
	views = append(views, bar.place(f.modes(bar, u*0.02, func(c choice, r rect) btn {
		tint := ink(f.tone(sk), c.current, dim)
		size := sk.text.Fit(f.w, c.label, r.w*0.8, r.h*0.22, false)
		return btn{content: []gogui.View{gogui.Column(gogui.ContainerCfg{
			HAlign: gogui.HAlignCenter, Spacing: gogui.SpacingPx(r.h * 0.05),
			Content: []gogui.View{
				kit.Icon(c.glyph, r.h*0.4, tint),
				sk.text.Label(c.label, size, tint, false),
			},
		})}}
	})))
	return f.facade(sk, views...), sk
}

func gridLook(f *face) (gogui.View, skin) {
	cyan, dim, white := kit.Hex(0x45e6ff), kit.Hex(0x1f6f80), kit.Hex(0xe8fdff)
	sk := f.wear(skin{
		back: kit.Hex(0x010a10), ink: cyan, muted: dim, accent: cyan, accentInk: kit.Hex(0x010a10),
		heat: kit.Hex(0xff6f7a), cool: cyan, edge: cyan, edgeWide: max(2, f.u()*0.005),
		radius: f.u() * 0.01, text: kit.Type{Upper: true}, digits: kit.Type{}, glow: true,
	})
	u := f.u()
	m := u * 0.05
	head := rect{m, m * 0.6, f.vw - 2*m - f.reserve, u * 0.06}
	modes := f.modeBar(0.6, 0.12)
	top := head.y + head.h + m*0.5
	bodyH := modes.y - top - m*0.5
	d := min(bodyH, f.vw*0.55)
	if f.tall() {
		d = min(bodyH*0.7, f.vw*0.8)
	}
	ring := rect{f.vw/2 - d/2, top + (bodyH-d)/2, d, d}
	a := d * 0.15
	up := rect{ring.x + ring.w + a*0.2, ring.cy() - a*1.1, a, a}
	down := rect{up.x, ring.cy() + a*0.1, a, a}
	if up.x+a > f.vw-m*0.5 {
		up.x, down.x = ring.x+ring.w-a, ring.x+ring.w-a
	}
	inner := ring.inset(d * 0.2)
	working := f.working()
	spin := 0.0
	if working {
		spin = f.phase(8*time.Second) * 2 * math.Pi
	}
	views := []gogui.View{
		f.centered(sk, head, f.name()+" ["+f.status()+"]", cyan, false),
		f.canvas(
			"grid-ring",
			ring,
			kit.Version(d, float32(spin), float32(f.tone(sk).G)),
			func(dc *gogui.DrawContext) {
				c := d / 2
				w := max(2, d*0.012)
				dc.Circle(c, c, c*0.96, dim, max(1, d*0.004))
				for i := range 4 {
					st := float32(spin) + float32(i)*math.Pi/2 + 0.12
					dc.Arc(c, c, c*0.88, c*0.88, st, math.Pi/2-0.24, cyan, w*1.6)
				}
				dc.Arc(
					c,
					c,
					c*0.76,
					c*0.76,
					-float32(spin)*0.6,
					math.Pi*1.4,
					white.WithOpacity(0.8),
					w,
				)
				dc.Circle(c, c, c*0.68, dim.WithOpacity(0.6), max(1, d*0.003))
			},
		),
		inner.place(f.reading(sk, rect{inner.x, inner.y, inner.w, inner.h * 0.78},
			digits{ink: cyan, letter: true})),
		f.centered(sk, rect{inner.x, inner.y + inner.h*0.74, inner.w, inner.h * 0.14},
			say.T("thermostat.temperature"), cyan, false),
		up.place(f.arrow(sk, "up", up, true, "mdi:chevron-up", btn{ink: cyan})),
		down.place(f.arrow(sk, "down", down, false, "mdi:chevron-down", btn{ink: cyan})),
		modes.place(f.modes(modes, u*0.03, func(c choice, r rect) btn {
			on := ink(f.tone(sk), c.current, white)
			return outlined(sk, c, r, on, dim.WithOpacity(0.9), f, true)
		})),
	}
	if h := f.humidity(); h != "" && f.wide() && f.vw > u*1.4 {
		box := rect{f.vw - m - u*0.32, top + f.reserve*0.25, u * 0.32, u * 0.22}
		views = append(views,
			box.place(gogui.Column(gogui.ContainerCfg{
				Width:       box.w,
				Height:      box.h,
				Sizing:      gogui.FixedFixed,
				ColorBorder: cyan,
				SizeBorder:  gogui.BorderPx(sk.edgeWide),
				Radius:      gogui.RadiusPx(u * 0.02),
				Padding: gogui.PadAll(
					u * 0.015,
				),
				HAlign: gogui.HAlignCenter,
				VAlign: gogui.VAlignMiddle,
				Content: []gogui.View{
					sk.digits.Fitted(f.w, h, box.w*0.7, box.h*0.45, cyan, false),
					sk.text.Fitted(
						f.w,
						say.T("thermostat.humidity"),
						box.w*0.8,
						box.h*0.16,
						cyan,
						false,
					),
				},
			})))
	}
	return f.facade(sk, views...), sk
}

func tarsLook(f *face) (gogui.View, skin) {
	cyan, dim, back := kit.Hex(0x3ee6f0), kit.Hex(0x1b6870), kit.Hex(0x020606)
	sk := f.wear(skin{
		back: back, ink: cyan, muted: dim, accent: cyan, accentInk: back, heat: cyan, cool: cyan,
		edge: cyan, edgeWide: max(2, f.u()*0.004), text: kit.Type{Upper: true}, digits: kit.Type{},
	})
	u := f.u()
	m := u * 0.04
	head := rect{m, m * 0.6, f.vw - 2*m - f.reserve, u * 0.05}
	modes := rect{m, f.vh - m - min(u*0.14, f.vh*0.16), f.vw - 2*m, min(u*0.14, f.vh*0.16)}
	top := head.y + head.h + m
	bodyH := modes.y - top - m
	arcW := float32(0)
	if f.wide() {
		arcW = min(f.vw*0.2, bodyH*0.6)
	}
	body := rect{m + arcW, top, f.vw - 2*m - arcW, bodyH}
	a := min(body.h*0.22, u*0.13)
	up := rect{body.x + body.w*0.04, body.cy() - a/2, a, a}
	down := rect{body.x + body.w*0.96 - a, body.cy() - a/2, a, a}
	status := rect{body.x + a*1.4, body.y, body.w - a*2.8, body.h * 0.2}
	num := rect{up.x + a*1.2, body.y + body.h*0.2, down.x - up.x - a*1.4, body.h * 0.6}
	facts := rect{num.x, num.y + num.h, num.w, body.h * 0.14}
	target := say.T("thermostat.target") + ": " + f.number(f.setpoint()) + "°" + f.letter()
	views := []gogui.View{
		f.line(
			sk,
			rect{head.x, head.y, head.w * 0.6, head.h},
			f.name()+" | "+say.T("thermostat.app"),
			cyan,
			false,
		),
		f.right(
			sk,
			rect{head.x + head.w*0.6, head.y, head.w * 0.4, head.h},
			f.clock(),
			cyan,
			false,
		),
		gogui.Column(gogui.ContainerCfg{
			Width:   f.vw,
			Height:  1,
			Sizing:  gogui.FixedFixed,
			Padding: gogui.NewPadding(head.y+head.h+m*0.3, 0, 0, 0),
			Content: []gogui.View{gogui.Row(gogui.ContainerCfg{
				Width: f.vw, Height: max(1, u*0.002), Sizing: gogui.FixedFixed, Color: dim,
			})},
		}),
		f.centered(sk, rect{status.x, status.y, status.w, status.h * 0.5}, target, cyan, false),
		f.sweep("tars-sweep", rect{0, head.y + head.h + m*0.3 - u*0.004, f.vw, u * 0.01}, cyan),
		f.centered(sk, rect{status.x, status.y + status.h*0.5, status.w, status.h * 0.5},
			say.T("thermostat.status")+": "+f.status(), cyan, false),
		f.lamp(rect{0, head.y + head.h + m*0.3 - u*0.012, f.vw, u * 0.024}, cyan, back),
		num.place(f.reading(sk, num, digits{ink: cyan, letter: true})),
		f.centered(sk, facts, f.humidity(), cyan, false),
		up.place(
			f.arrow(sk, "up", up, true, "mdi:chevron-up", btn{edge: cyan, edgeWide: sk.edgeWide}),
		),
		down.place(
			f.arrow(
				sk,
				"down",
				down,
				false,
				"mdi:chevron-down",
				btn{edge: cyan, edgeWide: sk.edgeWide},
			),
		),
		modes.place(f.modes(modes, u*0.03, func(c choice, r rect) btn {
			label := "(" + c.label + ")"
			on := ink(cyan, c.current, cyan.WithOpacity(0.75))
			b := btn{
				fill: ink(cyan.WithOpacity(0.18), c.current, none), edge: on, edgeWide: sk.edgeWide,
				content: []gogui.View{sk.text.Fitted(f.w, label, r.w*0.7, r.h*0.36, on, false)},
			}
			return b
		})),
	}
	if arcW > 0 {
		arc := rect{m, top + bodyH*0.05, arcW, bodyH * 0.9}
		lo, hi := f.st.Min, f.st.Max
		if hi <= lo {
			lo, hi = 60, 80
		}
		at := f.setpoint()
		views = append(
			views,
			f.canvas("tars-arc", arc, kit.Version(arc.w, arc.h, float32(valueOr(at)), float32(lo)),
				func(dc *gogui.DrawContext) {
					cx, cy, rr := arc.w*1.05, arc.h/2, arc.h*0.48
					dc.Arc(cx, cy, rr, rr, math.Pi*0.62, math.Pi*0.76, cyan, max(4, arc.w*0.08))
					dc.Arc(
						cx,
						cy,
						rr*0.86,
						rr*0.86,
						math.Pi*0.64,
						math.Pi*0.72,
						dim,
						max(1.5, arc.w*0.01),
					)
					if at != nil {
						k := (*at - lo) / (hi - lo)
						a := float32(math.Pi*1.38 - min(max(k, 0), 1)*math.Pi*0.76)
						ca, sa := float32(math.Cos(float64(a))), float32(math.Sin(float64(a)))
						dc.Line(
							cx+ca*rr*0.8,
							cy+sa*rr*0.8,
							cx+ca*rr*1.08,
							cy+sa*rr*1.08,
							kit.Hex(0xffffff),
							max(3, arc.w*0.03),
						)
					}
				}),
		)
		cx, cy, rr := arc.w*1.05, arc.h/2, arc.h*0.48
		lh := u * 0.06
		for _, end := range []struct {
			angle float64
			value float64
		}{{math.Pi * 1.38, hi}, {math.Pi * 0.62, lo}} {
			x := arc.x + cx + rr*float32(math.Cos(end.angle)) + arc.w*0.1
			y := arc.y + cy + rr*float32(math.Sin(end.angle)) - lh/2
			views = append(views, f.line(sk, rect{x, y, arc.w * 0.7, lh},
				f.number(&end.value)+"°"+f.letter(), cyan, false))
		}
	}
	return f.facade(sk, views...), sk
}

func valueOr(v *float64) float64 {
	if v == nil {
		return -1
	}
	return *v
}

func halLook(f *face) (gogui.View, skin) {
	red, white, back := kit.Hex(0xff3b30), kit.Hex(0xf2f2f2), kit.Hex(0x0b0d10)
	sk := f.wear(skin{
		back: back, ink: red, muted: kit.Hex(0x8a8f96), accent: red, accentInk: white,
		heat: red, cool: red, edge: white, edgeWide: max(2, f.u()*0.004), radius: f.u() * 0.01,
		text: kit.Type{}, digits: kit.Type{},
	})
	u := f.u()
	m := u * 0.05
	eye := min(f.vh*0.28, f.vw*0.3)
	eyeR := rect{f.vw/2 - eye/2, m * 0.6, eye, eye}
	modes := f.modeBar(0.7, 0.12)
	body := rect{m, eyeR.y + eyeR.h + m*0.3, f.vw - 2*m, modes.y - eyeR.y - eyeR.h - m*0.8}
	s := f.spotsIn(body, plan{arrows: above})
	b := f.breath(3 * time.Second)
	views := []gogui.View{
		f.canvas("hal-eye", eyeR, kit.Version(eye, b), func(dc *gogui.DrawContext) {
			c := eye / 2
			dc.FilledCircle(c, c, c, kit.Hex(0x1a1c20))
			dc.FilledCircleGradient(c, c, c*0.94, &gogui.CanvasGradient{
				Radial: true, CX: c * 0.8, CY: c * 0.7, R: c * 1.1, Stops: []gogui.GradientStop{
					{Color: kit.Hex(0x9aa3ad), Pos: 0}, {Color: kit.Hex(0x3b4047), Pos: 1},
				},
			})
			dc.FilledCircle(c, c, c*0.8, kit.Hex(0x050505))
			dc.FilledCircleGradient(
				c,
				c,
				c*0.7,
				&gogui.CanvasGradient{
					Radial: true,
					CX:     c,
					CY:     c,
					R:      c * 0.7,
					Stops: []gogui.GradientStop{
						{Color: kit.Hex(0xffd0b0), Pos: 0},
						{Color: kit.Hex(0xff2010).WithOpacity(0.9 + 0.1*b), Pos: 0.12},
						{Color: kit.Hex(0x8a0500).WithOpacity(0.6 + 0.3*b), Pos: 0.45},
						{Color: kit.Hex(0x050000), Pos: 1},
					},
				},
			)
			dc.FilledCircle(c*0.82, c*0.7, c*0.08, gogui.RGBA(255, 255, 255, 50))
		}),
		s.up.place(f.arrow(sk, "up", s.up, true, "mdi:triangle-outline", btn{ink: white})),
		s.down.place(
			f.arrow(sk, "down", s.down, false, "mdi:triangle-down-outline", btn{ink: white}),
		),
		rect{
			s.number.x + s.number.w*0.2,
			s.number.y,
			s.number.w * 0.6,
			s.number.h + s.status.h + s.facts.h,
		}.
			place(
				f.reading(sk, rect{0, 0, s.number.w * 0.6, s.number.h + s.status.h + s.facts.h},
					digits{ink: red, letter: true}),
			),
		f.line(
			sk,
			rect{s.number.x + s.number.w*0.82, s.number.cy() - u*0.05, s.number.w * 0.18, u * 0.1},
			f.humidity(),
			red,
			false,
		),
		f.line(
			sk,
			rect{s.number.x, s.number.cy() - u*0.03, s.number.w * 0.2, u * 0.06},
			f.status(),
			sk.muted,
			false,
		),
		modes.place(f.modes(modes, u*0.03, func(c choice, r rect) btn {
			label := ink(red, c.current, white)
			return btn{
				edge: ink(red, c.current, white), edgeWide: sk.edgeWide, radius: sk.radius,
				content: []gogui.View{sk.text.Fitted(f.w, c.label, r.w*0.7, r.h*0.4, label, false)},
			}
		})),
	}
	return f.facade(sk, views...), sk
}

func (f *face) spotsIn(r rect, p plan) spots {
	inner := &face{v: f.v, w: f.w, vw: r.w, vh: r.h, st: f.st, unit: f.unit, still: f.still}
	p.margin = 0.001
	s := inner.spots(p)
	shift := func(x rect) rect { return rect{x.x + r.x, x.y + r.y, x.w, x.h} }
	return spots{
		head: shift(
			s.head,
		),
		status: shift(s.status),
		number: shift(s.number),
		facts:  shift(s.facts),
		modes:  shift(s.modes),
		up:     shift(s.up),
		down:   shift(s.down),
		body:   shift(s.body),
	}
}

func matrixLook(f *face) (gogui.View, skin) {
	green, dim, back := kit.Hex(0x9cffad), kit.Hex(0x3c9a52), kit.Hex(0x000300)
	thin := kit.Type{Family: style.MonoFamily, Upper: true, Thin: true}
	sk := f.wear(skin{
		back: back, ink: green, muted: dim, accent: green, accentInk: back, heat: green,
		cool: green, edge: green, edgeWide: max(1, f.u()*0.0025), text: thin,
		digits: kit.Type{Family: style.MonoFamily, Thin: true},
	})
	u := f.u()
	pw, ph := min(f.vw*0.7, u*1.3), min(f.vh*0.66, u*0.86)
	if !f.wide() {
		pw, ph = f.vw*0.9, min(f.vh*0.76, u*1.15)
	}
	panel := rect{(f.vw - pw) / 2, (f.vh - ph) / 2, pw, ph}
	in := panel.inset(min(pw, ph) * 0.07)
	head := rect{in.x, in.y, in.w, in.h * 0.12}
	num := rect{in.x + in.w*0.16, head.y + head.h, in.w * 0.68, in.h * 0.5}
	facts := rect{in.x, num.y + num.h, in.w, in.h * 0.1}
	modes := rect{
		in.x,
		facts.y + facts.h + in.h*0.06,
		in.w,
		in.h - head.h - num.h - facts.h - in.h*0.06,
	}
	a := min(num.h*0.36, in.w*0.12)
	up := rect{in.x, num.cy() - a*1.05, a, a}
	down := rect{in.x, num.cy() + a*0.05, a, a}
	views := []gogui.View{
		panel.place(gogui.Column(gogui.ContainerCfg{
			Width: pw, Height: ph, Sizing: gogui.FixedFixed, Color: back.WithOpacity(0.78),
			ColorBorder: green.WithOpacity(0.6), SizeBorder: gogui.BorderPx(sk.edgeWide),
		})),
		f.line(sk, rect{head.x, head.y, head.w * 0.52, head.h}, "> "+f.name(), dim, false),
		f.right(
			sk,
			rect{head.x + head.w*0.56, head.y, head.w * 0.44, head.h},
			f.status(),
			dim,
			false,
		),
		num.place(matrixNumber(f, sk, num, green)),
		f.centered(sk, facts, say.T("thermostat.humidity")+" "+f.humidity(), green, false),
		up.place(f.arrow(sk, "up", up, true, "mdi:chevron-up", btn{ink: green})),
		down.place(f.arrow(sk, "down", down, false, "mdi:chevron-down", btn{ink: green})),
		rect{in.x + in.w - a, up.y, a, a}.place(
			f.arrow(sk, "up2", rect{0, 0, a, a}, true, "mdi:chevron-up", btn{ink: green})),
		rect{in.x + in.w - a, down.y, a, a}.place(
			f.arrow(sk, "down2", rect{0, 0, a, a}, false, "mdi:chevron-down", btn{ink: green})),
		modes.place(f.modes(modes, in.w*0.03, func(c choice, r rect) btn {
			c2 := ink(green, c.current, dim)
			return btn{
				fill: ink(green.WithOpacity(0.12), c.current, none), edge: c2,
				edgeWide: sk.edgeWide, radius: r.h / 2,
				content: []gogui.View{thin.Fitted(f.w, c.label, r.w*0.7, r.h*0.36, c2, false)},
			}
		})),
	}
	return f.facade(sk, views...), sk
}

func matrixNumber(f *face, sk skin, r rect, c gogui.Color) gogui.View {
	if f.st.Ranged() {
		return f.reading(sk, rect{0, 0, r.w, r.h}, digits{ink: c})
	}
	text := f.number(f.setpoint())
	shown := text
	if !f.still {
		var moving bool
		shown, moving = f.v.settle(f.st.Entity, text, f.now)
		if moving {
			f.animate()
		}
	}
	shown += "°" + f.letter()
	size := sk.digits.Fit(f.w, text+"°"+f.letter(), r.w, r.h, false)
	return kit.Row(r.w, r.h, 0, gogui.HAlignCenter, sk.digits.Label(shown, size, c, false))
}

func targetLook(f *face) (gogui.View, skin) {
	amber, red, dim := kit.Hex(0xffb070), kit.Hex(0xff3a2a), kit.Hex(0x6a2a18)
	sk := f.wear(skin{
		back: kit.Hex(
			0x080202,
		),
		ink:       amber,
		muted:     dim,
		accent:    amber,
		accentInk: kit.Hex(0x080202),
		heat:      amber,
		cool:      amber,
		edge:      amber,
		edgeWide:  max(2, f.u()*0.004),
		text:      kit.Type{Upper: true},
		digits:    kit.Type{},
		glow:      true,
	})
	u := f.u()
	m := u * 0.05
	modes := f.modeBar(0.76, 0.13)
	d := min(f.vw*0.86, modes.y-m)
	ring := rect{f.vw/2 - d/2, max(m*0.5, (modes.y-d)/2), d, d}
	arcH := d * 0.16
	up := rect{ring.cx() - d*0.3, ring.y + d*0.06, d * 0.28, arcH}
	down := rect{ring.cx() + d*0.02, up.y, d * 0.28, arcH}
	num := rect{ring.cx() - d*0.32, ring.y + d*0.28, d * 0.64, d * 0.32}
	facts := rect{ring.cx() - d*0.3, num.y + num.h + d*0.03, d * 0.6, d * 0.09}
	text := f.number(f.setpoint())
	views := []gogui.View{
		f.canvas("target-ring", ring, kit.Version(d), func(dc *gogui.DrawContext) {
			c := d / 2
			dc.Circle(c, c, c*0.98, red.WithOpacity(0.5), max(1, d*0.004))
			dc.Arc(
				c,
				c,
				c*0.9,
				c*0.9,
				math.Pi*0.15,
				math.Pi*0.7,
				red.WithOpacity(0.6),
				max(1, d*0.003),
			)
			for i := range 60 {
				a := float32(i) / 60 * 2 * math.Pi
				ca, sa := float32(math.Cos(float64(a))), float32(math.Sin(float64(a)))
				dc.Line(
					c+ca*c*0.92,
					c+sa*c*0.92,
					c+ca*c*0.96,
					c+sa*c*0.96,
					red.WithOpacity(0.4),
					max(1, d*0.003),
				)
			}
			dc.Arc(
				c,
				c,
				c*0.8,
				c*0.8,
				math.Pi*1.18,
				math.Pi*0.3,
				amber.WithOpacity(0.9),
				max(2, d*0.006),
			)
			dc.Arc(
				c,
				c,
				c*0.8,
				c*0.8,
				math.Pi*1.52,
				math.Pi*0.3,
				amber.WithOpacity(0.9),
				max(2, d*0.006),
			)
			dc.Arc(
				c,
				c,
				c*0.62,
				c*0.62,
				math.Pi*1.18,
				math.Pi*0.64,
				amber.WithOpacity(0.6),
				max(1.5, d*0.004),
			)
			dc.Line(c-d*0.3, c+d*0.12, c+d*0.3, c+d*0.12, amber.WithOpacity(0.6), max(1, d*0.003))
		}),
		f.segmentText(
			"target-digits",
			rect{num.x, num.y, num.w * 0.74, num.h},
			text,
			amber,
			amber.WithOpacity(0.07),
		),
		f.line(
			sk,
			rect{num.x + num.w*0.74, num.y, num.w * 0.26, num.h * 0.42},
			"°"+f.letter(),
			amber,
			false,
		),
		f.centered(sk, facts, say.T("thermostat.humidity")+" "+f.humidity(), amber, false),
		up.place(f.arrow(sk, "up", up, true, "mdi:triangle-outline", btn{ink: amber})),
		down.place(f.arrow(sk, "down", down, false, "mdi:triangle-down-outline", btn{ink: amber})),
		modes.place(f.modes(modes, u*0.04, func(c choice, r rect) btn {
			on := ink(amber, c.current, amber.WithOpacity(0.85))
			if c.mode == "off" {
				on = red
			}
			b := btn{
				fill: ink(
					amber.WithOpacity(0.12),
					c.current,
					none,
				),
				edge:     on,
				edgeWide: sk.edgeWide,
				radius:   r.h * 0.12,
				content:  []gogui.View{sk.text.Fitted(f.w, c.label, r.w*0.7, r.h*0.42, on, false)},
			}
			if c.current {
				b.glow = amber.WithOpacity(0.4)
			}
			return b
		})),
	}
	return f.facade(sk, views...), sk
}
