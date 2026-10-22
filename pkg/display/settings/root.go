package settings

import (
	"fmt"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/audio/volume"
	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/homecontrol"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

type Section struct {
	Label, Glyph string
	Page         func() shell.View
}
type NetworkName interface{ Network() string }
type RootDependencies struct {
	Network                                          NetworkName
	Screen                                           storage.Section[schema.Screen]
	Volume                                           volume.Channel
	Basic                                            *BasicPages
	Assistants                                       *AssistantPages
	Home                                             *homecontrol.Engine
	Version                                          string
	Shell                                            *shell.Shell
	NetworkPage, FeaturesPage, VolumePage, AboutPage func() shell.View
	Sections                                         func() []Section
}

func RootPage(o RootDependencies) *shell.Page {
	return &shell.Page{Title: say.T("settings.title"), Build: func() ([]menu.Row, []func(int)) {
		rows := []menu.Row{}
		acts := []func(int){}
		add := func(glyph, label, value string, page func() shell.View) {
			rows = append(
				rows,
				menu.Row{Glyph: glyph, Label: say.T(label), Kind: menu.Chevron, Value: value},
			)
			acts = append(acts, func(int) { o.Shell.Push(page()) })
		}
		add(gogui.IconGlobe, "settings.network", o.Network.Network(), o.NetworkPage)
		add(gogui.IconPlug, "settings.features", "", o.FeaturesPage)
		add(
			gogui.IconSunnyO,
			"settings.display",
			ThemeSays(o.Screen.Read().Theme),
			func() shell.View { return o.Basic.Display() },
		)
		add(
			gogui.IconTablet,
			"dashboard.title",
			"",
			func() shell.View { return o.Home.Dashboard() },
		)
		add(gogui.IconHome, "home.title", "", func() shell.View { return o.Home.Page() })
		add(
			gogui.IconSpeaker,
			"settings.volume",
			fmt.Sprintf("%d%%", o.Volume.Level()),
			o.VolumePage,
		)
		add(gogui.IconBell, "alerts.page", "", func() shell.View { return o.Basic.AlertsPage() })
		add(
			gogui.IconComment,
			"settings.assistants",
			o.Assistants.Phrases(),
			func() shell.View { return o.Assistants.Page() },
		)
		if o.Sections != nil {
			for _, s := range o.Sections() {
				add(s.Glyph, s.Label, "", s.Page)
			}
		}
		add(
			gogui.IconGear,
			"settings.system",
			o.Basic.SystemSays(),
			func() shell.View { return o.Basic.System() },
		)
		add(gogui.IconCode, "settings.debug", "", func() shell.View { return o.Basic.Debug() })
		add(gogui.IconInfo, "settings.about", o.Version, o.AboutPage)
		return rows, acts
	}}
}
