package homealarmview

import (
	"math"
	"time"

	"github.com/go-gui-org/go-glyph"
	gogui "github.com/go-gui-org/go-gui/gui"
)

const ringDashes = 14

func ringLook(f *face) (gogui.View, skin) {
	sk := f.wear(skin{
		back: hex(0x121a2e), panel: hex(0x1a2440), key: hex(0x121a2e), ink: hex(0xf1f4fb),
		muted: hex(0x9aa6c3), calm: hex(0x8fb8ff), armed: hex(0xff8a7a), warn: hex(0xffb340),
		alarm: hex(0xff5a5a), chosen: hex(0x8fb8ff), chosenInk: hex(0x121a2e),
		edge: hex(0x3a4a70), edgeWide: max(2, f.unit()*0.004), radius: f.unit() * 0.1,
	})
	u := f.unit()
	m := u * 0.06
	tone := f.tone(sk)

	thin := sk.text(u*0.24, sk.ink, false)
	thin.Typeface = glyph.TypefaceRegular
	clockW := f.vw * 0.28
	clockText := f.clock()
	thin.Size = f.fit(sk, clockText, clockW*0.78, u*0.2, false)
	views := []gogui.View{
		place(m, f.vh*0.28, clockW, f.vh*0.44, gogui.Column(gogui.ContainerCfg{
			Sizing: gogui.FillFill, Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(u * 0.02),
			VAlign: gogui.VAlignMiddle,
			Content: []gogui.View{
				gogui.Label(clockText, thin),
				sk.label(f.date(), u*0.045, sk.muted, false),
			},
		})),
	}

	d := min(f.vh*0.66, f.vw*0.34)
	cx := m + clockW + (f.vw*0.62-clockW-m-d)/2 + d/2
	cy := f.vh / 2
	lead := tone
	turn := f.phase(30*time.Second) * 2 * math.Pi
	views = append(views, place(cx-d/2, cy-d/2, d, d, gogui.DrawCanvas(gogui.DrawCanvasCfg{
		ID: "homealarm-ring", Width: d, Height: d, Sizing: gogui.FixedFixed,
		Version: uint64(lead.R)<<16 | uint64(lead.G)<<8 | uint64(lead.B) | uint64(d)<<24 |
			uint64(turn*1000)<<40,
		OnDraw: func(dc *gogui.DrawContext) {
			r := d/2 - d*0.03
			step := 2 * math.Pi / ringDashes
			for i := range ringDashes {
				start := float32(float64(i)*step - math.Pi/2 + step*0.08 + turn)
				dc.Arc(d/2, d/2, r, r, start, float32(step*0.84), lead, d*0.03)
			}
		},
	})))
	inner := d * 0.62
	title := f.title()
	mid := []gogui.View{
		icon(f.glyph(), d*0.16, tone),
		sk.label(title, f.fit(sk, title, inner, d*0.12, true), sk.ink, true),
	}
	if det := f.detail(); det != "" {
		mid = append(mid, sk.label(det, f.fit(sk, det, inner, d*0.055, false), sk.warn, false))
	}
	views = append(
		views,
		place(cx-inner/2, cy-inner/2, inner, inner, gogui.Column(gogui.ContainerCfg{
			Sizing: gogui.FillFill, Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(d * 0.03),
			HAlign: gogui.HAlignCenter, VAlign: gogui.VAlignMiddle, Content: mid,
		})),
	)

	pillW := f.vw * 0.32
	px := f.vw - m - pillW
	acts := f.actions()
	emH := u * 0.13
	avail := f.vh - 2*m - f.cornerWidth()*0.5
	if f.emergency() {
		avail -= emH + u*0.04
	}
	gap := u * 0.035
	n := max(len(acts), 1)
	ph := min((avail-float32(n-1)*gap)/float32(n), u*0.17)
	pills := make([]gogui.View, 0, len(acts))
	for i, a := range acts {
		fill, ink := sk.key, sk.ink
		if a.current || i == len(acts)/2 && len(acts) == 3 {
			fill, ink = sk.chosen, sk.chosenInk
		}
		pills = append(pills, f.key(key{
			id: a.id, w: pillW, h: ph, fill: fill, ink: ink, edge: sk.edge, edgeWide: sk.edgeWide,
			radius: ph / 2, align: gogui.HAlignLeft,
			content: []gogui.View{
				icon(a.glyph, ph*0.34, ink),
				sk.label(a.label, f.fit(sk, a.label, pillW*0.6, ph*0.3, true), ink, true),
			},
			run: a.run,
		}))
	}
	views = append(
		views,
		place(px, m+f.cornerWidth()*0.5, pillW, avail, gogui.Column(gogui.ContainerCfg{
			Sizing: gogui.FillFill, Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(gap),
			VAlign: gogui.VAlignMiddle, Content: pills,
		})),
	)
	if f.emergency() {
		views = append(views, place(f.vw-m-emH, f.vh-m-emH, emH, emH, f.holdKey(key{
			id: "emergency", w: emH, h: emH, fill: hex(0xff5a5a), radius: emH / 2,
			content: []gogui.View{icon("mdi:alarm-light-outline", emH*0.45, hex(0xffffff))},
			run:     f.v.trigger,
		})))
	}
	return gogui.Stack(gogui.ContainerCfg{
		Width:   f.vw,
		Height:  f.vh,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Content: views,
	}), sk
}
