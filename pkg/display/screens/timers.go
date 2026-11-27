package screens

import (
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/idleview"
	"github.com/ygelfand/libcountertop/pkg/display/timerview"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func (a *App) timer(w *gogui.Window) gogui.View {
	c := a.o.Timers.Now()
	if !c.Showing && !timerview.Covers(c, a.o.Settings().Screen.Timer) {
		return nil
	}
	return a.timers.Build(w, c, a.o.Settings().Screen.Timer, a.timerScene(w))
}

func (a *App) timerCovers() bool {
	return timerview.Covers(a.o.Timers.Now(), a.o.Settings().Screen.Timer)
}

func (a *App) timerScene(w *gogui.Window) timerview.Scene {
	cfg := a.o.Settings()
	vw, vh := w.WindowSize()
	clear := markSize(vw, vh) * 1.3
	marks := a.o.Marks()
	scene := timerview.Scene{Clock: cfg.Clock, WeatherTopRight: a.weatherTopRight()}
	if marks.Mic {
		scene.ClearLeft = clear
	}
	if marks.Camera {
		scene.ClearRight = clear
	}
	return scene
}

func (a *App) weatherTopRight() bool {
	cfg := a.o.Settings().Weather
	at, shown := cfg.Placement, cfg.Dashboard && a.o.Offering() == ""
	if covering, _ := a.nav.Showing(a.o.Shell.Views()); covering != nil {
		at, shown = schema.Placement{}, false
		if _, idle := covering.View.(*idleview.View); idle {
			at, shown = cfg.SaverLayout().Placement, cfg.Idle
		}
	}
	return shown && at.TopRight()
}
