package screens

import (
	"fmt"
	"image"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/icons"
	"github.com/ygelfand/libcountertop/pkg/display/idleview"
	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/videopage"
	"github.com/ygelfand/libcountertop/pkg/display/visualview"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/homecontrol"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func (a *App) screenFor(v shell.View) *Screen {
	switch t := v.(type) {
	case *idleview.View:
		return a.idleScreen(t)
	case *visualview.View:
		return a.visualScreen(t)
	case *videopage.Page:
		return a.videoScreen(t)
	case *shell.Page:
		return a.pageScreen(t)
	case *homecontrol.Home:
		return a.homeScreen(t)
	case *homecontrol.Dashboard:
		return a.dashboardScreen(t)
	case *homecontrol.Picker:
		return a.pickerScreen(t)
	case *homecontrol.Full:
		return a.fullScreen(t)
	}
	for _, f := range a.o.Views {
		if s := f(v); s != nil {
			return s
		}
	}
	switch v {
	case a.o.PlayerPage():
		return &Screen{Title: say.T("player.now"), View: v, Fixed: true, Build: a.player.Body}
	case a.o.Volume.Card():
		return &Screen{View: v, Build: a.volumeCard}
	}
	return nil
}

func (a *App) pageScreen(p *shell.Page) *Screen {
	s := &Screen{
		Title: p.Title,
		View:  p,
		Build: func(w *gogui.Window) gogui.View {
			return widgets.PageBody(w, p, widgets.PageRenderer{
				Row: a.ui.Row, Tile: a.tileView, Preview: a.livePreview, Beside: widgets.Beside,
			})
		},
	}
	if len(p.Actions) > 0 {
		s.Foot = func(w *gogui.Window) gogui.View {
			return a.ui.Toolkit.ActionBar(w, p.Actions, a.ui.Palette())
		}
	}
	return s
}

func (a *App) videoScreen(p *videopage.Page) *Screen {
	return &Screen{
		Clear: true,
		View:  p,
		Fixed: true,
		Build: func(w *gogui.Window) gogui.View { return a.player.Video(w, p) },
	}
}

func (a *App) homeScreen(h *homecontrol.Home) *Screen {
	return &Screen{Title: say.T("home.title"), View: h, Build: a.home.Body}
}

func (a *App) dashboardScreen(d *homecontrol.Dashboard) *Screen {
	return &Screen{Title: say.T("dashboard.title"), View: d, Build: a.home.Dashboard}
}

func (a *App) pickerScreen(p *homecontrol.Picker) *Screen {
	return &Screen{
		Title: p.Sel.Name(),
		View:  p,
		Build: func(*gogui.Window) gogui.View { return a.home.Picker(p) },
	}
}

type controlsBelow interface{ ControlsBelow() bool }

func (a *App) rail(w *gogui.Window, covering *Screen) gogui.View {
	var entries []widgets.RailEntry
	for _, e := range a.o.Drawer.Entries() {
		glyph := icons.Glyph(icons.Named(e.Icon))
		entries = append(entries, widgets.RailEntry{Label: e.Label(), Glyph: glyph, Open: e.Open})
	}
	_, pinned := a.o.Shell.Dock()
	var close func()
	if !pinned {
		close = a.o.Shell.HideDock
	}
	edge := a.o.Settings().Screen.Drawer
	if covering != nil && pinned && edge == schema.EdgeBottom {
		if c, ok := covering.View.(controlsBelow); ok && c.ControlsBelow() {
			edge = schema.EdgeTop
		}
	}
	return a.ui.Toolkit.Rail(
		w,
		widgets.RailOptions{
			Entries: entries,
			Edge:    edge,
			Close:   close,
		},
	)
}

func (a *App) volumeCard(w *gogui.Window) gogui.View {
	vol := a.o.Volume
	stream, open := vol.Picked()
	var choices []widgets.VolumeChoice
	for _, s := range a.o.Streams {
		choices = append(
			choices,
			widgets.VolumeChoice{Key: string(s), Label: s.Label(), Level: vol.Level(s)},
		)
	}
	return a.ui.Toolkit.VolumeCard(w, widgets.VolumeOptions{
		Current: widgets.VolumeChoice{
			Key:   string(stream),
			Label: stream.Label(),
			Level: vol.Level(stream),
		},
		Choices: choices,
		Open:    open,
		Left:    a.o.Settings().Screen.Volume == schema.EdgeLeft,
		Set:     func(s string, v int) { vol.Set(schema.Stream(s), v) },
		Linger:  vol.Linger,
		Expand:  vol.Expand,
		Dismiss: vol.Dismiss,
		Grip:    a.ui.Grip,
	})
}

func (a *App) keyboard(w *gogui.Window) gogui.View {
	a.mu.Lock()
	r := a.r
	a.mu.Unlock()
	if r == nil {
		return nil
	}
	at, view := a.ui.Editor.Keyboard(w, widgets.KeyboardOptions{
		Size:    widgets.KeyboardSize(a.o.Settings().Screen.Keyboard),
		Palette: a.ui.Palette(),
		Kit:     a.ui.Kit(),
		Cancel:  say.T("keyboard.cancel"),
		Save:    say.T("keyboard.save"),
		Type:    r.Type,
		Press:   r.Press,
	})
	return widgets.Placed(at, view)
}

func (a *App) upgradeCard(w *gogui.Window) gogui.View {
	up := a.o.Upgrade()
	if !up.Active {
		return nil
	}
	img := a.logo()
	return widgets.UpgradeCard(
		w,
		up,
		a.ui.Palette(),
		widgets.ImageSource(fmt.Sprintf("logo/%p", img), img),
	)
}

func (a *App) upgradeCovers() bool { return a.o.Upgrade().Active }

func (a *App) logo() image.Image {
	if theme.Dark(a.ui.Palette().Background) {
		return a.o.Night()
	}
	return a.o.Logo()
}

func reach() float32 { return style.Reach() }

func (a *App) livePreview(w *gogui.Window, watch func(ui.Rect) shell.Feed) gogui.View {
	return a.preview.View(w, watch)
}

func (a *App) tileView(w *gogui.Window, id, page string, c menu.Cell, tap func(int)) gogui.View {
	t := gogui.CurrentTheme().Cfg
	tw := int(t.SizeTextMedium * 8)
	th := int(float32(tw) * 0.72)
	var content []gogui.View
	if c.Preview != nil {
		content = append(content, c.Preview.View(w, float32(tw), float32(th)))
	}
	return a.ui.Toolkit.Tile(id, c.Label, c.Chosen, content, tap, c.Edit)
}

func (a *App) painted(key string, w, h int, fill theme.Color, draw widgets.Paint) string {
	return widgets.Painted(key, w, h, fill, a.ui.Palette(), draw)
}
