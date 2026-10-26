package homealarmview

import (
	"strconv"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/say"
)

func consoleLook(f *face) (gogui.View, skin) {
	sk := f.wear(phosphor(f))
	u := f.unit()
	m := u * 0.05
	bar := u * 0.09
	tone := f.tone(sk)
	room := f.vw - 2*m - f.cornerWidth()
	status := []gogui.View{
		sk.label(f.title(), f.fit(sk, f.title(), room*0.4, u*0.04, true), sk.back, true),
	}
	for _, s := range []string{f.ready(), f.dev.Message, f.state.ChangedBy} {
		if s != "" {
			status = append(
				status,
				sk.label(f.short(sk, s, room*0.3, u*0.04, false), u*0.04, sk.back, false),
			)
		}
	}
	status = append(
		status,
		gogui.Row(gogui.ContainerCfg{Sizing: gogui.FillFit, Padding: gogui.NoPadding}),
		sk.label(f.now.Format("15:04"), u*0.04, sk.back, true),
		gogui.Row(
			gogui.ContainerCfg{
				Width:   f.cornerWidth(),
				Sizing:  gogui.FixedFit,
				Padding: gogui.NoPadding,
			},
		),
	)
	views := []gogui.View{place(0, 0, f.vw, bar, gogui.Row(gogui.ContainerCfg{
		Sizing:  gogui.FillFill,
		Color:   tone,
		Clip:    true,
		Padding: gogui.PadVH(0, m),
		Spacing: gogui.SpacingPx(u * 0.05),
		VAlign:  gogui.VAlignMiddle,
		Content: status,
	}))}

	log := []gogui.View{
		sk.label("-- "+say.T("homealarm.console.log")+" / "+f.now.Format("02 Jan 2006")+" --",
			u*0.042, sk.muted, false),
	}
	for i, e := range f.v.history() {
		text := e.at.Format("15:04:05") + "  " + say.T("homealarm.status."+string(e.status))
		if e.by != "" {
			text += " " + say.T("homealarm.console.by") + " " + e.by
		}
		c := sk.muted
		if i == len(f.v.history())-1 {
			c = sk.ink
		}
		log = append(log, sk.label(text, u*0.042, c, false))
	}
	if f.note != "" {
		log = append(log, sk.label(f.note, u*0.042, sk.warn, false))
	}
	keyH := u * 0.12
	logH := f.vh - bar - 3*m - keyH - u*0.1
	views = append(views, place(m, bar+m, f.vw-2*m, logH, gogui.Column(gogui.ContainerCfg{
		Sizing: gogui.FillFill, Padding: gogui.NoPadding, Spacing: gogui.SpacingPx(u * 0.02),
		Clip: true, Content: log,
	})))
	cursor := " █"
	if !f.blink(time.Second) {
		cursor = "  "
	}
	prompt := "> " + say.T("homealarm.console.prompt") + " █"
	size := f.fit(sk, prompt, f.vw-2*m, u*0.055, true)
	views = append(views, place(m, bar+m+logH, f.vw-2*m, u*0.1,
		sk.label(prompt[:len(prompt)-len(" █")]+cursor, size, sk.ink, true)))

	acts := append(f.actions(), f.toggles()...)
	n := len(acts)
	if f.emergency() {
		n++
	}
	gap := u * 0.025
	kw := (f.vw - 2*m - float32(max(n-1, 0))*gap) / float32(max(n, 1))
	keys := make([]gogui.View, 0, n)
	for i, a := range acts {
		label := "F" + strconv.Itoa(i+1) + " " + a.label
		fill, ink := sk.back, sk.ink
		if a.current {
			fill, ink = sk.chosen, sk.chosenInk
		}
		keys = append(keys, f.key(key{
			id:       a.id,
			w:        kw,
			h:        keyH,
			fill:     fill,
			ink:      ink,
			edge:     sk.edge,
			edgeWide: sk.edgeWide,
			align:    gogui.HAlignCenter,
			content: []gogui.View{
				sk.label(label, f.fit(sk, label, kw*0.85, keyH*0.36, false), ink, false),
			},
			run: a.run,
		}))
	}
	if f.emergency() {
		keys = append(keys, f.emergencyKey(sk, kw, keyH, sk.chosen, sk.chosenInk))
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
