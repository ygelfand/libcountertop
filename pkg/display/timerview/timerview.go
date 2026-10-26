package timerview

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/go-gui-org/go-glyph"
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/assistant/timer"
	"github.com/ygelfand/libcountertop/pkg/display/clockview"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	dialShare    = 0.62
	squarish     = 1.15
	sweepFastest = time.Second / 24
	sweepSlowest = time.Second
	sweepStep    = 2
)

type Timers interface {
	Stop() bool
}

type View struct {
	UI     *widgets.UI
	Timers Timers
	Again  func(w *gogui.Window, after time.Duration)
}

type Scene struct {
	Clock           schema.Clock
	WeatherTopRight bool
	ClearLeft       float32
	ClearRight      float32
}

func Covers(c timer.Card, s schema.TimerStyle) bool { return c.Showing && s == schema.TimerFull }

func (v *View) Build(w *gogui.Window, c timer.Card, s schema.TimerStyle, scene Scene) gogui.View {
	switch s {
	case schema.TimerBanner:
		return v.banner(w, c)
	case schema.TimerDial:
		return v.dialStyle(w, c)
	case schema.TimerFull:
		return v.full(w, c)
	case schema.TimerFace:
		return v.face(w, c, scene.Clock)
	case schema.TimerCorner:
		return v.corner(w, c, scene)
	}
	return v.bar(w, c)
}

func (v *View) bar(w *gogui.Window, c timer.Card) gogui.View {
	t := gogui.CurrentTheme()
	vw, vh := w.WindowSize()
	ink := t.Cfg.TextStyleDef.Color
	glyph := gogui.IconClock
	if c.Ringing {
		ink, glyph = t.Cfg.ColorError, gogui.IconBell
	}
	big := t.TextStyleTitle
	big.Color = ink

	line := []gogui.View{
		gogui.Label(glyph, widgets.IconStyle(ink)),
		gogui.Label(timer.Remaining(c.Left), big),
	}
	if c.Name != "" {
		line = append(line, gogui.Label(c.Name, widgets.Secondary()))
	}
	content := []gogui.View{
		gogui.Row(
			gogui.ContainerCfg{
				Sizing:  gogui.FillFit,
				VAlign:  gogui.VAlignMiddle,
				Padding: gogui.NoPadding,
				Spacing: gogui.SpacingMedium,
				Content: line,
			},
		),
	}
	if !c.Ringing && c.Of > 0 {
		content = append(
			content,
			v.UI.Toolkit.ProgressBar(
				gogui.ProgressBarCfg{Percent: float32(c.Share()), Sizing: gogui.FillFit},
			),
		)
	}

	card := v.UI.Presses.Pressable(
		gogui.Column,
		v.UI.Toolkit.Panel(gogui.ContainerCfg{
			ID:      "timer",
			Width:   float32(vw) * 0.84,
			Sizing:  gogui.FixedFit,
			Padding: gogui.PaddingMedium,
			Spacing: gogui.SpacingSmall,
			Content: content,
		}),
		v.tap(c),
	)
	return gogui.Column(gogui.ContainerCfg{
		Width:   float32(vw),
		Height:  float32(vh),
		Sizing:  gogui.FixedFixed,
		Padding: gogui.PaddingMedium,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignBottom,
		Content: []gogui.View{card},
	})
}

