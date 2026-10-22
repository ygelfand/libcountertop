package settings

import (
	"slices"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	ha "github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/say"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func (p *BasicPages) dashboardOffered() bool {
	return p.options.Home != nil && p.options.HA != nil &&
		p.options.Home.Enabled() && p.options.HA.Access() == ha.Allowed
}

func (p *BasicPages) dashboardMode() config.HomeMode {
	if !p.dashboardOffered() {
		return config.HomeNative
	}
	return p.options.Home.Speaks()
}

func (p *BasicPages) DashboardPage() *shell.Page {
	return &shell.Page{
		Title: say.T("display.dashboard"),
		Build: func() ([]menu.Row, []func(int)) {
			cfg := p.state()
			media := menu.Steps(len(config.MediaDelays()))
			weather := p.options.Preferences.IdleState().Weather
			mode := p.dashboardMode()
			rows := []menu.Row{
				{
					Glyph: gogui.IconClock,
					Label: say.T("display.clock"),
					Kind:  menu.Chevron,
					Value: cfg.Clock.Face.Label(),
				},
				{
					Glyph: gogui.IconSunnyO,
					Label: say.T("weather.title"),
					Kind:  menu.Chevron,
					Value: weatherShown(weather.Dashboard, weather.WeatherLayout),
				},
				{
					Glyph: gogui.IconPicture,
					Label: say.T("display.poster"),
					Kind:  menu.Chevron,
					Value: cfg.PosterLabel,
				},
			}
			acts := []func(int){
				p.open(func() shell.View { return p.options.Clocks.Page() }),
				p.open(func() shell.View { return p.options.Idle.DashboardWeather() }),
				p.open(func() shell.View { return p.Poster() }),
			}
			if p.dashboardOffered() && mode.Tessera() {
				for n := range rows {
					rows[n].Dim = true
					acts[n] = func(int) {}
				}
			}
			rows = append(rows, menu.Row{
				Label: say.T("idle.media"),
				Hint:  say.T("idle.media.hint"),
				Kind:  menu.Slider,
				Level: media.Level(slices.Index(config.MediaDelays(), cfg.Idle.Media)),
				Snap:  media.Snap,
				Value: cfg.Idle.Media.Label(),
			})
			acts = append(acts, func(level int) {
				p.options.Media.SetIdle(config.MediaDelays()[media.Index(level)])
			})
			if !p.dashboardOffered() {
				return rows, acts
			}
			return append(
					[]menu.Row{
						p.options.Marks.Row(say.T("display.dashboard"), config.HomeModes(), mode),
					},
					rows...),
				append(
					[]func(int){p.options.Marks.Pick(config.HomeModes(), p.options.Home.SetMode)},
					acts...)
		},
	}
}
