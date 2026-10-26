package alertview

import (
	"image"
	"slices"
	"strconv"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	mdi "github.com/ygelfand/libcountertop/pkg/display/icons"
	"github.com/ygelfand/libcountertop/pkg/display/message"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	bannerLines  = 3
	pictureShare = 2.5
	closeKey     = -1
	slideFrame   = 16 * time.Millisecond
)

type Messages interface {
	Press(key int)
	Discard()
	Catch(area image.Rectangle)
	Offset() (int, bool)
}

type View struct {
	UI       *widgets.UI
	Messages Messages
	Again    func(w *gogui.Window, after time.Duration)
}

func Covers(s schema.AlertStyle) bool { return s == schema.AlertFull }

func (v *View) Build(w *gogui.Window, m message.Message, s schema.AlertStyle) gogui.View {
	view := v.styled(w, m, s)
	dx, moving := v.Messages.Offset()
	if moving && v.Again != nil {
		v.Again(w, slideFrame)
	}
	if dx == 0 {
		return view
	}
	vw, vh := w.WindowSize()
	return widgets.Placed(ui.Rect{X: dx, W: vw, H: vh}, view)
}

func (v *View) styled(w *gogui.Window, m message.Message, s schema.AlertStyle) gogui.View {
	switch s {
	case schema.AlertCard:
		return v.card(w, m)
	case schema.AlertFull:
		return v.full(w, m)
	case schema.AlertToast:
		return v.toast(w, m)
	}
	return v.banner(w, m)
}

func (v *View) banner(w *gogui.Window, m message.Message) gogui.View {
	t := gogui.CurrentTheme()
	vw, vh := w.WindowSize()
	pal := v.UI.Palette()
	tint := m.Tint(pal)
	margin := style.Reach() * 0.5
	width := float32(vw) - 2*margin
	keySt := t.Cfg.TextStyleDef
	beside := keysWidth(w, m, keySt, style.Reach()) <= width*0.3

	head := t.TextStyleTitle
	head.Color = widgets.Color(tint)
	words := messageText(w, m, head, t.Cfg.TextStyleDef, bannerLines)
	if !beside {
		words = append(words, v.keys(w, m, pal, keySt, style.Reach(), true)...)
	}
	content := []gogui.View{widgets.Edge(v.pulse(w, m, tint, pal.Background), t.Cfg.RadiusMedium)}
	content = append(content, messageIcon(m, widgets.Color(tint), t.TextStyleTitle.Size*1.6)...)
	content = append(content, gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFit,
		Padding: gogui.NoPadding,
		Spacing: gogui.SpacingSmall,
		Content: words,
	}))
	if beside {
		content = append(content, v.keys(w, m, pal, keySt, style.Reach(), false)...)
	}
	banner := gogui.Row(widgets.Blocking(v.UI.Toolkit.Panel(gogui.ContainerCfg{
		ID:          "message",
		Width:       width,
		MaxHeight:   float32(vh) * 0.6,
		Sizing:      gogui.FixedFit,
		VAlign:      gogui.VAlignMiddle,
		Padding:     gogui.PaddingMedium,
		Spacing:     gogui.SpacingMedium,
		Clip:        true,
		AmendLayout: v.caught,
		Content:     content,
	})))
	return gogui.Column(gogui.ContainerCfg{
		Width:   float32(vw),
		Height:  float32(vh),
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NewPadding(margin, margin, margin, margin),
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignTop,
		Content: []gogui.View{banner},
	})
}

func (v *View) card(w *gogui.Window, m message.Message) gogui.View {
	t := gogui.CurrentTheme()
	vw, vh := w.WindowSize()
	pal := v.UI.Palette()
	tint := m.Tint(pal)
	icon := messageIcon(m, widgets.Color(tint), t.TextStyleDisplay.Size*1.6)

	head := t.TextStyleDisplay
	head.Color = widgets.Color(tint)
	var words []gogui.View
	if vw < vh {
		words = append(words, icon...)
		icon = nil
	}
	words = append(words, messageText(w, m, head, t.TextStyleTitle, 0)...)
	words = append(words, v.keys(w, m, pal, t.Cfg.TextStyleDef, style.Reach(), true)...)

	content := []gogui.View{widgets.Edge(v.pulse(w, m, tint, pal.Background), t.Cfg.RadiusMedium)}
	content = append(content, icon...)
	content = append(content, gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFit,
		Padding: gogui.NoPadding,
		Spacing: gogui.SpacingMedium,
		Content: words,
	}))
	card := gogui.Row(v.UI.Toolkit.Panel(gogui.ContainerCfg{
		ID:        "message",
		Width:     float32(vw) * 0.82,
		MaxHeight: float32(vh) * 0.85,
		Sizing:    gogui.FixedFit,
		VAlign:    gogui.VAlignMiddle,
		Padding:   gogui.PaddingLarge,
		Spacing:   gogui.SpacingLarge,
		Clip:      true,
		Content:   content,
	}))
	return gogui.Column(widgets.Blocking(gogui.ContainerCfg{
		Width:       float32(vw),
		Height:      float32(vh),
		Sizing:      gogui.FixedFixed,
		Padding:     gogui.NoPadding,
		Color:       gogui.Black.WithOpacity(0.5),
		HAlign:      gogui.HAlignCenter,
		VAlign:      gogui.VAlignMiddle,
		AmendLayout: v.caught,
		Content:     []gogui.View{card},
	}))
}