func (v *View) banner(w *gogui.Window, c timer.Card) gogui.View {
	t := gogui.CurrentTheme()
	vw, vh := w.WindowSize()
	pal := v.UI.Palette()
	tint := widgets.Color(v.tint(w, c, pal.Background))
	margin := style.Reach() * 0.5
	label, name := timerReads(c)

	head := t.TextStyleTitle
	head.Color = tint
	words := []gogui.View{gogui.Label(label, head)}
	if name != "" {
		words = append(words, gogui.Label(name, widgets.Secondary()))
	}
	icon := widgets.IconStyle(tint)
	icon.Size = t.TextStyleTitle.Size * 1.6
	line := []gogui.View{
		widgets.Edge(tint, t.Cfg.RadiusMedium),
		gogui.Label(timerGlyph(c), icon),
		gogui.Column(gogui.ContainerCfg{
			Sizing:  gogui.FillFit,
			Padding: gogui.NoPadding,
			Spacing: gogui.SpacingSmall,
			Clip:    true,
			Content: words,
		}),
	}
	if c.Ringing {
		line = append(line, v.stopKey(w, t.Cfg.TextStyleDef, style.Reach(), 0, c))
	}
	content := []gogui.View{gogui.Row(gogui.ContainerCfg{
		Sizing:  gogui.FillFit,
		VAlign:  gogui.VAlignMiddle,
		Padding: gogui.NoPadding,
		Spacing: gogui.SpacingMedium,
		Content: line,
	})}
	if !c.Ringing && c.Of > 0 {
		content = append(content, v.UI.Toolkit.ProgressBar(gogui.ProgressBarCfg{
			Percent:  float32(c.Share()),
			Height:   max(t.Cfg.RadiusMedium*0.6, 2),
			Sizing:   gogui.FillFixed,
			ColorBar: widgets.Color(pal.Accent),
		}))
	}
	banner := v.UI.Presses.Pressable(
		gogui.Column,
		widgets.Blocking(v.UI.Toolkit.Panel(gogui.ContainerCfg{
			ID:      "timer",
			Width:   float32(vw) - 2*margin,
			Sizing:  gogui.FixedFit,
			Padding: gogui.PaddingMedium,
			Spacing: gogui.SpacingSmall,
			Clip:    true,
			Content: content,
		})),
		v.tap(c),
	)
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

func (v *View) dialStyle(w *gogui.Window, c timer.Card) gogui.View {
	vw, vh := w.WindowSize()
	scrim := gogui.ContainerCfg{
		Width:   float32(vw),
		Height:  float32(vh),
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Color:   gogui.Black.WithOpacity(0.5),
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Content: []gogui.View{v.dial(w, c, float32(min(vw, vh))*dialShare)},
	}
	if c.Ringing {
		scrim = widgets.Blocking(scrim)
	}
	return gogui.Column(scrim)
}

func (v *View) dial(w *gogui.Window, c timer.Card, side float32) gogui.View {
	pal := v.UI.Palette()
	tint := v.tint(w, c, pal.Surface)
	wide := side * 0.07
	radius := side/2 - wide*1.1
	share := c.Share()
	if c.Ringing {
		share = 1
	} else {
		v.sweep(w, c, 2*math.Pi*radius)
	}
	label, under := timerReads(c)

	big := clockview.Lettering(glyph.TypefaceBold, 0, pal.Text)
	big.Size = clockview.Fitted(w, label, big, side*0.56, side*0.24)
	small := clockview.Lettering(glyph.TypefaceRegular, 0, pal.Muted)
	if c.Ringing {
		big = widgets.IconStyle(widgets.Color(tint))
		big.Size = side * 0.24
		label = timerGlyph(c)
		under = say.T("timer.stop")
		small = clockview.Lettering(glyph.TypefaceBold, 0, tint)
	}
	if under != "" {
		small.Size = clockview.Fitted(w, under, small, side*0.5, side*0.08)
	}

	fill := widgets.Color(tint)
	track := widgets.Color(tint.Blend(pal.Surface, 0.8))
	ring := gogui.DrawCanvas(gogui.DrawCanvasCfg{
		ID:      "timer-dial",
		Width:   side,
		Height:  side,
		Version: uint64(time.Now().UnixNano()),
		OnDraw: func(dc *gogui.DrawContext) {
			cx, cy := side/2, side/2
			drawRing(dc, cx, cy, radius, wide, share, fill, track)

			tall := dc.FontHeight(big)
			block, gap := tall, float32(0)
			if under != "" {
				gap = side * 0.02
				block += gap + dc.FontHeight(small)
			}
			top := cy - block/2
			dc.Text(cx-dc.TextWidth(label, big)/2, top, label, big)
			if under != "" {
				dc.Text(cx-dc.TextWidth(under, small)/2, top+tall+gap, under, small)
			}
		},
	})
	disc := v.UI.Toolkit.Panel(gogui.ContainerCfg{
		ID:      "timer",
		Width:   side,
		Height:  side,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Clip:    true,
		Content: []gogui.View{ring},
	})
	disc.Radius = gogui.RadiusPx(side / 2)
	return v.UI.Presses.Pressable(gogui.Column, widgets.Blocking(disc), v.tap(c))
}

func (v *View) full(w *gogui.Window, c timer.Card) gogui.View {
	t := gogui.CurrentTheme()
	vw, vh := w.WindowSize()
	fw, fh := float32(vw), float32(vh)
	pal := v.UI.Palette()
	label, name := timerReads(c)

	back, ink, muted := pal.Background, pal.Text, pal.Muted
	if c.Ringing {
		deeper := widgets.White
		if theme.Dark(pal.Warning) {
			deeper = widgets.Black
		}
		back = widgets.Pulse(w, v.Again, pal.Warning, deeper)
		ink = pal.Contrast(pal.Warning)
		muted = ink
	}

	big := clockview.Lettering(glyph.TypefaceBold, 0, ink)
	big.Size = clockview.Fitted(w, label, big, fw*0.86, fh*0.42)
	var middle []gogui.View
	if c.Ringing {
		bell := widgets.IconStyle(widgets.Color(ink))
		bell.Size = big.Size * 0.5
		middle = append(middle, gogui.Label(timerGlyph(c), bell))
	}
	middle = append(middle, gogui.Label(label, big))
	if name != "" {
		st := t.TextStyleTitle
		st.Color, st.Align = widgets.Color(muted), gogui.TextAlignCenter
		middle = append(middle, gogui.Label(name, st))
	}
	content := []gogui.View{gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFill,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Padding: gogui.NoPadding,
		Spacing: gogui.SpacingLarge,
		Clip:    true,
		Content: middle,
	})}
	if c.Ringing {
		key := v.loudStopKey(w, t.TextStyleTitle, style.Reach()*1.6, fw*0.4, c)
		content = append(content, gogui.Row(gogui.ContainerCfg{
			Sizing:  gogui.FillFit,
			HAlign:  gogui.HAlignCenter,
			Padding: gogui.NoPadding,
			Content: []gogui.View{key},
		}))
	}

	var tide *gogui.GradientDef
	if !c.Ringing {
		v.sweep(w, c, fh)
		drained := 1 - float32(c.Share())
		full := widgets.Color(back)
		tinted := widgets.Color(pal.Accent).WithOpacity(0.28).Over(full)
		tide = &gogui.GradientDef{
			Direction: gogui.GradientToBottom,
			Stops: []gogui.GradientStop{
				{Color: full, Pos: 0},
				{Color: full, Pos: drained},
				{Color: tinted, Pos: drained},
				{Color: tinted, Pos: 1},
			},
		}
	}
	return v.UI.Presses.Pressable(gogui.Column, widgets.Blocking(gogui.ContainerCfg{
		ID:       "timer",
		Width:    fw,
		Height:   fh,
		Sizing:   gogui.FixedFixed,
		Color:    widgets.Color(back),
		Radius:   gogui.NoRadius,
		Gradient: tide,
		Padding:  gogui.PaddingLarge,
		Spacing:  gogui.SpacingLarge,
		Content:  content,
	}),

		v.tap(c))
}

