package thermostatview

import (
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/dashboard"
	kit "github.com/ygelfand/libcountertop/pkg/display/panelkit"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/thermostat"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const TabKind = "thermostat"

type look struct {
	id    schema.ThermostatLook
	back  gogui.Color
	sheet sheet
	draw  func(f *face)
}

var looks = map[schema.ThermostatLook]look{
	schema.ThermostatDial:     dialLook,
	schema.ThermostatColumn:   columnLook,
	schema.ThermostatWheel:    wheelLook,
	schema.ThermostatBars:     barsLook,
	schema.ThermostatSplit:    splitLook,
	schema.ThermostatTerminal: terminalLook,
	schema.ThermostatBrass:    brassLook,
	schema.ThermostatMission:  missionLook,
}

func lookOf(l schema.ThermostatLook) look {
	lk, ok := looks[l]
	if !ok {
		l, lk = schema.ThermostatDial, looks[schema.ThermostatDial]
	}
	lk.id = l
	return lk
}

func tiles(n int, w, h, gap float32) []rect {
	switch n {
	case 0, 1:
		return []rect{{0, 0, w, h}}
	case 2:
		if w >= h {
			hw := (w - gap) / 2
			return []rect{{0, 0, hw, h}, {hw + gap, 0, hw, h}}
		}
		hh := (h - gap) / 2
		return []rect{{0, 0, w, hh}, {0, hh + gap, w, hh}}
	case 3:
		hw, hh := (w-gap)/2, (h-gap)/2
		if w >= h {
			return []rect{{0, 0, hw, h}, {hw + gap, 0, hw, hh}, {hw + gap, hh + gap, hw, hh}}
		}
		th := (h - 2*gap) / 3
		return []rect{{0, 0, w, th}, {0, th + gap, w, th}, {0, 2 * (th + gap), w, th}}
	}
	hw, hh := (w-gap)/2, (h-gap)/2
	return []rect{
		{0, 0, hw, hh}, {hw + gap, 0, hw, hh},
		{0, hh + gap, hw, hh}, {hw + gap, hh + gap, hw, hh},
	}
}

func (v *View) build(w *gogui.Window) gogui.View {
	ww, wh := w.WindowSize()
	vw, vh := float32(ww), float32(wh)
	lk := lookOf(v.look())
	content := []gogui.View{v.grid(w, rect{0, 0, vw, vh}, lk, false)}
	content = append(content, v.corners(vw)...)
	if c := v.sheetView(w, vw, vh, lk); c != nil {
		content = append(content, c)
	}
	return gogui.Stack(gogui.ContainerCfg{
		ID:      "thermostat",
		Width:   vw,
		Height:  vh,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Color:   hex(0x0b0d0f),
		Clip:    true,
		Content: content,
	})
}

func (v *View) grid(w *gogui.Window, at rect, lk look, board bool) gogui.View {
	list := v.shown()
	if len(list) == 0 {
		list = []thermostat.State{{}}
	}
	gap := float32(0)
	if len(list) > 1 {
		gap = min(at.w, at.h) * 0.015
	}
	unit := v.o.Thermostats.Unit()
	now := time.Now()
	views := make([]gogui.View, 0, len(list))
	for i, r := range tiles(len(list), at.w, at.h, gap) {
		f := &face{
			v: v, w: w, vw: r.w, vh: r.h, st: list[i], unit: unit, now: now, board: board,
		}
		views = append(views, r.place(f.render(lk, len(list) > 1)))
	}
	return kit.Stack(at.w, at.h, views...)
}

func (f *face) render(lk look, rounded bool) gogui.View {
	f.frame()
	f.look = lk.id
	lk.draw(f)
	body := f.view(lk.back)
	if !rounded {
		return body
	}
	return gogui.Stack(gogui.ContainerCfg{
		Width: f.vw, Height: f.vh, Sizing: gogui.FixedFixed, Padding: gogui.NoPadding,
		Radius: gogui.RadiusPx(min(f.vw, f.vh) * 0.035), Clip: true, Content: []gogui.View{body},
	})
}

func (v *View) corners(vw float32) []gogui.View {
	p := v.o.UI.Presses
	side, pad := widgets.CornerSide(), int(gogui.PadLarge)
	x := int(vw) - pad - side
	level := v.o.Shell.Controls()
	if list := v.shown(); len(list) == 0 || !list[0].Available() {
		level = 1
	}
	return []gogui.View{
		widgets.CloseCornerAt(p, "thermostat-close", x, level, func() { v.o.Shell.Remove(v.app) }),
		widgets.SettingsCornerAt(p, "thermostat-settings", x-pad-side, level,
			func() { v.o.Shell.Push(v.Page()) }),
	}
}

func (v *View) DashboardTabs() []dashboard.Tab {
	_, rehearsing := v.Rehearsing()
	if cfg := v.o.Settings.Read(); !cfg.Tab || len(cfg.Entities) == 0 && !rehearsing {
		return nil
	}
	return []dashboard.Tab{{Kind: TabKind, Key: TabKind, Name: say.T("thermostat.app")}}
}

func (*View) Glyph(dashboard.Tab) string { return gogui.IconSunnyO }

func (v *View) Board(w *gogui.Window, at ui.Rect, _ dashboard.Tab, _ theme.Theme) gogui.View {
	r := rect{0, 0, float32(at.W), float32(at.H)}
	lk := lookOf(v.look())
	content := []gogui.View{v.grid(w, r, lk, true)}
	if c := v.sheetView(w, r.w, r.h, lk); c != nil {
		content = append(content, c)
	}
	return widgets.Placed(at, gogui.Stack(gogui.ContainerCfg{
		ID:      "thermostat-board",
		Width:   r.w,
		Height:  r.h,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Radius:  gogui.RadiusMedium,
		Clip:    true,
		Content: content,
	}))
}
