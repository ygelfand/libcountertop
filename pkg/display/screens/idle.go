package screens

import (
	"image"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/clockview"
	"github.com/ygelfand/libcountertop/pkg/display/dashboard"
	"github.com/ygelfand/libcountertop/pkg/display/idleview"
	"github.com/ygelfand/libcountertop/pkg/display/photoframe"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/visual"
	"github.com/ygelfand/libcountertop/pkg/display/visualview"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
)

func (a *App) idleScreen(v *idleview.View) *Screen {
	return &Screen{
		Clear: true,
		View:  v,
		Build: func(w *gogui.Window) gogui.View { return a.idle(w, v) },
	}
}

func (a *App) idle(w *gogui.Window, v *idleview.View) gogui.View {
	if a.o.Tessera != nil {
		a.o.Tessera.Rested()
	}
	vw, vh := w.WindowSize()
	wake := func(e gogui.EventCtx) {
		a.o.Shell.Remove(v)
		e.Consume()
		e.Window.InvalidateLayout()
	}
	night := v.Night()
	if f := a.o.Firmware; f != nil && f.On() && f.Rests() && a.o.Settings().Idle.Tessera() &&
		!night.Quiet() {
		v.Close()
		return gogui.Column(gogui.ContainerCfg{
			ID:      "idle",
			Sizing:  gogui.FillFill,
			Padding: gogui.NoPadding,
			Content: []gogui.View{
				widgets.Placed(ui.Rect{W: vw, H: vh}, f.Saver(ui.Rect{W: vw, H: vh}, wake)),
			},
		})
	}
	if a.o.Tessera != nil && a.o.Home.Tessera() && !a.o.Home.Firmware() &&
		a.o.Settings().Idle.Tessera() && !night.Quiet() {
		if layers := a.o.Tessera.Saver(w, ui.Rect{W: vw, H: vh}); layers != nil {
			v.Close()
			return gogui.Column(gogui.ContainerCfg{
				ID:      "idle",
				Sizing:  gogui.FillFill,
				Padding: gogui.NoPadding,
				Content: layers,
				OnClick: wake,
			})
		}
	}
	cfg := a.o.Settings()
	idle := idleview.Dark(cfg.Idle, cfg.Clock.Face, night)
	pal := a.ui.Palette()
	slots := v.Keep(vw, vh)
	areas := idleview.Areas(vw, vh, len(slots))
	spot := v.Spot(idle, time.Now())
	box := dashboard.Place(spot.At, spot.Align, idle.Size, vw, vh)

	whole := ui.Rect{W: vw, H: vh}
	var layers []gogui.View
	if len(slots) == 0 && (night.Quiet() || !a.paper(whole, box, pal.Background)) {
		layers = append(layers, widgets.Placed(whole, a.ground(pal.Background)))
	}
	for n, slot := range slots {
		if !slot.Photos() {
			continue
		}
		area := areas[n]
		look := photoframe.Look{
			Box:    image.Rect(box.X-area.X, box.Y-area.Y, box.X-area.X+box.W, box.Y-area.Y+box.H),
			Back:   pal.Background,
			Fit:    idle.PhotoFit,
			Change: idle.PhotoChange,
			Span:   idle.PhotoEvery.After(),
		}
		if !v.Photo(n, area, look) {
			layers = append(layers, widgets.Placed(area, a.ground(pal.Background)))
		}
	}
	if face, ok := clockview.Of(idle.Face); ok {
		r := idleview.Reading(
			idleview.Settings{Screen: cfg.Screen, Clock: cfg.Clock, Idle: idle},
			time.Now(),
		)
		tones := make([]visual.Traits, len(slots))
		for n, slot := range slots {
			tones[n] = visual.Kind(slot.Kind).Traits()
		}
		for _, p := range idleview.Pieces(pal, tones, areas, box) {
			clock := face.Draw(w, box, r, cfg.Clock.Ink.Over(p.Palette))
			if p.Clip.W == 0 {
				layers = append(layers, widgets.Placed(box, clock))
				continue
			}
			inner := ui.Rect{X: box.X - p.Clip.X, Y: box.Y - p.Clip.Y, W: box.W, H: box.H}
			layers = append(layers, widgets.Placed(p.Clip, gogui.Column(gogui.ContainerCfg{
				Sizing:  gogui.FillFill,
				Padding: gogui.NoPadding,
				Clip:    true,
				Content: []gogui.View{widgets.Placed(inner, clock)},
			})))
		}
	}
	if r, ok := a.o.Forecast.Now(); (ok || a.weather.Alerting()) && cfg.Weather.Idle &&
		!night.Quiet() {
		layers = append(layers, a.weather.Layer(w, r, cfg.Weather.SaverLayout(), vw, vh, 0, pal))
	}
	for n, slot := range slots {
		if slot.Photos() {
			layers = append(
				layers,
				photoframe.Taps(areas[n], func(forward bool) { v.Page(n, forward) })...)
		}
	}
	return gogui.Column(gogui.ContainerCfg{
		ID:      "idle",
		Sizing:  gogui.FillFill,
		Padding: gogui.NoPadding,
		Content: layers,
		OnClick: wake,
	})
}

func (a *App) visualScreen(v *visualview.View) *Screen {
	return &Screen{
		Clear: true,
		View:  v,
		Build: func(w *gogui.Window) gogui.View {
			vw, vh := w.WindowSize()
			v.Keep(vw, vh)
			return gogui.Column(gogui.ContainerCfg{
				ID:      "visual",
				Sizing:  gogui.FillFill,
				Padding: gogui.NoPadding,
				OnClick: func(e gogui.EventCtx) {
					e.Consume()
					if !v.Tap() {
						a.o.Shell.Remove(v)
					}
					e.Window.InvalidateLayout()
				},
			})
		},
	}
}

func (a *App) paper(area, box ui.Rect, bg theme.Color) bool {
	if a.wallpaper == nil || !a.o.Poster.Shows() {
		return false
	}
	look := a.o.Poster.Look(image.Rect(box.X, box.Y, box.X+box.W, box.Y+box.H), bg)
	if !a.wallpaper.Keep(area, look) {
		return false
	}
	a.papering = true
	a.papered.Store(true)
	return true
}

func (a *App) ground(bg theme.Color) gogui.View {
	return gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFill,
		Padding: gogui.NoPadding,
		Color:   widgets.Color(bg),
	})
}

func (a *App) seeThrough(on bool) {
	a.mu.Lock()
	r := a.r
	a.mu.Unlock()
	if r == nil {
		return
	}
	if on {
		r.SetClear(&gogui.ColorTransparent)
	} else {
		r.SetClear(nil)
	}
}