func (v *View) face(w *gogui.Window, c timer.Card, clock schema.Clock) gogui.View {
	vw, vh := w.WindowSize()
	pal := v.UI.Palette()
	ink := clock.Ink.Over(pal)
	ink.Text = v.tint(w, c, ink.Surface)

	face := timerFace(clock.Face)
	left, name := c.Left, c.Name
	if c.Ringing {
		left, name = 0, ""
	}
	bw := float32(vw) * 0.8
	bh := min(float32(vh)*0.55, bw*0.62)
	box := ui.Rect{W: int(bw), H: int(bh)}
	panel := v.UI.Presses.Pressable(
		gogui.Column,
		widgets.Blocking(v.UI.Toolkit.Panel(gogui.ContainerCfg{
			ID:      "timer",
			Width:   bw,
			Height:  bh,
			Sizing:  gogui.FixedFixed,
			Padding: gogui.NoPadding,
			Clip:    true,
			Content: []gogui.View{face.Draw(w, box, timerReading(left, face, name), ink)},
		})),
		v.tap(c),
	)
	return gogui.Column(gogui.ContainerCfg{
		Width:   float32(vw),
		Height:  float32(vh),
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Content: []gogui.View{panel},
	})
}

func (v *View) corner(w *gogui.Window, c timer.Card, scene Scene) gogui.View {
	if c.Ringing {
		return v.dialStyle(w, c)
	}
	t := gogui.CurrentTheme()
	vw, vh := w.WindowSize()
	pal := v.UI.Palette()
	label := timer.Remaining(c.Left)

	st := t.TextStyleTitle
	st.Color = widgets.Color(pal.Text)
	mini := st.Size * 1.6
	wide := mini * 0.16
	radius := mini/2 - wide/2
	share := c.Share()
	v.sweep(w, c, 2*math.Pi*radius)
	fill, track := widgets.Color(pal.Accent), widgets.Color(pal.Accent.Blend(pal.Surface, 0.75))
	ring := gogui.DrawCanvas(gogui.DrawCanvasCfg{
		ID:      "timer-corner",
		Width:   mini,
		Height:  mini,
		Version: uint64(time.Now().UnixNano()),
		OnDraw: func(dc *gogui.DrawContext) {
			drawRing(dc, mini/2, mini/2, radius, wide, share, fill, track)
		},
	})

	pad := t.Cfg.PaddingMedium.Left
	gap := t.Cfg.SpacingMedium
	words := w.TextWidth(zeroed(label), st) * 1.05
	cw, ch := 2*pad+mini+gap+words, 2*pad+mini
	chip := v.UI.Presses.Pressable(
		gogui.Row,
		widgets.Blocking(v.UI.Toolkit.Panel(gogui.ContainerCfg{
			ID:      "timer",
			Width:   cw,
			Height:  ch,
			Sizing:  gogui.FixedFixed,
			VAlign:  gogui.VAlignMiddle,
			Padding: gogui.PaddingMedium,
			Spacing: gogui.SpacingMedium,
			Clip:    true,
			Content: []gogui.View{ring, gogui.Label(label, st)},
		})),
		v.tap(c),
	)

	margin := style.Reach() * 0.5
	left := scene.WeatherTopRight
	top := max(margin, scene.ClearRight)
	if left {
		top = max(margin, scene.ClearLeft)
	}
	x, y := cornerSpot(vw, vh, cw, margin, top)
	if left {
		x = float32(vw) - x - cw
	}
	return gogui.Column(gogui.ContainerCfg{
		Width:   float32(vw),
		Height:  float32(vh),
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Content: []gogui.View{
			widgets.Placed(ui.Rect{X: int(x), Y: int(y), W: int(cw), H: int(ch)}, chip),
		},
	})
}

