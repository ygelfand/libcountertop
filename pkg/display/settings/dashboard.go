package settings

import (
	"slices"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/say"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func (p *BasicPages) LocalRows() ([]menu.Row, []func(int)) {
	cfg := p.state()
	rows := []menu.Row{
		{
			Glyph: gogui.IconClock,
			Label: say.T("display.clock"),
			Kind:  menu.Chevron,
			Value: cfg.Clock.Face.Label(),
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
		p.open(func() shell.View { return p.Poster() }),
	}
	return rows, acts
}

func (p *BasicPages) WeatherRow() (menu.Row, func(int)) {
	weather := p.options.Preferences.IdleState().Weather
	return menu.Row{
		Glyph: gogui.IconSunnyO,
		Label: say.T("weather.title"),
		Kind:  menu.Chevron,
		Value: weatherShown(weather.Dashboard, weather.WeatherLayout),
	}, p.open(func() shell.View { return p.options.Idle.DashboardWeather() })
}

func (p *BasicPages) MediaRow() (menu.Row, func(int)) {
	media := menu.Steps(len(config.MediaDelays()))
	delay := p.state().Idle.Media
	return menu.Row{
			Label: say.T("idle.media"),
			Hint:  say.T("idle.media.hint"),
			Kind:  menu.Slider,
			Level: media.Level(slices.Index(config.MediaDelays(), delay)),
			Snap:  media.Snap,
			Value: delay.Label(),
		}, func(level int) {
			p.options.Media.SetIdle(config.MediaDelays()[media.Index(level)])
		}
}
