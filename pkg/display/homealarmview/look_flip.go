package homealarmview

import (
	"slices"
	"strings"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/homeassistant/homealarm"
	"github.com/ygelfand/libcountertop/pkg/say"
)

const flipRows = 7

func clip(s string, most int) string {
	r := []rune(s)
	if len(r) > most {
		return string(r[:most])
	}
	return s
}

func flipLook(f *face) (gogui.View, skin) {
	sk := f.wear(skin{
		back: hex(0x161616), panel: hex(0x222222), key: hex(0x242424), ink: hex(0xf2efe8),
		muted: hex(0x8a8780), calm: hex(0xf2efe8), armed: hex(0xff6b5a), warn: hex(0xffd23f),
		alarm: hex(0xe03a2a), chosen: hex(0xffd23f), chosenInk: hex(0x161616),
		radius: f.unit() * 0.01, family: "", upper: false,
	})
	u := f.unit()
	m := u * 0.05
	tone := f.tone(sk)

	tiles := func(text string, cell float32, ink gogui.Color, count int) gogui.View {
		text = strings.ToUpper(text)
		runes := []rune(text)
		if count > 0 && len(runes) < count {
			runes = append(runes, []rune(strings.Repeat(" ", count-len(runes)))...)
		}
		out := make([]gogui.View, 0, len(runes))
		st := sk.text(cell*0.62, ink, true)
		for _, r := range runes {
			out = append(out, gogui.Column(gogui.ContainerCfg{
				Width:  cell * 0.78,
				Height: cell,
				Sizing: gogui.FixedFixed,
				Color:  sk.key,
				Radius: gogui.RadiusPx(
					cell * 0.08,
				),
				HAlign:  gogui.HAlignCenter,
				VAlign:  gogui.VAlignMiddle,
				Padding: gogui.NoPadding,
				Content: []gogui.View{
					gogui.Label(string(r), st),
					place(0, cell*0.49, cell*0.78, max(1, cell*0.02), gogui.Row(gogui.ContainerCfg{
						Sizing: gogui.FillFill, Color: sk.back, Padding: gogui.NoPadding,
					})),
				},
			}))
		}
		return gogui.Row(gogui.ContainerCfg{
			Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(cell * 0.08), Content: out,
		})
	}

	title := f.title()
	titleCells := float32(len([]rune(title)))
	headW := f.vw*0.6 - m
	cell := min(u*0.13, headW/max(titleCells, 1)/0.86)
	clockCell := u * 0.1
	views := []gogui.View{
		place(m, m, headW, cell, tiles(title, cell, tone, 0)),
		place(f.vw-m-f.cornerWidth()-clockCell*0.86*5, m+u*0.01, clockCell*0.86*5, clockCell,
			tiles(f.ticking(), clockCell, sk.ink, 0)),
	}
	if d := f.detail(); d != "" {
		views = append(
			views,
			place(m, m+cell+u*0.02, headW, u*0.06, sk.label(d, u*0.045, sk.warn, true)),
		)
	}

	listY := m + cell + u*0.11
	keyH := u * 0.13
	listH := f.vh - listY - u*0.06 - keyH - 2*m
	rowH := listH / flipRows
	rc := rowH * 0.78
	type flipRow struct {
		left, mid, right string
		ink              gogui.Color
	}
	var rows []flipRow
	heads := []string{say.T("homealarm.flip.time"), "", say.T("homealarm.flip.state")}
	if zones := f.zones(); len(zones) > 0 {
		heads = []string{say.T("homealarm.zones"), "", say.T("homealarm.flip.state")}
		slices.SortStableFunc(zones, func(a, b zoneLine) int {
			if a.alert != b.alert {
				if a.alert {
					return -1
				}
				return 1
			}
			return 0
		})
		for _, z := range zones {
			ink := sk.ink
			if z.alert {
				ink = sk.warn
			}
			rows = append(rows, flipRow{left: z.number, mid: z.name, right: z.state, ink: ink})
		}
	} else {
		log := f.v.history()
		for i := len(log) - 1; i >= 0; i-- {
			e := log[i]
			ink := sk.ink
			if e.status.Urgent() || e.status == homealarm.Arming {
				ink = sk.warn
			}
			rows = append(rows, flipRow{
				left: e.at.Format(
					"15:04",
				),
				right: say.T("homealarm.status." + string(e.status)),
				ink:   ink,
			})
		}
	}
	views = append(views, place(m, listY, f.vw-2*m, u*0.05, gogui.Row(gogui.ContainerCfg{
		Sizing: gogui.FillFill, Padding: gogui.NoPadding,
		Content: []gogui.View{
			sk.label(heads[0], u*0.03, sk.muted, true),
			gogui.Row(gogui.ContainerCfg{Sizing: gogui.FillFit, Padding: gogui.NoPadding}),
			sk.label(heads[2], u*0.03, sk.muted, true),
		},
	})))
	nameCells := max(int((f.vw*0.5)/(rc*0.86)), 4)
	stateCells := max(int((f.vw*0.3-m)/(rc*0.86)), 4)
	for i, r := range rows {
		if i >= flipRows {
			break
		}
		y := listY + u*0.06 + float32(i)*rowH
		views = append(views, place(m, y, f.vw*0.14, rc, tiles(r.left, rc, sk.muted, 0)))
		if r.mid != "" {
			views = append(
				views,
				place(m+f.vw*0.12, y, f.vw*0.5, rc, tiles(clip(r.mid, nameCells), rc, sk.ink, 0)),
			)
		}
		views = append(
			views,
			place(f.vw*0.7, y, f.vw*0.3-m, rc, tiles(clip(r.right, stateCells), rc, r.ink, 0)),
		)
	}

	acts := f.actions()
	n := len(acts)
	if f.emergency() {
		n++
	}
	gap := u * 0.025
	kw := (f.vw - 2*m - float32(max(n-1, 0))*gap) / float32(max(n, 1))
	keys := make([]gogui.View, 0, n)
	for i, a := range acts {
		fill, ink := sk.key, sk.ink
		if a.current || len(acts) > 1 && i == 1 {
			fill, ink = sk.chosen, sk.chosenInk
		}
		keys = append(keys, f.key(key{
			id:     a.id,
			w:      kw,
			h:      keyH,
			fill:   fill,
			ink:    ink,
			radius: sk.radius,
			align:  gogui.HAlignCenter,
			content: []gogui.View{
				sk.label(a.label, f.fit(sk, a.label, kw*0.85, keyH*0.34, true), ink, true),
			},
			run: a.run,
		}))
	}
	if f.emergency() {
		keys = append(keys, f.emergencyKey(sk, kw, keyH, sk.alarm, hex(0xffffff)))
	}
	views = append(views, place(m, f.vh-m-keyH, f.vw-2*m, keyH,
		row(f.vw-2*m, keyH, gap, gogui.HAlignLeft, keys...)))
	return gogui.Stack(gogui.ContainerCfg{
		Width:   f.vw,
		Height:  f.vh,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Content: views,
	}), sk
}