func (v *View) tint(w *gogui.Window, c timer.Card, toward theme.Color) theme.Color {
	pal := v.UI.Palette()
	if !c.Ringing {
		return pal.Accent
	}
	return widgets.Pulse(w, v.Again, pal.Warning, toward)
}

func (v *View) tap(c timer.Card) func(gogui.EventCtx) {
	if !c.Ringing {
		return nil
	}
	return func(gogui.EventCtx) { v.Timers.Stop() }
}

func (v *View) stopKey(
	w *gogui.Window,
	st gogui.TextStyle,
	tall, least float32,
	c timer.Card,
) gogui.View {
	text := say.T("timer.stop")
	cfg := stopShape(w, text, st, tall, least)
	cfg.Color = widgets.KeyFill(v.UI.Palette())
	return v.UI.Toolkit.Keyed(cfg, text, st, style.Rest, v.tap(c))
}

func (v *View) loudStopKey(
	w *gogui.Window,
	st gogui.TextStyle,
	tall, least float32,
	c timer.Card,
) gogui.View {
	pal := v.UI.Palette()
	text := say.T("timer.stop")
	cfg := stopShape(w, text, st, tall, least)
	cfg.Color = widgets.Color(pal.Contrast(pal.Warning))
	cfg.HAlign, cfg.VAlign, cfg.Padding = gogui.HAlignCenter, gogui.VAlignMiddle, gogui.NoPadding
	st.Color = widgets.Color(pal.Warning)
	cfg.Content = []gogui.View{gogui.Label(text, st)}
	return v.UI.Presses.Pressable(gogui.Row, cfg, v.tap(c))
}

