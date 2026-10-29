package thermostatview

import (
	"math"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	kit "github.com/ygelfand/libcountertop/pkg/display/panelkit"
	"github.com/ygelfand/libcountertop/pkg/say"
)

func octagon(cx, cy, r float32) []float32 {
	pts := make([]float32, 0, 16)
	for i := range 8 {
		a := float64(i)*math.Pi/4 + math.Pi/8
		pts = append(pts, cx+r*float32(math.Cos(a)), cy+r*float32(math.Sin(a)))
	}
	return pts
}

func shrineLook(f *face) (gogui.View, skin) {
	cyan, deep, gold := kit.Hex(0x58d8ff), kit.Hex(0x06121e), kit.Hex(0xb8a27a)
	orange := kit.Hex(0xff9a3c)
	sk := f.wear(skin{
		back: deep, ink: cyan, muted: kit.Hex(0x2c6b88), accent: cyan, accentInk: deep,
		heat: orange, cool: cyan, edge: cyan, edgeWide: max(2, f.u()*0.004),
		text: kit.Type{Upper: true}, digits: kit.Type{}, glow: true,
	})
	u := f.u()
	m := u * 0.05
	modes := f.modeBar(0.76, 0.15)
	body := rect{m, m + f.reserve*0.3, f.vw - 2*m, modes.y - m*1.6 - f.reserve*0.3}
	d := min(body.h*0.6, body.w*0.46)
	top := body.y + (body.h-d*1.55)/2
	circle := rect{f.vw/2 - d/2, max(top, body.y), d, d}
	hex := min(d*0.5, (body.w-d)/2*0.75)
	up := rect{circle.x - hex*1.35, circle.cy() - hex/2, hex, hex}
	down := rect{circle.x + d + hex*0.35, up.y, hex, hex}
	meter := rect{
		f.vw/2 - d*0.75,
		circle.y + d*0.9,
		d * 1.5,
		min(d*0.6, body.y+body.h-circle.y-d*0.9),
	}
	lit := 0
	if f.st.Humidity != nil {
		lit = int(math.Round(*f.st.Humidity / 100 * 16))
	}
	pulse := f.breath(4 * time.Second)
	views := []gogui.View{
		f.canvas(
			"shrine-frame",
			rect{0, 0, f.vw, f.vh},
			kit.Version(f.vw, f.vh, d, pulse, float32(lit)),
			func(dc *gogui.DrawContext) {
				c := d / 2
				ccx, ccy := circle.cx(), circle.cy()
				dc.Circle(ccx, ccy, c, cyan.WithOpacity(0.7+0.3*pulse), max(2, d*0.012))
				dc.Circle(ccx, ccy, c*1.08, cyan.WithOpacity(0.25), max(1, d*0.004))
				for _, a := range []rect{up, down} {
					ring := octagon(a.cx(), a.cy(), a.w*0.62)
					dc.PolylineJoined(append(ring, ring[:2]...), gold, max(2, a.w*0.035))
				}
				dc.Line(
					up.x+up.w*1.07,
					up.cy(),
					ccx-c*1.08,
					ccy,
					cyan.WithOpacity(0.6),
					max(1, d*0.006),
				)
				dc.Line(
					down.x-down.w*0.07,
					down.cy(),
					ccx+c*1.08,
					ccy,
					cyan.WithOpacity(0.6),
					max(1, d*0.006),
				)
				mc, mr := meter.cx(), meter.w*0.42
				my := meter.y + mr*0.25
				for i := range 16 {
					a0 := math.Pi*1.0 + float64(i)/16*math.Pi
					a1 := a0 + math.Pi/16*0.78
					col := cyan.WithOpacity(0.18)
					if i < lit {
						col = cyan.WithOpacity(0.55 + 0.45*float32(i)/16)
					}
					p := []float32{}
					for _, rr := range []float32{mr, mr * 0.78} {
						for _, a := range []float64{a0, a1} {
							p = append(
								p,
								mc+rr*float32(math.Cos(a)),
								my+mr+rr*float32(math.Sin(a))-mr*0.7,
							)
						}
					}
					dc.FilledPolygon([]float32{p[0], p[1], p[2], p[3], p[6], p[7], p[4], p[5]}, col)
				}
			},
		),
		circle.place(f.reading(sk, circle.inset(d*0.14), digits{ink: cyan, letter: true})),
		up.place(f.arrow(sk, "up", up, true, "mdi:triangle", btn{ink: cyan})),
		down.place(f.arrow(sk, "down", down, false, "mdi:triangle-down", btn{ink: cyan})),
		f.centered(
			sk,
			rect{meter.x, meter.y + meter.h*0.35, meter.w, meter.h * 0.4},
			f.humidity(),
			cyan,
			true,
		),
		modes.place(f.modes(modes, u*0.04, func(c choice, r rect) btn {
			tone := ink(cyan, c.current, cyan.WithOpacity(0.4))
			if c.current && c.mode == "heat" {
				tone = orange
			}
			b := btn{
				edge: ink(
					gold,
					c.current,
					gold.WithOpacity(0.5),
				),
				edgeWide: sk.edgeWide,
				radius:   r.h * 0.2,
				content:  f.modeContent(sk, c, r, tone, true),
			}
			if c.current {
				b.glow = tone.WithOpacity(0.35)
			}
			return b
		})),
	}
	return f.facade(sk, views...), sk
}

