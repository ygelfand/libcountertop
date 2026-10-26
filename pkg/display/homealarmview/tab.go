package homealarmview

import (
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/dashboard"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/say"
)

const TabKind = "homealarm"

func (v *View) DashboardTabs() []dashboard.Tab {
	if cfg := v.o.Settings.Read(); !cfg.Tab || cfg.Entity == "" {
		return nil
	}
	return []dashboard.Tab{{Kind: TabKind, Key: TabKind, Name: say.T("homealarm.app")}}
}

func (*View) Glyph(dashboard.Tab) string { return gogui.IconShield }

func (v *View) Board(w *gogui.Window, at ui.Rect, _ dashboard.Tab, _ theme.Theme) gogui.View {
	f := v.face(w, false)
	f.vw, f.vh, f.board = float32(at.W), float32(at.H), true
	draw, ok := looks[v.look()]
	if !ok {
		draw = keypadLook
	}
	body, sk := draw(f)
	content := []gogui.View{body}
	if f.asking {
		content = append(content, f.sheet(sk))
	}
	return widgets.Placed(at, gogui.Stack(gogui.ContainerCfg{
		ID:      "homealarm-board",
		Width:   f.vw,
		Height:  f.vh,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Color:   sk.back,
		Radius:  gogui.RadiusMedium,
		Clip:    true,
		Content: content,
	}))
}