func stopShape(
	w *gogui.Window,
	text string,
	st gogui.TextStyle,
	tall, least float32,
) gogui.ContainerCfg {
	return gogui.ContainerCfg{
		ID:     "timer-stop",
		Width:  max(widgets.KeyWidth(w, text, st, tall), least),
		Height: tall,
		Sizing: gogui.FixedFixed,
		Radius: gogui.RadiusLarge,
	}
}

func (v *View) sweep(w *gogui.Window, c timer.Card, length float32) {
	if frame := sweepFrame(c.Of, length); frame < sweepSlowest {
		v.Again(w, frame)
	}
}

func sweepFrame(of time.Duration, length float32) time.Duration {
	if of <= 0 || length <= 0 {
		return sweepSlowest
	}
	step := time.Duration(float64(of) * sweepStep / float64(length))
	return min(max(step, sweepFastest), sweepSlowest)
}

func ringArc(share float64) (start, sweep float32) {
	turn := 2 * math.Pi
	return float32(-math.Pi/2 + turn*(1-share)), float32(turn * share)
}

func drawRing(
	dc *gogui.DrawContext,
	cx, cy, radius, wide float32,
	share float64,
	fill, track gogui.Color,
) {
	dc.Circle(cx, cy, radius, track, wide)
	if share <= 0 {
		return
	}
	start, sweep := ringArc(share)
	dc.Arc(cx, cy, radius, radius, start, sweep, fill, wide)
	for _, at := range []float32{start, start + sweep} {
		dx, dy := math.Cos(float64(at)), math.Sin(float64(at))
		dc.FilledCircle(cx+radius*float32(dx), cy+radius*float32(dy), wide/2, fill)
	}
}

func cornerSpot(vw, vh int, wide, margin, top float32) (x, y float32) {
	x, y = float32(vw)-margin-wide, top
	short, long := float32(min(vw, vh)), float32(max(vw, vh))
	if long > short*squarish {
		return x, y
	}
	reach := (short/2 - margin) / math.Sqrt2
	return min(x, float32(vw)/2+reach-wide), max(y, float32(vh)/2-reach)
}

func timerReads(c timer.Card) (label, name string) {
	if c.Ringing {
		return timer.Remaining(0), ""
	}
	return timer.Remaining(c.Left), c.Name
}

func timerGlyph(c timer.Card) string {
	if c.Ringing {
		return gogui.IconBell
	}
	return gogui.IconClock
}

func timerFace(name schema.Face) clockview.Face {
	f, ok := clockview.Of(name)
	if ok && f.Countdown != "" {
		f, ok = clockview.Of(f.Countdown)
	}
	if !ok {
		f, _ = clockview.Of(schema.DefaultFace)
	}
	return f
}

func timerReading(left time.Duration, f clockview.Face, name string) clockview.Reading {
	whole := int((max(left, 0) + time.Second - 1) / time.Second)
	h, m, s := whole/3600, whole/60%60, whole%60
	r := clockview.Reading{Date: name, Second: s}
	if h > 0 || f.Ticks {
		r.Time = fmt.Sprintf("%d:%02d", h, m)
		return r
	}
	r.Time = fmt.Sprintf("%d:%02d", m, s)
	return r
}

func zeroed(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return '0'
		}
		return r
	}, s)
}
