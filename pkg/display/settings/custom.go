package settings

import (
	"log/slog"
	"sync"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/say"
)

const draftSettle = 400 * time.Millisecond

func (p *BasicPages) CustomTheme() *shell.Page {
	e := &customEditor{p: p}
	e.pick(theme.CustomFields[0])
	return &shell.Page{
		Title: say.T("theme.custom.title"),
		Side:  e.view,
		Build: e.rows,
	}
}

type customEditor struct {
	p *BasicPages

	mu      sync.Mutex
	field   string
	value   gogui.HSLA
	pending *time.Timer
}

func (e *customEditor) rows() ([]menu.Row, []func(int)) {
	saved := e.p.state().Screen.Custom.Theme()
	field, draft := e.current()
	var (
		rows []menu.Row
		acts []func(int)
	)
	for _, f := range theme.CustomFields {
		c := saved.Color(f)
		if f == field {
			c = hslaColor(draft)
		}
		rows = append(rows, menu.Row{
			Label:   say.T("theme.custom." + f),
			Value:   c.String(),
			Chosen:  f == field,
			Preview: swatch(c),
		})
		acts = append(acts, func(int) { e.pick(f) })
	}
	return rows, acts
}

func (e *customEditor) view(w *gogui.Window) gogui.View {
	vw, vh := w.WindowSize()
	reach := style.Reach()
	size := min(float32(min(vw, vh))*0.55, reach*9)
	_, v := e.current()
	change := func(next gogui.HSLA, ev gogui.EventCtx) {
		e.set(next)
		ev.Window.InvalidateLayout()
	}
	return gogui.Row(gogui.ContainerCfg{
		Sizing:  gogui.FitFit,
		Padding: gogui.NoPadding,
		Spacing: gogui.SpacingPx(reach * 0.4),
		Content: []gogui.View{
			gogui.ColorPlane(gogui.ColorPlaneCfg{
				ID:            "custom-plane",
				Value:         v,
				Size:          size,
				MarkerSize:    reach * 0.55,
				Radius:        gogui.RadiusMedium,
				FocusDisabled: true,
				OnChange:      change,
			}),
			gogui.ColorChannelSlider(gogui.ColorChannelSliderCfg{
				ID:            "custom-hue",
				Channel:       gogui.ChannelHue,
				Value:         v,
				Vertical:      true,
				Height:        size,
				Width:         reach * 0.9,
				FocusDisabled: true,
				OnChange:      change,
			}),
		},
	})
}

func (e *customEditor) pick(field string) {
	e.flush()
	c := e.p.state().Screen.Custom.Theme().Color(field)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.field = field
	e.value = gogui.ColorToHSLA(gogui.RGB(c.R, c.G, c.B))
}

func (e *customEditor) current() (string, gogui.HSLA) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.field, e.value
}

func (e *customEditor) set(v gogui.HSLA) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.value = v
	if e.pending != nil {
		e.pending.Stop()
	}
	e.pending = time.AfterFunc(draftSettle, e.flush)
}

func (e *customEditor) flush() {
	e.mu.Lock()
	if e.pending == nil {
		e.mu.Unlock()
		return
	}
	e.pending.Stop()
	e.pending = nil
	name, hex := "custom."+e.field, hslaColor(e.value).String()
	e.mu.Unlock()
	if err := e.p.options.Screen.Set(name, hex); err != nil {
		slog.Warn("saving the custom theme failed", "setting", name, "err", err)
	}
}

func hslaColor(v gogui.HSLA) theme.Color {
	c := v.Color()
	return theme.Color{R: c.R, G: c.G, B: c.B}
}

type swatch theme.Color

func (c swatch) View(_ *gogui.Window, width, height float32) gogui.View {
	return gogui.Column(gogui.ContainerCfg{
		Width:   width,
		Height:  height,
		Sizing:  gogui.FixedFixed,
		Color:   widgets.Color(theme.Color(c)),
		Radius:  gogui.RadiusPx(height / 4),
		Padding: gogui.NoPadding,
	})
}
