package homealarmview

import (
	"math"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/homealarm"
	"github.com/ygelfand/libcountertop/pkg/say"
)

const (
	vaultTeeth = 16
	vaultGap   = 0.17 * math.Pi
)

var vaultLamps = []uint32{0xffc04d, 0x4da6ff, 0xb46bff, 0x5ce07a, 0xffd23f}

type wedge struct {
	id, label string
	lamp      gogui.Color
	run       func()
	hold      bool
	mid, span float64
}

func vaultLook(f *face) (gogui.View, skin) {
	sk := f.wear(skin{
		back: hex(0x050505), panel: hex(0x3a3a3c), key: hex(0x2c2e33), keyInk: hex(0xd8d8d8),
		ink: hex(0xe8e8e8), muted: hex(0x9a9a9a), calm: hex(0x5ce07a), armed: hex(0xff5040),
		warn: hex(0xffd23f), alarm: hex(0xff3020), chosen: hex(0x3d6b3a), chosenInk: hex(0xc8ffb0),
		edge: hex(0x15161a), edgeWide: max(1.5, f.unit()*0.003), radius: f.unit() * 0.008,
		family: style.MonoFamily, upper: true,
	})
	tone := f.tone(sk)
	pw, ph := f.vw, f.vh
	u := f.unit()
	m := u * 0.04
	ring := min(ph-2*m, pw*0.56)
	cx, cy := m+ring/2, ph/2
	outer, inner := ring*0.5, ring*0.34
	door := inner * 0.92

	wedges := vaultWedges(f, sk)
	toneKey := uint64(tone.R)<<16 | uint64(tone.G)<<8 | uint64(tone.B)
	canvas := gogui.DrawCanvas(gogui.DrawCanvasCfg{
		ID: "homealarm-vault", Width: pw, Height: ph, Sizing: gogui.FixedFixed,
		Version: uint64(pw)<<40 | uint64(ph)<<28 | toneKey<<4 | uint64(len(wedges)),
		OnDraw: func(dc *gogui.DrawContext) {
			dc.FilledRect(0, 0, pw, ph, hex(0x1b1b1d))
			e := u * 0.012
			dc.FilledRect(e, e, pw-2*e, ph-2*e, hex(0x56575a))
			for x := e; x < pw-e; x += max(2, u*0.003) {
				shade := hex(0x6a6b6e).WithOpacity(0.18 + 0.12*float32(math.Sin(float64(x)*0.37)))
				dc.Line(x, e, x, ph-e, shade, 1)
			}
			for _, b := range [][2]float32{{1, 1}, {-1, 1}, {1, -1}, {-1, -1}} {
				bx, by := pw/2+b[0]*(u*0.04-pw/2), ph/2+b[1]*(u*0.04-ph/2)
				dc.FilledCircle(bx, by, u*0.016, hex(0x2a2a2c))
				dc.FilledCircle(bx, by, u*0.011, hex(0x8c8d90))
			}

			dc.FilledCircle(cx, cy, outer+u*0.012, hex(0x141518))
			dc.FilledCircle(cx, cy, outer, hex(0x1d2333))
			for _, w := range wedges {
				start := float32(w.mid - w.span/2)
				dc.FilledArc(cx, cy, outer-u*0.008, outer-u*0.008, start, float32(w.span),
					w.lamp.WithOpacity(0.55))
				dc.FilledArc(cx, cy, outer-u*0.016, outer-u*0.016, start+0.01,
					float32(w.span)-0.02, hex(0x2a3044))
			}
			dc.Circle(cx, cy, outer, hex(0x8a7a3a), u*0.006)

			dc.FilledCircle(cx, cy, inner, hex(0x8a7a3a))
			dc.FilledCircle(cx, cy, inner-u*0.006, hex(0x22252c))
			for i := range vaultTeeth {
				a := float64(i) / vaultTeeth * 2 * math.Pi
				dc.FilledPolygon(tooth(cx, cy, door, a), hex(0x7b7c80))
			}
			dc.FilledCircle(cx, cy, door*0.86, hex(0x6c6d71))
			dc.Circle(cx, cy, door*0.86, hex(0x9a9b9f), u*0.005)
			dc.FilledCircle(cx, cy, door*0.64, hex(0x4a4b4f))
			for i := range 6 {
				a := float64(i) / 6 * 2 * math.Pi
				ca, sa := float32(math.Cos(a)), float32(math.Sin(a))
				dc.Line(cx+ca*door*0.3, cy+sa*door*0.3, cx+ca*door*0.62, cy+sa*door*0.62,
					hex(0x8c8d90), u*0.016)
			}
			dc.FilledCircle(cx, cy, door*0.32, hex(0x2b2c30))
			dc.Circle(cx, cy, door*0.32, tone, u*0.01)
		},
	})
	views := []gogui.View{place(0, 0, pw, ph, canvas)}
	views = append(
		views,
		place(cx-door*0.16, cy-door*0.16, door*0.32, door*0.32, gogui.Column(gogui.ContainerCfg{
			Sizing: gogui.FillFill, HAlign: gogui.HAlignCenter, VAlign: gogui.VAlignMiddle,
			Padding: gogui.NoPadding, Content: []gogui.View{icon(f.glyph(), door*0.2, tone)},
		})),
	)

	band := outer - inner
	for _, w := range wedges {
		rm := float64(inner + band*0.5)
		wx := cx + float32(math.Cos(w.mid)*rm)
		wy := cy + float32(math.Sin(w.mid)*rm)
		bw := min(float32(rm*w.span)*0.8, band*1.3)
		bh := band * 0.78
		k := key{
			id: "vault-" + w.id, w: bw, h: bh, align: gogui.HAlignCenter,
			content: []gogui.View{gogui.Column(gogui.ContainerCfg{
				Padding: gogui.NoPadding,
				HAlign:  gogui.HAlignCenter,
				Spacing: gogui.SpacingPx(bh * 0.06),
				Content: []gogui.View{
					gogui.Column(gogui.ContainerCfg{
						Width: bh * 0.13, Height: bh * 0.13, Sizing: gogui.FixedFixed,
						Radius: gogui.RadiusPx(bh * 0.065), Color: w.lamp, Padding: gogui.NoPadding,
					}),
					sk.label(w.label, f.fit(sk, w.label, bw*0.9, bh*0.17, true), w.lamp, true),
				},
			})},
			run: w.run,
		}
		var v gogui.View
		if w.hold {
			v = f.holdKey(k)
		} else {
			v = f.key(k)
		}
		views = append(views, place(wx-bw/2, wy-bh/2, bw, bh, v))
	}

	rx := cx + outer + m*1.5
	rw := pw - rx - m*1.5
	top := f.cornerWidth() * 0.5
	if f.still {
		top = m
	}
	lcdH := min(ph*0.2, rw*0.36)
	lines := []string{f.stateName(), f.title(), f.now.Format("Jan 2  15:04")}
	switch {
	case f.note != "":
		lines[2] = f.note
	case f.ready() != "":
		lines[2] = f.ready()
	case f.dev.Message != "":
		lines[1] = f.dev.Message
	}
	lcd := make([]gogui.View, 0, len(lines))
	for _, l := range lines {
		lcd = append(lcd, sk.label(l, f.fit(sk, l, rw*0.86, lcdH*0.2, false), hex(0x0f2a0f), false))
	}
	bezel := u * 0.014
	views = append(views,
		place(rx-bezel, top-bezel, rw+2*bezel, lcdH+2*bezel, gogui.Column(gogui.ContainerCfg{
			Sizing: gogui.FillFill, Color: hex(0x0d0d0e), Radius: gogui.RadiusPx(bezel),
			Padding: gogui.NoPadding,
		})),
		place(rx, top, rw, lcdH, gogui.Column(gogui.ContainerCfg{
			Sizing: gogui.FillFill, Color: hex(0x86c96c), Radius: gogui.RadiusPx(bezel * 0.5),
			HAlign: gogui.HAlignCenter, VAlign: gogui.VAlignMiddle, Padding: gogui.NoPadding,
			Spacing: gogui.SpacingPx(lcdH * 0.04), Content: lcd,
		})),
	)

	padY := top + lcdH + m*1.5
	padH := ph - padY - m*1.5
	padW := min(rw, padH*0.95)
	padX := rx + (rw-padW)/2
	views = append(views,
		place(padX-bezel, padY-bezel, padW+2*bezel, padH+2*bezel, gogui.Column(gogui.ContainerCfg{
			Sizing: gogui.FillFill, Color: hex(0x101113), Radius: gogui.RadiusPx(bezel),
			ColorBorder: hex(0x2a5a2a), SizeBorder: gogui.BorderPx(max(1, u*0.003)),
			Padding: gogui.NoPadding,
		})),
		place(padX, padY, padW, padH, f.padGrid(sk, padW, padH)),
	)
	return gogui.Stack(gogui.ContainerCfg{
		Width:   f.vw,
		Height:  f.vh,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Content: views,
	}), sk
}