func topLook(f *face) (gogui.View, skin) {
	teal, slate, ink0 := kit.Hex(0x7fe3f0), kit.Hex(0x14202a), kit.Hex(0xcfe9ee)
	sk := f.wear(skin{
		back: slate, panel: kit.Hex(0x1b2a35), ink: teal, muted: kit.Hex(0x5d7b86), accent: teal,
		accentInk: slate, heat: kit.Hex(0xffb38a), cool: teal, edge: kit.Hex(0x2f4652),
		edgeWide: max(1.5, f.u()*0.003), text: kit.Type{Upper: true}, digits: kit.Type{Thin: true},
	})
	u := f.u()
	m := u * 0.05
	modes := f.modeBar(0.56, 0.11)
	numH := (modes.y - m) * 0.32
	num := rect{f.vw * 0.25, m + f.reserve*0.25, f.vw * 0.5, numH}
	a := min(numH*0.6, u*0.11)
	up := rect{num.x - a*1.3, num.cy() - a/2, a, a}
	down := rect{num.x + num.w + a*0.3, num.cy() - a/2, a, a}
	discY := num.y + num.h + m*0.3
	d := min(modes.y-discY-m*0.4, f.vw*0.6)
	disc := rect{f.vw/2 - d/2, discY, d, d}
	spin := f.phase(1200*time.Millisecond) * 2 * math.Pi
	wobble := float32(math.Sin(f.phase(5*time.Second)*2*math.Pi)) * 0.06
	if f.st.Mode == "off" || f.still {
		spin, wobble = 0, 0
	}
	views := []gogui.View{
		f.canvas(
			"top-disc",
			disc,
			kit.Version(d, float32(spin), wobble),
			func(dc *gogui.DrawContext) {
				c := d / 2
				dc.FilledCircle(c, c, c, kit.Hex(0x1a2833))
				dc.Circle(c, c, c*0.98, teal.WithOpacity(0.35), max(1.5, d*0.006))
				dc.FilledCircle(c, c, c*0.56, kit.Hex(0x213440))
				dc.Circle(c, c, c*0.56, kit.Hex(0x2f4652), max(1, d*0.004))
				dc.FilledCircle(c, c*0.82, c*0.26, kit.Hex(0x162630))
				dc.Save()
				dc.Translate(c, c*0.82)
				dc.Rotate(wobble)
				tw := c * 0.24
				sq := float32(math.Abs(math.Cos(spin)))*0.25 + 0.75
				dc.FilledArc(0, 0, tw*sq*1.4, tw*0.32, 0, 2*math.Pi, teal.WithOpacity(0.25))
				dc.FilledPolygon(
					[]float32{-tw * sq, 0, tw * sq, 0, 0, tw * 0.9},
					teal.WithOpacity(0.9),
				)
				dc.FilledPolygon(
					[]float32{-tw * 0.55 * sq, 0, tw * 0.55 * sq, 0, 0, -tw * 0.5},
					ink0,
				)
				dc.Line(0, -tw*0.5, 0, -tw*0.85, ink0, max(2, tw*0.08))
				dc.Restore()
			},
		),
		num.place(f.reading(sk, num, digits{ink: teal, letter: true, degree: 0.36})),
		up.place(f.arrow(sk, "up", up, true, "mdi:chevron-up", btn{
			edge: sk.edge, edgeWide: sk.edgeWide, radius: a / 2, ink: teal,
		})),
		down.place(f.arrow(sk, "down", down, false, "mdi:chevron-down", btn{
			edge: sk.edge, edgeWide: sk.edgeWide, radius: a / 2, ink: teal,
		})),
		f.centered(sk, rect{disc.x + d*0.2, disc.y + d*0.66, d * 0.6, d * 0.08},
			say.T("thermostat.humidity")+" "+f.humidity(), ink0, false),
		modes.place(f.modes(modes, 0, func(c choice, r rect) btn {
			return btn{
				fill: ink(
					kit.Hex(0x223743),
					c.current,
					kit.Hex(0x16232c),
				),
				edge:     sk.edge,
				edgeWide: sk.edgeWide,
				content: []gogui.View{sk.text.Fitted(f.w, c.label, r.w*0.7, r.h*0.32,
					ink(teal, c.current, sk.muted), false)},
			}
		})),
	}
	return f.facade(sk, views...), sk
}

