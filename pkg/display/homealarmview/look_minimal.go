package homealarmview

import (
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/widgets"
)

func minimalLook(f *face) (gogui.View, skin) {
	pal := f.v.o.UI.Palette()
	sk := f.wear(skin{
		back:  widgets.Color(pal.Background),
		panel: widgets.Color(pal.Surface),
		key: widgets.Color(
			pal.Surface,
		),
		ink:   widgets.Color(pal.Text),
		muted: widgets.Color(pal.Muted),
		calm: widgets.Color(
			pal.Success,
		),
		armed:     widgets.Color(pal.Danger),
		warn:      widgets.Color(pal.Warning),
		alarm:     widgets.Color(pal.Danger),
		chosen:    widgets.Color(pal.Accent),
		chosenInk: widgets.Color(pal.Contrast(pal.Accent)),
		radius:    f.unit() * 0.03,
	})
	u := f.unit()
	tone := f.tone(sk)
	title := f.title()
	tsize := f.fit(sk, title, f.vw*0.8, u*0.16, true)
	mid := []gogui.View{
		icon(f.glyph(), u*0.16, tone),
		sk.label(title, tsize, sk.ink, true),
	}
	if d := f.detail(); d != "" {
		mid = append(mid, sk.label(d, u*0.04, sk.muted, false))
	}
	acts := f.actions()
	kh := u * 0.13
	keys := make([]gogui.View, 0, len(acts)+1)
	for _, a := range acts {
		w := max(f.w.TextWidth(a.label, sk.text(kh*0.34, sk.ink, true))+kh, kh*2.4)
		keys = append(keys, f.key(key{
			id:      a.id,
			w:       w,
			h:       kh,
			fill:    sk.key,
			ink:     sk.ink,
			radius:  kh / 2,
			align:   gogui.HAlignCenter,
			content: []gogui.View{sk.label(a.label, kh*0.34, sk.ink, true)},
			run:     a.run,
		}))
	}
	if f.emergency() {
		keys = append(
			keys,
			f.emergencyKey(skin{radius: kh / 2}, kh*3.2, kh, sk.alarm, hex(0xffffff)),
		)
	}
	mid = append(mid, gogui.Row(gogui.ContainerCfg{
		Padding: gogui.PadVH(u*0.04, 0), Spacing: gogui.SpacingPx(u * 0.03), Content: keys,
	}))
	return gogui.Column(gogui.ContainerCfg{
		Width: f.vw, Height: f.vh, Sizing: gogui.FixedFixed, Padding: gogui.NoPadding,
		HAlign: gogui.HAlignCenter, VAlign: gogui.VAlignMiddle, Spacing: gogui.SpacingPx(u * 0.02),
		Content: mid,
	}), sk
}
