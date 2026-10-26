package doorbellview

import (
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/dashboard"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
)

const (
	TabKind  = "doorbell"
	tabFresh = 30 * time.Second
)

func (v *View) DashboardTabs() []dashboard.Tab {
	if !v.o.Settings.Read().Tab || len(v.o.Bell.Doors()) == 0 {
		return nil
	}
	return []dashboard.Tab{{Kind: TabKind, Key: TabKind, Name: say.T("doorbell.app")}}
}

func (*View) Glyph(dashboard.Tab) string { return gogui.IconBell }

func (v *View) Board(_ *gogui.Window, at ui.Rect, _ dashboard.Tab, _ theme.Theme) gogui.View {
	if now := time.Now().Unix(); now-v.boarded.Load() > int64(tabFresh/time.Second) {
		v.boarded.Store(now)
		safe.Go("doorbell tab pictures", v.freshen)
	}
	return widgets.Placed(at, v.page(v.doors(ui.Rect{W: at.W, H: at.H}, 0)))
}
