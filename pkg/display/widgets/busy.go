package widgets

import (
	"math"
	"sync/atomic"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/icons"
	"github.com/ygelfand/libcountertop/pkg/display/interaction"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
)

const (
	busyDots  = 12
	busyShare = 0.42
	busyMost  = 0.8
)

var busySpinning atomic.Bool

func (kit *Toolkit) Busy(w *gogui.Window, b shell.Busy, pal theme.Theme) gogui.View {
	vw, vh := w.WindowSize()
	title := gogui.CurrentTheme().TextStyleTitle
	title.Color = Color(pal.Text)
	detail := gogui.CurrentTheme().Cfg.TextStyleDef
	detail.Color = Color(pal.Text)
	wide := max(w.TextWidth(b.Text, title), w.TextWidth(b.Detail, detail))
	d := min(
		max(float32(min(vw, vh))*busyShare, wide*1.3),
		float32(min(vw, vh))*busyMost,
	)
	box := ui.Rect{
		X: int((float32(vw) - d) / 2),
		Y: int((float32(vh) - d) / 2),
		W: int(d),
		H: int(d),
	}
	lead := int(time.Now().UnixNano()/int64(SpinStep)) % busyDots
	ring := busyRing(d, b.Outcome, lead, pal)
	lines := []gogui.View{}
	if glyph := outcomeGlyph(b.Outcome); glyph != "" {
		mark := IconStyle(Color(outcomeColor(b.Outcome, pal)))
		mark.Family = icons.Family
		mark.Size = style.Reach()
		lines = append(lines, gogui.Label(icons.Glyph(icons.Named(glyph)), mark))
	}
	lines = append(lines, gogui.Label(b.Text, title))
	if b.Detail != "" {
		lines = append(lines, gogui.Label(b.Detail, detail))
	}
	if b.Outcome == shell.Failed && b.Dismiss != nil {
		lines = append(lines, kit.busyButton(w, b.Close, b.Dismiss, pal))
	}
	if b.Outcome == shell.Working {
		busyAgain(w)
	}
	return gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFill,
		Padding: gogui.NoPadding,
		Color:   gogui.RGBA(0, 0, 0, 0x99),
		OnClick: func(e gogui.EventCtx) { e.Consume() },
		Content: []gogui.View{
			Placed(box, gogui.Column(gogui.ContainerCfg{
				Sizing:  gogui.FillFill,
				Padding: gogui.NoPadding,
				Radius:  gogui.RadiusPx(d / 2),
				Color:   Color(pal.Background),
			})),
			Placed(box, ring),
			Placed(box, gogui.Column(gogui.ContainerCfg{
				Sizing:  gogui.FillFill,
				Padding: gogui.NewPadding(0, d*0.12, 0, d*0.12),
				Spacing: gogui.SpacingSmall,
				HAlign:  gogui.HAlignCenter,
				VAlign:  gogui.VAlignMiddle,
				Content: lines,
			})),
		},
	})
}

func (kit *Toolkit) busyButton(
	w *gogui.Window,
	label string,
	do func(),
	pal theme.Theme,
) gogui.View {
	st := gogui.CurrentTheme().TextStyleTitle
	st.Color = Color(pal.Contrast(pal.Accent))
	tall := style.Reach()
	return kit.Pressable(gogui.Row, gogui.ContainerCfg{
		ID:      "busy-close",
		Width:   KeyWidth(w, label, st, tall) + tall,
		Height:  tall,
		Sizing:  gogui.FixedFixed,
		Radius:  gogui.RadiusLarge,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Padding: gogui.NoPadding,
		Color:   Color(pal.Accent),
		Content: []gogui.View{gogui.Label(label, st)},
	}, func(e gogui.EventCtx) {
		do()
		e.Window.InvalidateLayout()
	})
}

func busyRing(d float32, o shell.Outcome, lead int, pal theme.Theme) gogui.View {
	return gogui.DrawCanvas(gogui.DrawCanvasCfg{
		Width: d, Height: d,
		Version: uint64(lead)<<2 | uint64(o),
		OnDraw: func(dc *gogui.DrawContext) {
			c, r, dot := d/2, d*0.44, max(d*0.03, 3)
			for i := range busyDots {
				a := 2 * math.Pi * float64(i) / busyDots
				fill := outcomeColor(o, pal)
				if o == shell.Working {
					age := (lead - i + busyDots) % busyDots
					fill = pal.Background.Blend(pal.Accent, 1-float64(age)/busyDots)
				}
				dc.FilledPolygon(
					interaction.Disc(c+r*float32(math.Sin(a)), c-r*float32(math.Cos(a)), dot),
					Color(fill),
				)
			}
		},
	})
}

func outcomeColor(o shell.Outcome, pal theme.Theme) theme.Color {
	switch o {
	case shell.Succeeded:
		return pal.Success
	case shell.Failed:
		return pal.Danger
	}
	return pal.Accent
}

func outcomeGlyph(o shell.Outcome) string {
	switch o {
	case shell.Succeeded:
		return "mdi:check-circle-outline"
	case shell.Failed:
		return "mdi:alert-circle-outline"
	}
	return ""
}

func busyAgain(w *gogui.Window) {
	if busySpinning.Swap(true) {
		return
	}
	time.AfterFunc(SpinStep, func() {
		busySpinning.Store(false)
		w.QueueCommand(func(w *gogui.Window) { w.InvalidateLayout() })
	})
}