func (v *View) full(w *gogui.Window, m message.Message) gogui.View {
	t := gogui.CurrentTheme()
	vw, vh := w.WindowSize()
	pal := v.UI.Palette()
	tint := m.Tint(pal)
	ink := pal.Contrast(tint)
	deeper := widgets.White
	if theme.Dark(tint) {
		deeper = widgets.Black
	}

	head := t.TextStyleDisplay
	head.Size *= 1.3
	head.Color, head.Align = widgets.Color(ink), gogui.TextAlignCenter
	body := t.TextStyleTitle
	body.Color, body.Align = widgets.Color(ink), gogui.TextAlignCenter
	middle := messageIcon(m, widgets.Color(ink), t.TextStyleDisplay.Size*3)
	middle = append(middle, messageText(w, m, head, body, 0)...)

	content := []gogui.View{gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFill,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Padding: gogui.NoPadding,
		Spacing: gogui.SpacingLarge,
		Clip:    true,
		Content: middle,
	})}
	content = append(content, v.keys(w, m, pal, t.TextStyleTitle, style.Reach()*1.4, true)...)
	return gogui.Column(widgets.Blocking(gogui.ContainerCfg{
		ID:          "message",
		Width:       float32(vw),
		Height:      float32(vh),
		Sizing:      gogui.FixedFixed,
		Color:       v.pulse(w, m, tint, deeper),
		Padding:     gogui.PaddingLarge,
		Spacing:     gogui.SpacingLarge,
		AmendLayout: v.caught,
		Content:     content,
	}))
}

func (v *View) toast(w *gogui.Window, m message.Message) gogui.View {
	t := gogui.CurrentTheme()
	vw, vh := w.WindowSize()
	pal := v.UI.Palette()
	tint := m.Tint(pal)
	margin := style.Reach() * 0.5
	room := float32(vw) - 2*margin
	st := t.Cfg.TextStyleDef
	tall := st.Size * 2
	keys := keysWidth(w, m, st, tall)
	inline := keys <= room*0.35

	icon := messageIcon(m, widgets.Color(tint), st.Size*1.4)
	bar := t.Cfg.RadiusMedium * 0.6
	pad := t.Cfg.PaddingMedium.Left
	textRoom := room - 2*pad - bar - 2*t.Cfg.SpacingMedium
	if len(icon) > 0 {
		textRoom -= st.Size*1.4 + t.Cfg.SpacingMedium
	}
	if inline && keys > 0 {
		textRoom -= keys + t.Cfg.SpacingMedium
	}

	head := st
	head.Color = widgets.Color(tint)
	line, rest := m.Title, m.Body
	if line == "" {
		line, rest = m.Body, ""
	}
	words := []gogui.View{gogui.Label(line, head)}
	if rest != "" && w.TextWidth(rest, widgets.Secondary()) <= textRoom {
		words = append(words, gogui.Label(rest, widgets.Secondary()))
	}

	top := []gogui.View{widgets.Edge(v.pulse(w, m, tint, pal.Background), bar)}
	top = append(top, icon...)
	top = append(top, gogui.Column(gogui.ContainerCfg{
		MaxWidth: max(textRoom, 0),
		Padding:  gogui.NoPadding,
		Clip:     true,
		Content:  words,
	}))
	panel := gogui.ContainerCfg{
		ID:          "message",
		MaxWidth:    room,
		VAlign:      gogui.VAlignMiddle,
		Padding:     gogui.PaddingMedium,
		Spacing:     gogui.SpacingMedium,
		Clip:        true,
		AmendLayout: v.caught,
	}
	layout := gogui.Row
	if inline {
		panel.Content = append(top, v.keys(w, m, pal, st, tall, false)...)
	} else {
		layout = gogui.Column
		panel.Width, panel.Sizing = room, gogui.FixedFit
		panel.Content = append([]gogui.View{gogui.Row(gogui.ContainerCfg{
			VAlign:  gogui.VAlignMiddle,
			Padding: gogui.NoPadding,
			Spacing: gogui.SpacingMedium,
			Content: top,
		})}, v.keys(w, m, pal, st, tall, true)...)
	}
	toast := layout(widgets.Blocking(v.UI.Toolkit.Panel(panel)))
	return gogui.Column(gogui.ContainerCfg{
		Width:   float32(vw),
		Height:  float32(vh),
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NewPadding(margin, margin, style.Reach(), margin),
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignBottom,
		Content: []gogui.View{toast},
	})
}

