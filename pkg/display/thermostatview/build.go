package thermostatview

import (
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/dashboard"
	kit "github.com/ygelfand/libcountertop/pkg/display/panelkit"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/visual"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/thermostat"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const TabKind = "thermostat"

type look struct {
	draw     func(f *face) (gogui.View, skin)
	backdrop visual.Kind
}

var looks = map[schema.ThermostatLook]look{
	schema.ThermostatMinimal:   {draw: minimalLook},
	schema.ThermostatEditorial: {draw: editorialLook},
	schema.ThermostatPill:      {draw: pillLook},
	schema.ThermostatTop:       {draw: topLook, backdrop: visual.AmbientRipple},
	schema.ThermostatVault:     {draw: pipGreen, backdrop: visual.AmbientPip3},
	schema.ThermostatVaultNV:   {draw: pipAmber, backdrop: visual.AmbientPipNV},
	schema.ThermostatShrine:    {draw: shrineLook, backdrop: visual.AmbientShrine},
	schema.ThermostatHUD:       {draw: hudLook, backdrop: visual.AmbientRings},
	schema.ThermostatGrid:      {draw: gridLook, backdrop: visual.AmbientGrid},
	schema.ThermostatTARS:      {draw: tarsLook},
	schema.ThermostatHAL:       {draw: halLook, backdrop: visual.AmbientEye},
	schema.ThermostatMatrix:    {draw: matrixLook, backdrop: visual.AmbientMatrix},
	schema.ThermostatTarget:    {draw: targetLook, backdrop: visual.AmbientReticle},
	schema.ThermostatLab:       {draw: labLook},
	schema.ThermostatBrass:     {draw: brassLook},
}

func lookOf(l schema.ThermostatLook) look {
	if lk, ok := looks[l]; ok {
		return lk
	}
	return looks[schema.ThermostatEditorial]
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
			return []rect{{0, 0, hw, hh}, {hw + gap, 0, hw, hh}, {0, hh + gap, w, hh}}
		}
		return []rect{{0, 0, w, hh}, {0, hh + gap, hw, hh}, {hw + gap, hh + gap, hw, hh}}
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
	animated := lk.backdrop != "" && v.keep(&v.screen, lk.backdrop, ui.Rect{W: ww, H: wh})
	if lk.backdrop == "" {
		v.drop(&v.screen)
	}
	body, back := v.grid(w, rect{0, 0, vw, vh}, lk, animated, false)
	content := []gogui.View{body}
	content = append(content, v.corners(vw)...)
	if animated {
		back = gogui.RGBA(0, 0, 0, 0)
	}
	return gogui.Stack(gogui.ContainerCfg{
		ID:      "thermostat",
		Width:   vw,
		Height:  vh,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Color:   back,
		Clip:    true,
		Content: content,
	})
}

func (v *View) grid(
	w *gogui.Window,
	at rect,
	lk look,
	animated, board bool,
) (gogui.View, gogui.Color) {
	list := v.shown()
	if len(list) == 0 {
		list = []thermostat.State{{}}
	}
	gap := float32(0)
	if len(list) > 1 {
		gap = min(at.w, at.h) * 0.02
	}
	unit := v.o.Thermostats.Unit()
	now := time.Now()
	reserve := float32(0)
	if !board {
		reserve = float32(2*(widgets.CornerSide()+int(gogui.PadLarge)) + int(gogui.PadLarge))
	}
	var back gogui.Color
	views := make([]gogui.View, 0, len(list))
	for i, r := range tiles(len(list), at.w, at.h, gap) {
		f := &face{
			v: v, w: w, vw: r.w, vh: r.h, st: list[i], unit: unit, now: now, board: board,
		}
		if r.y == 0 && r.x+r.w >= at.w-1 {
			f.reserve = reserve
		}
		body, sk := lk.draw(f)
		if i == 0 {
			back = sk.back
		}
		fill := sk.back
		if animated {
			fill = gogui.RGBA(0, 0, 0, 0)
		}
		radius := float32(0)
		if len(list) > 1 {
			radius = min(r.w, r.h) * 0.04
		}
		views = append(views, r.place(gogui.Stack(gogui.ContainerCfg{
			Width: r.w, Height: r.h, Sizing: gogui.FixedFixed, Padding: gogui.NoPadding,
			Color: fill, Radius: gogui.RadiusPx(radius), Clip: true, Content: []gogui.View{body},
		})))
	}
	return kit.Stack(at.w, at.h, views...), back
}

func (v *View) corners(vw float32) []gogui.View {
	p := v.o.UI.Presses
	side, pad := widgets.CornerSide(), int(gogui.PadLarge)
	x := int(vw) - pad - side
	return []gogui.View{
		widgets.CloseCornerAt(p, "thermostat-close", x, func() { v.o.Shell.Remove(v.app) }),
		widgets.SettingsCornerAt(p, "thermostat-settings", x-pad-side,
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

func (v *View) Ground(_ dashboard.Tab, at ui.Rect) bool {
	lk := lookOf(v.look())
	if lk.backdrop == "" || !v.keep(&v.tab, lk.backdrop, at) {
		v.drop(&v.tab)
		return false
	}
	return true
}

func (v *View) grounded(lk look) bool {
	return lk.backdrop != "" && v.o.Backdrop != nil && v.o.Tabbed != nil
}

func (v *View) Board(w *gogui.Window, at ui.Rect, _ dashboard.Tab, _ theme.Theme) gogui.View {
	r := rect{0, 0, float32(at.W), float32(at.H)}
	lk := lookOf(v.look())
	animated := v.grounded(lk)
	body, back := v.grid(w, r, lk, animated, true)
	radius := gogui.RadiusMedium
	if animated {
		back, radius = gogui.RGBA(0, 0, 0, 0), gogui.RadiusPx(0)
	}
	return widgets.Placed(at, gogui.Stack(gogui.ContainerCfg{
		ID:      "thermostat-board",
		Width:   r.w,
		Height:  r.h,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Color:   back,
		Radius:  radius,
		Clip:    true,
		Content: []gogui.View{body},
	}))
}