func labLook(f *face) (gogui.View, skin) {
	plate, steel, yellow := kit.Hex(0x23272a), kit.Hex(0x8c9196), kit.Hex(0xf2c230)
	orange, cream := kit.Hex(0xff5a1f), kit.Hex(0xeee6c6)
	sk := f.wear(skin{
		back: kit.Hex(0x15181a), panel: plate, ink: cream, muted: steel, accent: orange,
		accentInk: cream, heat: orange, cool: kit.Hex(0x8fd18f), edge: kit.Hex(0x3a3f43),
		edgeWide: max(2, f.u()*0.005), text: kit.Type{Upper: true}, digits: kit.Type{Thin: true},
	})
	u := f.u()
	stripe := min(f.vw*0.06, u*0.08)
	if !f.wide() {
		stripe = 0
	}
	in := rect{stripe + u*0.04, u * 0.04, f.vw - 2*stripe - u*0.08, f.vh - u*0.08}
	topH := in.h * 0.5
	gaugeW := min(in.w*0.36, topH)
	gauge := rect{in.x, in.y, gaugeW, topH}
	tubes := rect{gauge.x + gauge.w + u*0.03, in.y, in.w - gauge.w - u*0.03, topH * 0.72}
	switches := rect{tubes.x, tubes.y + tubes.h + u*0.01, tubes.w, topH - tubes.h - u*0.01}
	modes := rect{in.x + in.w*0.08, in.y + topH + u*0.04, in.w * 0.92, in.h - topH - u*0.04}
	text := f.number(f.setpoint()) + "°" + f.letter()
	flick := float32(1)
	if !f.still && f.working() {
		flick = 0.92 + 0.08*f.breath(170*time.Millisecond)
	}
	views := []gogui.View{
		f.canvas(
			"lab-plate",
			rect{0, 0, f.vw, f.vh},
			kit.Version(f.vw, f.vh, stripe),
			func(dc *gogui.DrawContext) {
				if stripe > 0 {
					hazard(dc, u*0.01, 0, stripe, f.vh, kit.Hex(0x111111), yellow)
					hazard(dc, f.vw-stripe-u*0.01, 0, stripe, f.vh, kit.Hex(0x111111), yellow)
				}
				dc.FilledRoundedRect(
					in.x-u*0.015,
					in.y-u*0.015,
					in.w+u*0.03,
					in.h+u*0.03,
					u*0.02,
					plate,
				)
				for _, p := range [][2]float32{{in.x, in.y}, {in.x + in.w, in.y}, {in.x, in.y + in.h}, {in.x + in.w, in.y + in.h}} {
					screw(dc, p[0], p[1], u*0.012, steel, kit.Hex(0x2a2a2a))
				}
				dc.FilledRoundedRect(tubes.x, tubes.y, tubes.w, tubes.h, u*0.015, kit.Hex(0x0d0d0d))
			},
		),
		f.dial("lab-gauge", gauge.inset(u*0.01), dial{
			from:   0,
			to:     120,
			start:  math.Pi * 1.15,
			sweep:  math.Pi * 0.7,
			ticks:  24,
			labels: 2,
			value:  f.st.Humidity,
			face:   kit.Hex(0xefe3a6),
			rim:    kit.Hex(0x0a0a0a),
			bezel:  0.08,
			tick:   kit.Hex(0x222222),
			needle: kit.Hex(0x111111),
			hub:    kit.Hex(0x555555),
			text: kit.Type{
				Upper: true,
			},
			caption: say.T("thermostat.humidity"),
			reading: f.humidity(),
		}),
		f.canvas("lab-tubes", tubes, kit.Version(tubes.w, tubes.h, float32(hashText(text)), flick),
			func(dc *gogui.DrawContext) {
				runes := []string{}
				for _, r := range text {
					runes = append(runes, string(r))
				}
				n := float32(len(runes))
				tw := min(tubes.w/(n*1.12), tubes.h*0.62)
				th := tw * 1.55
				x := (tubes.w - n*tw*1.12) / 2
				y := (tubes.h - th) / 2
				st := sk.digits.Style(th*0.62, orange.WithOpacity(flick), false)
				for _, r := range runes {
					nixie(dc, x, y, tw, th, r, st, orange, kit.Hex(0x2a1a14).WithOpacity(0.9))
					x += tw * 1.12
				}
			}),
	}
	sw := min(switches.h, switches.w*0.22)
	for i, s := range []struct {
		id string
		up bool
	}{{"down", false}, {"up", true}} {
		r := rect{
			switches.x + switches.w*(0.2+0.45*float32(i)),
			switches.y + (switches.h-sw)/2,
			sw,
			sw,
		}
		label := "−"
		if s.up {
			label = "+"
		}
		views = append(
			views,
			r.place(f.arrow(sk, s.id, r, s.up, "mdi:toggle-switch-outline", btn{
				fill: kit.Hex(
					0x3b4044,
				),
				ink:      cream,
				edge:     kit.Hex(0x111111),
				edgeWide: sk.edgeWide,
				radius:   r.w * 0.12,
			})),
			f.right(
				sk,
				rect{r.x - sw*1.05, r.y, sw, sw},
				say.T("thermostat.temp")+" "+label,
				cream,
				true,
			),
		)
	}
	views = append(views, modes.place(f.modes(modes, u*0.04, func(c choice, r rect) btn {
		d := min(r.w, r.h) * 0.9
		core := ink(orange, c.current, kit.Hex(0xd8d4c8))
		if c.current && c.mode == "cool" {
			core = sk.cool
		}
		return btn{content: []gogui.View{gogui.Column(gogui.ContainerCfg{
			Width: d, Height: d, Sizing: gogui.FixedFixed, Color: kit.Hex(0x9aa0a6),
			ColorBorder: kit.Hex(0x2c3034), SizeBorder: gogui.BorderPx(max(2, d*0.06)),
			Radius: gogui.RadiusPx(d / 2), HAlign: gogui.HAlignCenter, VAlign: gogui.VAlignMiddle,
			Content: []gogui.View{gogui.Column(gogui.ContainerCfg{
				Width:  d * 0.72,
				Height: d * 0.72,
				Sizing: gogui.FixedFixed,
				Color:  core,
				Radius: gogui.RadiusPx(d * 0.36),
				HAlign: gogui.HAlignCenter,
				VAlign: gogui.VAlignMiddle,
				Shadow: shadowIf(c.current, core),
				Content: []gogui.View{sk.text.Fitted(f.w, c.label, d*0.55, d*0.2,
					ink(cream, c.current, kit.Hex(0x333333)), true)},
			})},
		})}}
	})))
	return f.facade(sk, views...), sk
}

