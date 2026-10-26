package widgets

import (
	"strings"
	"unicode"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type KeyboardSize = schema.KeyboardSize

const (
	KeyboardCompact  = schema.KeyboardCompact
	KeyboardStandard = schema.KeyboardStandard
	KeyboardLarge    = schema.KeyboardLarge
)

type KeyboardStyle interface {
	Key(cfg *gogui.ContainerCfg, st *gogui.TextStyle, state style.State)
	Panel(cfg *gogui.ContainerCfg)
}

type KeyboardOptions struct {
	Size         KeyboardSize
	Palette      theme.Theme
	Kit          KeyboardStyle
	Cancel, Save string
	Type         func(rune)
	Press        func(gogui.KeyCode)
}

var (
	letterRows = []string{"qwertyuiop", "asdfghjkl", "zxcvbnm"}
	symbolRows = []string{"1234567890", "-/:;()&@\"", ".,?!'#"}
)

func (editor *Editor) Keyboard(w *gogui.Window, opts KeyboardOptions) (ui.Rect, gogui.View) {
	pal := opts.Palette
	vw, vh := w.WindowSize()
	size := opts.Size
	rowScale, textScale := keySize(size)
	gap := style.Reach() * 0.12
	pad := style.Reach() * 0.2
	high := style.Reach() * rowScale
	unit := min((float32(vw)-2*pad-9*gap)/10, high*1.6)

	plain := pal.Background.Blend(pal.Text, 0.18)
	special := pal.Background.Blend(pal.Text, 0.10)
	key := func(id, label string, width float32, fill theme.Color, do func()) gogui.View {
		state := style.Rest
		if fill == special {
			state = style.Partial
		}
		st := gogui.CurrentTheme().Cfg.TextStyleDef
		if label == gogui.IconArrowUp || label == gogui.IconArrowLeft {
			st = gogui.CurrentTheme().TextStyleIconMedium
		}
		st.Size *= textScale
		st.Color = Color(pal.Text)
		cfg := gogui.ContainerCfg{
			ID:      "key-" + id,
			Width:   width,
			Height:  high,
			Sizing:  gogui.FixedFixed,
			Radius:  gogui.RadiusMedium,
			Color:   Color(fill),
			HAlign:  gogui.HAlignCenter,
			VAlign:  gogui.VAlignMiddle,
			Padding: gogui.NoPadding,
			OnClick: func(e gogui.EventCtx) {
				do()
				e.Window.InvalidateLayout()
				e.Consume()
			},
		}
		opts.Kit.Key(&cfg, &st, state)
		cfg.Content = []gogui.View{gogui.Label(label, st)}
		return gogui.Row(cfg)
	}

	typeRune := func(ch rune) func() {
		return func() {
			typed := ch
			if editor.Shift {
				typed = unicode.ToUpper(ch)
				editor.Shift = false
			}
			opts.Type(typed)
		}
	}

	rows := letterRows
	if editor.Symbols {
		rows = symbolRows
	}
	var lines []gogui.View
	for i, row := range rows {
		var line []gogui.View
		if i == 2 {
			label := gogui.IconArrowUp
			if editor.Symbols {
				label = ""
			}
			line = append(line, key("shift", label, unit*1.5+gap*0.5, special, func() {
				if !editor.Symbols {
					editor.Shift = !editor.Shift
				}
			}))
		}
		for _, ch := range row {
			label := string(ch)
			if editor.Shift && !editor.Symbols {
				label = strings.ToUpper(label)
			}
			line = append(line, key(string(ch), label, unit, plain, typeRune(ch)))
		}
		if i == 2 {
			line = append(
				line,
				key(
					"back",
					gogui.IconArrowLeft,
					unit*1.5+gap*0.5,
					special,
					func() { opts.Press(gogui.KeyBackspace) },
				),
			)
		}
		lines = append(lines, keyRow(line, gap))
	}

	mode := "123"
	if editor.Symbols {
		mode = "abc"
	}
	lines = append(lines, keyRow([]gogui.View{
		key(
			"mode",
			mode,
			unit*2+gap,
			special,
			func() { editor.Symbols, editor.Shift = !editor.Symbols, false },
		),
		key("space", " ", unit*4+gap*3, plain, func() { opts.Type(' ') }),
		key("cancel", opts.Cancel, unit*2+gap, special, func() { w.ClearFocus() }),
		key("save", opts.Save, unit*2+gap, special, func() {
			editor.Save()
			w.ClearFocus()
		}),
	}, gap))

	tall := int(4*high + 3*gap + pad*2.5)
	at := ui.Rect{Y: vh - tall, W: vw, H: tall}
	editor.Top = float32(at.Y)
	var corners gogui.Radius
	if size == KeyboardCompact {
		at.W = min(int(10*unit+9*gap+2*pad)+1, vw)
		at.X = vw - at.W
		corners = gogui.RadiusLarge
	}
	tray := gogui.ContainerCfg{
		ID:      "keyboard",
		Sizing:  gogui.FillFill,
		Padding: gogui.NewPadding(pad, pad, pad*1.5, pad),
		Spacing: gogui.SpacingPx(gap),
		Color:   Color(pal.Background.Blend(pal.Text, 0.04)),
		Radius:  corners,
		HAlign:  gogui.HAlignCenter,
		Content: lines,
		OnClick: func(e gogui.EventCtx) { e.Consume() },
	}
	opts.Kit.Panel(&tray)
	tray.Color = gogui.RGB(tray.Color.R, tray.Color.G, tray.Color.B)
	return at, gogui.Column(tray)
}

func keySize(k KeyboardSize) (row, text float32) {
	switch k {
	case KeyboardStandard:
		return 1, 1.15
	case KeyboardLarge:
		return 1.2, 1.25
	}
	return 0.75, 1
}

func keyRow(keys []gogui.View, gap float32) gogui.View {
	return gogui.Row(gogui.ContainerCfg{
		Sizing:  gogui.FillFit,
		Padding: gogui.NoPadding,
		Spacing: gogui.SpacingPx(gap),
		HAlign:  gogui.HAlignCenter,
		Content: keys,
	})
}