func (v *View) pulse(w *gogui.Window, m message.Message, c, toward theme.Color) gogui.Color {
	if !m.Flash {
		return widgets.Color(c)
	}
	return widgets.Color(widgets.Pulse(w, v.Again, c, toward))
}

func (v *View) caught(e gogui.EventCtx) {
	s := e.Layout.Shape
	v.Messages.Catch(image.Rect(int(s.X), int(s.Y), int(s.X+s.Width), int(s.Y+s.Height)))
}

func messageText(
	w *gogui.Window,
	m message.Message,
	head, body gogui.TextStyle,
	lines int,
) []gogui.View {
	var words []gogui.View
	if m.Title != "" {
		words = append(
			words,
			gogui.Text(gogui.TextCfg{Text: m.Title, TextStyle: head, Mode: gogui.TextModeWrap}),
		)
	}
	if m.Body == "" {
		return words
	}
	text := gogui.Text(gogui.TextCfg{Text: m.Body, TextStyle: body, Mode: gogui.TextModeWrap})
	if lines > 0 {
		text = gogui.Column(gogui.ContainerCfg{
			Sizing:    gogui.FillFit,
			MaxHeight: float32(lines) * lineHeight(w, body),
			Padding:   gogui.NoPadding,
			Clip:      true,
			Content:   []gogui.View{text},
		})
	}
	return append(words, text)
}

func lineHeight(w *gogui.Window, st gogui.TextStyle) float32 {
	if tm := w.TextMeasurer(); tm != nil {
		return tm.FontHeight(st)
	}
	return st.Size * 1.4
}

func messageIcon(m message.Message, ink gogui.Color, size float32) []gogui.View {
	if p := m.Picture; p.Src != "" && p.Width > 0 && p.Height > 0 {
		tall := size * pictureShare
		return []gogui.View{gogui.Image(gogui.ImageCfg{
			Src:    p.Src,
			Width:  tall * float32(p.Width) / float32(p.Height),
			Height: tall,
		})}
	}
	if m.Icon == "" {
		return nil
	}
	glyph := mdi.Named(m.Icon)
	if glyph == 0 {
		glyph = mdi.Named("alert-outline")
	}
	st := gogui.CurrentTheme().TextStyleDisplay
	st.Family, st.Color, st.Size = mdi.Family, ink, size
	return []gogui.View{gogui.Label(mdi.Glyph(glyph), st)}
}

func keysWidth(w *gogui.Window, m message.Message, st gogui.TextStyle, tall float32) float32 {
	if len(m.Keys) == 0 {
		return 0
	}
	sum := gogui.CurrentTheme().Cfg.SpacingMedium * float32(len(m.Keys)-1)
	for _, k := range m.Keys {
		sum += widgets.KeyWidth(w, k.Text, st, tall)
	}
	return sum
}

func (v *View) keys(
	w *gogui.Window,
	m message.Message,
	pal theme.Theme,
	st gogui.TextStyle,
	tall float32,
	fill bool,
) []gogui.View {
	if len(m.Keys) == 0 {
		return nil
	}
	keys := make([]gogui.View, 0, len(m.Keys)+1)
	all := append(slices.Clone(m.Keys), message.Key{Text: say.T("message.close")})
	for i, k := range all {
		cfg := gogui.ContainerCfg{
			ID:      "message-key-" + strconv.Itoa(i),
			Width:   widgets.KeyWidth(w, k.Text, st, tall),
			Height:  tall,
			Sizing:  gogui.FixedFixed,
			Radius:  gogui.RadiusLarge,
			Color:   widgets.KeyFill(pal),
			HAlign:  gogui.HAlignCenter,
			VAlign:  gogui.VAlignMiddle,
			Padding: gogui.NoPadding,
		}
		if fill {
			cfg.Width, cfg.Sizing = 0, gogui.FillFixed
		}
		if i == len(m.Keys) {
			i = closeKey
		}
		keys = append(keys, v.key(i, k, pal, cfg, st))
	}
	row := gogui.ContainerCfg{Padding: gogui.NoPadding, Spacing: gogui.SpacingMedium, Content: keys}
	if fill {
		row.Sizing = gogui.FillFit
	}
	return []gogui.View{gogui.Row(row)}
}

func (v *View) key(
	i int,
	k message.Key,
	pal theme.Theme,
	cfg gogui.ContainerCfg,
	st gogui.TextStyle,
) gogui.View {
	press := func(gogui.EventCtx) { v.Messages.Press(i) }
	if i == closeKey {
		press = func(gogui.EventCtx) { v.Messages.Discard() }
	}
	c, ok := message.Swatch(k.Swatch, pal)
	if !ok {
		return v.UI.Toolkit.Keyed(cfg, k.Text, st, style.Rest, press)
	}
	st.Color = widgets.Color(pal.Contrast(c))
	cfg.Color = widgets.Color(c)
	cfg.Content = []gogui.View{gogui.Label(k.Text, st)}
	return v.UI.Presses.Pressable(gogui.Row, cfg, press)
}