func shadowIf(on bool, c gogui.Color) *gogui.BoxShadow {
	if !on {
		return nil
	}
	return &gogui.BoxShadow{Color: c.WithOpacity(0.6), BlurRadius: 18}
}

func brassLook(f *face) (gogui.View, skin) {
	wood, brass, dark := kit.Hex(0x1e1712), kit.Hex(0xc9a24f), kit.Hex(0x3a2a1c)
	neon, paper := kit.Hex(0xff5b2e), kit.Hex(0xefe2c2)
	sk := f.wear(skin{
		back:      wood,
		panel:     dark,
		ink:       paper,
		muted:     kit.Hex(0x9c8360),
		accent:    brass,
		accentInk: wood,
		heat:      neon,
		cool:      kit.Hex(0x58b7ff),
		edge:      brass,
		edgeWide:  max(2, f.u()*0.006),
		text:      kit.Type{Upper: true},
		digits:    kit.Type{},
	})
	u := f.u()
	m := u * 0.05
	modes := rect{
		f.vw * 0.36,
		f.vh - m - min(u*0.18, f.vh*0.22),
		f.vw*0.64 - m,
		min(u*0.18, f.vh*0.22),
	}
	topH := modes.y - m*1.6
	big := min(topH, f.vw*0.36)
	gauge := rect{m, m, big, big}
	sign := rect{
		gauge.x + gauge.w + m*0.6,
		m + topH*0.18,
		f.vw - gauge.w - 2.6*m - big*0.42,
		topH * 0.5,
	}
	small := rect{f.vw - m - big*0.42, m + max(topH*0.2, f.reserve*0.3), big * 0.42, big * 0.42}
	if !f.wide() {
		gauge = rect{f.vw/2 - big*0.35, m, big * 0.7, big * 0.7}
		sign = rect{
			m,
			gauge.y + gauge.h + m*0.4,
			f.vw - 2*m,
			(modes.y - gauge.y - gauge.h - m) * 0.6,
		}
		small = rect{}
		modes = rect{m, modes.y, f.vw - 2*m, modes.h}
	}
	lo, hi := f.st.Min, f.st.Max
	if hi <= lo {
		lo, hi = 60, 80
	}
	a := min(u*0.1, sign.h*0.4)
	up := rect{sign.x + sign.w - a, sign.y + sign.h + m*0.3, a, a}
	down := rect{up.x - a*1.25, up.y, a, a}
	text := f.number(f.setpoint()) + "°" + f.letter()
	needle, glow := f.setpoint(), neon
	if needle != nil && f.live() {
		wob := *needle + (hi-lo)*0.006*math.Sin(f.phase(1700*time.Millisecond)*2*math.Pi)
		needle = &wob
		if uint32(f.now.UnixMilli()/80)*2654435761>>27 == 0 {
			glow = neon.WithOpacity(0.55)
		}
	}
	views := []gogui.View{
		f.canvas(
			"brass-wood",
			rect{0, 0, f.vw, f.vh},
			kit.Version(f.vw, f.vh),
			func(dc *gogui.DrawContext) {
				for i := 0; i < 5; i++ {
					y := f.vh * float32(i) / 5
					dc.Line(0, y, f.vw, y, kit.Hex(0x120d0a), max(1, u*0.004))
				}
				dc.FilledRoundedRect(sign.x, sign.y, sign.w, sign.h, u*0.015, kit.Hex(0x2b2016))
				dc.RoundedRect(sign.x, sign.y, sign.w, sign.h, u*0.015, brass, max(2, u*0.008))
			},
		),
		f.dial("brass-setting", gauge, dial{
			from: lo, to: hi, start: math.Pi * 0.75, sweep: math.Pi * 1.5, ticks: 20, labels: 4,
			value: needle, face: kit.Hex(0xe9dcb8), rim: brass, bezel: 0.1,
			tick: kit.Hex(0x3a2a1c), needle: kit.Hex(0x7a5520), hub: brass,
			text: kit.Type{Upper: true}, caption: say.T("thermostat.dial"), reading: text,
		}),
		sign.place(kit.Row(sign.w, sign.h, 0, gogui.HAlignCenter,
			sk.digits.Fitted(f.w, text, sign.w*0.86, sign.h*0.8, glow, false))),
		up.place(f.arrow(sk, "up", up, true, "mdi:chevron-up", btn{
			fill: dark, ink: brass, edge: brass, edgeWide: sk.edgeWide, radius: a / 2,
		})),
		down.place(f.arrow(sk, "down", down, false, "mdi:chevron-down", btn{
			fill: dark, ink: brass, edge: brass, edgeWide: sk.edgeWide, radius: a / 2,
		})),
		f.line(sk, rect{sign.x, up.y, sign.w * 0.5, a}, f.status(), sk.muted, false),
		modes.place(f.modes(modes, u*0.03, func(c choice, r rect) btn {
			lamp := kit.Icon("mdi:circle", r.h*0.12, ink(f.tone(sk), c.current, kit.Hex(0x2a2a2a)))
			label := ink(f.tone(sk), c.current, paper)
			return btn{
				fill: ink(
					kit.Hex(0x4a3824),
					c.current,
					kit.Hex(0x2a1f15),
				),
				edge:     brass,
				edgeWide: sk.edgeWide,
				radius:   r.h * 0.1,
				content: []gogui.View{gogui.Column(gogui.ContainerCfg{
					HAlign:  gogui.HAlignCenter,
					Spacing: gogui.SpacingPx(r.h * 0.06),
					Content: []gogui.View{
						lamp,
						sk.text.Fitted(f.w, c.label, r.w*0.7, r.h*0.24, label, true),
					},
				})},
			}
		})),
	}
	if small.w > 0 {
		views = append(views, f.dial("brass-humidity", small, dial{
			from: 0, to: 100, start: math.Pi * 0.75, sweep: math.Pi * 1.5, ticks: 10, labels: 2,
			value: f.st.Humidity, face: kit.Hex(0xdfe8ee), rim: brass, bezel: 0.12,
			tick: kit.Hex(0x223040), needle: kit.Hex(0x223040), text: kit.Type{Upper: true},
			caption: say.T("thermostat.humidity"), reading: f.humidity(),
		}))
	}
	return f.facade(sk, views...), sk
}
