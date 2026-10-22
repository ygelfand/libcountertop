package screens

import (
	"image"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	face "github.com/ygelfand/libcountertop/pkg/display/clockface"
	"github.com/ygelfand/libcountertop/pkg/display/clockview"
	"github.com/ygelfand/libcountertop/pkg/display/dashboard"
	"github.com/ygelfand/libcountertop/pkg/display/navigation"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func (a *App) root(w *gogui.Window) gogui.View {
	covering, above := a.nav.Showing(a.o.Shell.Views())
	overlays := a.nav.Overlays()
	if top := navigation.Covering(overlays); top >= 0 {
		for _, o := range overlays[:top] {
			if o.Covered != nil {
				o.Covered()
			}
		}
		a.seeThrough(false)
		pause(covering)
		return stack(overlaid(w, []gogui.View{blank()}, overlays[top:]))
	}
	if a.fullLook() || a.o.Stage.StagedFull() {
		a.seeThrough(true)
		return stack(overlaid(w, []gogui.View{blank()}, overlays))
	}
	a.seeThrough(covering != nil && covering.Clear)
	layers := []gogui.View{a.base(w, covering)}
	if shown, _ := a.o.Shell.Dock(); shown && covering == nil && a.o.Offering() == "" {
		layers = append(layers, widgets.Floating(0, a.rail(w)))
	}
	for _, s := range above {
		layers = append(layers, widgets.Floating(0, s.Build(w)))
	}
	layers = overlaid(w, layers, overlays)
	if a.ui.Editor.Typing(w) {
		if v := a.keyboard(w); v != nil {
			layers = append(
				layers,
				gogui.Column(
					gogui.ContainerCfg{
						Float:       true,
						FloatZIndex: 1001,
						Sizing:      gogui.FillFill,
						Padding:     gogui.NoPadding,
						Content:     []gogui.View{v},
					},
				),
			)
		}
	}
	return stack(layers)
}

func (a *App) base(w *gogui.Window, covering *Screen) gogui.View {
	switch {
	case covering == nil:
	case covering.Title == "":
		return covering.Build(w)
	default:
		return a.page(w, covering)
	}
	if addr := a.o.Offering(); addr != "" {
		return a.onboard(w, addr)
	}
	return a.dashboard(w)
}

func overlaid(
	w *gogui.Window,
	layers []gogui.View,
	overlays []*navigation.Overlay,
) []gogui.View {
	for _, o := range overlays {
		if v := o.Build(w); v != nil {
			layers = append(layers, widgets.Floating(o.Priority+1, v))
		}
	}
	return layers
}

func (a *App) covered() bool { return navigation.Covering(a.nav.Overlays()) >= 0 }

func pause(s *Screen) {
	if s == nil || !s.Clear {
		return
	}
	if c, ok := s.View.(interface{ Close() }); ok {
		c.Close()
	}
}

func blank() gogui.View {
	return gogui.Column(gogui.ContainerCfg{Sizing: gogui.FillFill, Padding: gogui.NoPadding})
}

func stack(layers []gogui.View) gogui.View {
	return gogui.Column(
		gogui.ContainerCfg{Sizing: gogui.FillFill, Padding: gogui.NoPadding, Content: layers},
	)
}

func (a *App) dashboard(w *gogui.Window) gogui.View {
	vw, vh := w.WindowSize()
	pal := a.ui.Palette()
	margin := int(reach() * 0.5)

	var layers []gogui.View
	switch {
	case a.o.Firmware != nil && a.o.Firmware.On():
		whole := ui.Rect{W: vw, H: vh}
		layers = []gogui.View{widgets.Placed(whole, a.o.Firmware.Draw(whole))}
	case a.o.Home != nil && a.o.Home.Tessera():
		layers = a.tesseraDash(w, vw, vh, 0, margin)
	default:
		tabs := a.o.Tabs.List()
		strip := ui.Rect{X: margin, Y: margin, W: vw - 2*margin, H: int(reach() * 2)}
		top := 0
		if len(tabs) > 0 {
			top = strip.Y + strip.H
		}
		layers = a.nativeDash(w, vw, vh, top, margin, pal)
		if len(tabs) > 0 {
			showing, _ := a.o.Tabs.Showing()
			layers = append(layers, a.tabStrip(strip, tabs, showing.Key))
		}
	}

	dash := gogui.Column(gogui.ContainerCfg{
		ID:      "dashboard",
		Sizing:  gogui.FillFill,
		Padding: gogui.NoPadding,
		Content: layers,
	})
	return gogui.Column(
		gogui.ContainerCfg{
			Sizing:  gogui.FillFill,
			Padding: gogui.NoPadding,
			Color:   widgets.Color(pal.Background),
			Content: []gogui.View{dash},
		},
	)
}

func (a *App) tesseraDash(w *gogui.Window, vw, vh, top, margin int) []gogui.View {
	if a.o.Tessera == nil {
		return nil
	}
	inset := ui.Rect{X: margin, Y: top + margin, W: vw - 2*margin, H: vh - top - 2*margin}
	if n := a.o.Tessera.Notice(inset); n != nil {
		return []gogui.View{widgets.Placed(inset, n)}
	}
	return a.o.Tessera.Draw(w, ui.Rect{Y: top, W: vw, H: vh - top})
}

func (a *App) nativeDash(w *gogui.Window, vw, vh, top, margin int, pal theme.Theme) []gogui.View {
	cfg := a.o.Settings()
	ink := cfg.Clock.Ink.Over(pal)
	r := face.Read(time.Now(), cfg.Screen.Hours == schema.TwentyFourHour)
	if !cfg.Clock.Date {
		r = r.Undated()
	}
	box := dashboard.Place(cfg.Clock.Position, cfg.Clock.Align, cfg.Clock.Size, vw, vh)
	if top > box.Y {
		shift := top - box.Y
		box.Y += shift
		box.H = max(box.H-shift, 1)
	}
	showing, tabbed := a.o.Tabs.Showing()

	var layers []gogui.View
	if backdrop, behind := a.o.Poster.
		Backdrop(
			vw,
			vh,
			image.Rect(box.X, box.Y, box.X+box.W, box.Y+box.H),
			pal.Background,
		); backdrop != nil {
		layers = append(
			layers,
			widgets.Placed(
				ui.Rect{W: vw, H: vh},
				widgets.Picture(widgets.ImageSource("poster/"+behind, backdrop), vw, vh),
			),
		)
	}
	if kind, drawn := a.boards[showing.Kind]; tabbed && drawn {
		below := top + margin
		return append(layers, kind.Draw(
			w,
			ui.Rect{X: margin, Y: below, W: vw - 2*margin, H: vh - below - margin},
			showing,
			pal,
		))
	}
	if build, ok := clockview.Of(cfg.Clock.Face); ok {
		layers = append(layers, widgets.Placed(box, build(w, box, r, ink)))
	}
	if r, ok := a.o.Forecast.Now(); (ok || a.weather.Alerting()) && cfg.Weather.Dashboard {
		layers = append(
			layers,
			a.weather.Layer(w, r, cfg.Weather.WeatherLayout, vw, vh, float32(top), pal),
		)
	}
	return layers
}

func (a *App) page(w *gogui.Window, p *Screen) gogui.View {
	return a.ui.Toolkit.PageFrame(
		w,
		widgets.FrameOptions{
			Title:   p.Title,
			Fixed:   p.Fixed,
			Build:   p.Build,
			Foot:    p.Foot,
			Back:    func() { a.o.Shell.Pop() },
			Gesture: a.ui.HoldStill,
			Room:    a.ui.Editor.Room(w),
		},
	)
}
