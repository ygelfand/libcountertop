package homealarmview

import (
	"strings"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type look func(f *face) (gogui.View, skin)

var looks = map[schema.HomeAlarmLook]look{
	schema.HomeAlarmKeypad:    keypadLook,
	schema.HomeAlarmShield:    shieldLook,
	schema.HomeAlarmCard:      cardLook,
	schema.HomeAlarmRing:      ringLook,
	schema.HomeAlarmDial:      dialLook,
	schema.HomeAlarmNeon:      neonLook,
	schema.HomeAlarmHardware:  hardwareLook,
	schema.HomeAlarmFlip:      flipLook,
	schema.HomeAlarmTerminal:  terminalLook,
	schema.HomeAlarmConsole:   consoleLook,
	schema.HomeAlarmBlueprint: blueprintLook,
	schema.HomeAlarmMatrix:    matrixLook,
	schema.HomeAlarmVault:     vaultLook,
	schema.HomeAlarmMinimal:   minimalLook,
}

var inline = map[schema.HomeAlarmLook]bool{
	schema.HomeAlarmKeypad: true,
	schema.HomeAlarmVault:  true,
}

func (v *View) build(w *gogui.Window) gogui.View {
	popup := v.o.Shell.Visible(v.popup) && v.o.Shell.Top() == shell.View(v.popup)
	f := v.face(w, popup)
	draw, ok := looks[v.look()]
	if !ok {
		draw = keypadLook
	}
	vw, vh := w.WindowSize()
	if v.look() == schema.HomeAlarmMatrix {
		f.backdrop = v.backdrop(f.current(), vw, vh)
	} else {
		v.dropBackdrops()
	}
	body, sk := draw(f)
	if f.backdrop {
		sk.back = gogui.RGBA(0, 0, 0, 0)
	}
	content := []gogui.View{body}
	content = append(content, f.corners()...)
	if f.asking {
		content = append(content, f.sheet(sk))
	}
	return gogui.Column(gogui.ContainerCfg{
		ID:      "homealarm",
		Width:   f.vw,
		Height:  f.vh,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Color:   sk.back,
		Clip:    true,
		Content: content,
	})
}

func (f *face) dots(sk skin) string {
	typed := len(f.v.pad.Typed())
	if typed == 0 {
		return sk.words(say.T("homealarm.code.enter"))
	}
	return strings.Repeat("● ", typed)
}

func (f *face) padGrid(sk skin, w, h float32) gogui.View {
	gap := min(w, h) * 0.04
	kw, kh := (w-2*gap)/3, (h-3*gap)/4
	size := min(kh*0.42, kw*0.42)
	digit := func(d string) gogui.View {
		return f.key(key{
			id: "digit-" + d, w: kw, h: kh, fill: sk.key, ink: sk.onKey(), edge: sk.edge,
			edgeWide: sk.edgeWide, radius: sk.radius, align: gogui.HAlignCenter,
			content: []gogui.View{sk.label(d, size, sk.onKey(), true)},
			run:     func() { f.v.pad.Press(d, codeMost, func(string) {}) },
		})
	}
	special := func(id, glyph string, run func()) gogui.View {
		return f.key(key{
			id: id, w: kw, h: kh, fill: sk.key, ink: sk.muted, edge: sk.edge,
			edgeWide: sk.edgeWide, radius: sk.radius, align: gogui.HAlignCenter,
			content: []gogui.View{icon(glyph, size, sk.muted)},
			run:     run,
		})
	}
	line := func(keys ...gogui.View) gogui.View { return row(w, kh, gap, gogui.HAlignCenter, keys...) }
	return column(w, h, gap, gogui.HAlignCenter,
		line(digit("1"), digit("2"), digit("3")),
		line(digit("4"), digit("5"), digit("6")),
		line(digit("7"), digit("8"), digit("9")),
		line(
			special("clear", "mdi:close", f.v.pad.Clear),
			digit("0"),
			special("back", "mdi:backspace-outline", f.v.pad.Back),
		),
	)
}

func (f *face) sheet(sk skin) gogui.View {
	u := f.unit()
	pw := min(f.vw*0.9, u*0.78)
	ph := min(f.vh*0.94, u*0.98)
	pad := u * 0.035
	inner := pw - 2*pad
	head := u * 0.06
	shown := u * 0.08
	foot := u * 0.1
	grid := ph - 2*pad - head - shown - foot - 3*pad*0.6
	sk2 := sk
	note := f.note
	title := say.T("homealarm.code.title")
	if note != "" {
		title = note
	}
	buttons := row(inner, foot, pad*0.6, gogui.HAlignCenter,
		f.key(key{
			id: "sheet-cancel", w: (inner - pad*0.6) / 2, h: foot, fill: sk.key, ink: sk.onKey(),
			edge: sk.edge, edgeWide: sk.edgeWide, radius: sk.radius, align: gogui.HAlignCenter,
			content: []gogui.View{sk.label(say.T("homealarm.cancel"), foot*0.36, sk.onKey(), true)},
			run:     f.v.cancel,
		}),
		f.key(key{
			id: "sheet-ok", w: (inner - pad*0.6) / 2, h: foot, fill: sk.chosen, ink: sk.chosenInk,
			edge: sk.edge, edgeWide: sk.edgeWide, radius: sk.radius, align: gogui.HAlignCenter,
			content: []gogui.View{sk.label(say.T("homealarm.ok"), foot*0.36, sk.chosenInk, true)},
			run:     f.v.submit,
		}),
	)
	panel := gogui.Column(gogui.ContainerCfg{
		Width:       pw,
		Height:      ph,
		Sizing:      gogui.FixedFixed,
		Color:       sk.panel,
		ColorBorder: sk.edge,
		SizeBorder:  gogui.BorderPx(max(sk.edgeWide, 1)),
		Radius:      gogui.RadiusPx(sk.radius * 1.5),
		Padding:     gogui.PadAll(pad),
		Spacing:     gogui.SpacingPx(pad * 0.6),
		HAlign:      gogui.HAlignCenter,
		Content: []gogui.View{
			row(inner, head, 0, gogui.HAlignCenter, sk2.label(title, head*0.55, sk.ink, true)),
			row(inner, shown, 0, gogui.HAlignCenter,
				sk.label(f.dots(sk), shown*0.5, sk.chosen, true)),
			f.padGrid(sk, inner, grid),
			buttons,
		},
	})
	return gogui.Column(gogui.ContainerCfg{
		Float:   !f.board,
		Width:   f.vw,
		Height:  f.vh,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Color:   gogui.RGBA(0, 0, 0, 0xaa),
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		OnClick: func(e gogui.EventCtx) { e.Consume() },
		Content: []gogui.View{panel},
	})
}