func vaultWedges(f *face, sk skin) []wedge {
	var out []wedge
	for i, a := range f.actions() {
		lamp := hex(vaultLamps[i%len(vaultLamps)])
		if a.status == homealarm.Disarmed {
			lamp = sk.calm
		}
		out = append(out, wedge{id: a.id, label: a.label, lamp: lamp, run: a.run})
	}
	for i, t := range f.toggles() {
		lamp := hex(0x6b6b6b)
		if t.current {
			lamp = hex(0x4dd0ff)
		}
		out = append(out, wedge{id: t.id, label: f.dev.Toggles[i].Name, lamp: lamp, run: t.run})
	}
	names := shortNames(f.dev.Buttons)
	for i, b := range f.dev.Buttons {
		out = append(out, wedge{id: "button-" + b.Entity, label: names[i], lamp: sk.alarm,
			run: f.v.press(b.Entity), hold: true})
	}
	if len(f.dev.Buttons) == 0 && f.emergency() {
		out = append(out, wedge{id: "emergency", label: say.T("homealarm.panic"), lamp: sk.alarm,
			run: f.v.trigger, hold: true})
	}
	left := (len(out) + 1) / 2
	place := func(ws []wedge, from, to float64) {
		if len(ws) == 0 {
			return
		}
		each := (to - from) / float64(len(ws))
		for i := range ws {
			ws[i].mid = from + each*(float64(i)+0.5)
			ws[i].span = math.Abs(each) * 0.92
		}
	}
	place(out[:left], math.Pi*1.5-vaultGap, math.Pi*0.5+vaultGap)
	place(out[left:], -math.Pi*0.5+vaultGap, math.Pi*0.5-vaultGap)
	return out
}

func tooth(cx, cy, r float32, a float64) []float32 {
	half := 0.09
	r0, r1 := float64(r)*0.82, float64(r)*1.02
	pts := []float64{a - half, a + half}
	out := make([]float32, 0, 8)
	for _, p := range []struct {
		rad   float64
		angle float64
	}{{r0, pts[0]}, {r1, pts[0] + 0.02}, {r1, pts[1] - 0.02}, {r0, pts[1]}} {
		out = append(out, cx+float32(math.Cos(p.angle)*p.rad), cy+float32(math.Sin(p.angle)*p.rad))
	}
	return out
}
