package settings

import (
	"image"
	"slices"
	"time"

	face "github.com/ygelfand/libcountertop/pkg/display/clockface"
	"github.com/ygelfand/libcountertop/pkg/display/dashboard"
	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/previews"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/visual"
	ha "github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/say"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type IdleState struct {
	Screen  config.Screen
	Clock   config.Clock
	Idle    config.Idle
	Visual  config.Visual
	Weather config.Weather
}
type IdleSlots interface {
	SetKind(int, string)
	SetSource(int, config.Source)
}
type IdleClock interface {
	SetAfter(config.Delay)
	SetFace(config.Face)
	SetSaver(config.HomeMode)
}
type IdleNight interface {
	SetNight(config.Night)
	SetNightLux(int)
	SetNightAfter(config.Delay)
}
type IdleActions interface {
	IdleClock
	IdleSlots
	IdleNight
	Placement
}
type WeatherCatalog interface {
	Fetch()
	Offered() ([]ha.Entity, error, bool)
}
type WeatherEntity interface {
	Title(string) string
	SetEntity(string)
}
type WeatherPlaces interface {
	SetDashboard(bool)
	SetIdle(bool)
	SetAlerts(bool)
	SetRadarSource(config.RadarSource)
	SetRadarRange(config.RadarRange)
}
type WeatherRendering interface {
	SetAnimate(bool)
	SetThemed(bool)
	SetLook(config.WeatherLook)
	Placement
}
type WeatherActions interface {
	WeatherCatalog
	WeatherEntity
	WeatherPlaces
	WeatherRendering
}
type MediaIdle interface{ SetIdle(config.Delay) }
type VisualLabel interface{ SetLabel(string) }
type HomeAccess interface{ Access() ha.Access }
type IdleDependencies struct {
	Preferences  Preferences
	Home         HomeAccess
	Marks        ModeMarks
	Sensors      func() *shell.Page
	Shell        *shell.Shell
	Idle         IdleActions
	Weather      WeatherActions
	SaverWeather WeatherRendering
	Visuals      VisualLabel
	Thumbnail    func(visual.Kind, int, int) *image.RGBA
	Previews     previews.Kit
}
type IdlePages struct{ options IdleDependencies }

func NewIdlePages(o IdleDependencies) *IdlePages { return &IdlePages{options: o} }

func (p *IdlePages) open(
	page shell.View,
) func(int) {
	return func(int) { p.options.Shell.Push(page) }
}
func (p *IdlePages) makePage(page func() shell.View) func(int) {
	return func(int) { p.options.Shell.Push(page()) }
}

func (p *IdlePages) visualPicker(
	title, empty string,
	none func(),
	chosen func() string,
	pick func(visual.Kind),
) *shell.Page {
	return VisualPicker(title, empty, none, chosen, pick, p.options.Thumbnail, p.options.Previews)
}
func IdleKindLabel(kind string) string {
	if kind == "" {
		return say.T("idle.none")
	}
	return visual.Kind(kind).Label()
}
func (p *IdlePages) Page() *shell.Page {
	return &shell.Page{
		Title: say.T("idle.title"),
		Build: func() ([]menu.Row, []func(int)) {
			c := p.options.Preferences.IdleState().Idle
			i := p.options.Idle
			rows, taps := []menu.Row{
				{Label: say.T("idle.after"), Kind: menu.Chevron, Value: c.After.Label()},
				{
					Label: say.T("idle.visual1"),
					Kind:  menu.Chevron,
					Value: IdleKindLabel(c.First.Kind),
				},
				{
					Label: say.T("idle.source1"),
					Kind:  menu.Chevron,
					Value: c.First.Source.Label(),
				},
				{
					Label: say.T("idle.visual2"),
					Kind:  menu.Chevron,
					Value: IdleKindLabel(c.Second.Kind),
				},
				{
					Label: say.T("idle.source2"),
					Kind:  menu.Chevron,
					Value: c.Second.Source.Label(),
				},
				{Label: say.T("idle.face"), Kind: menu.Chevron, Value: c.Face.Label()},
			}, []func(int){
				p.open(Choose(
					say.T("idle.after.title"),
					config.Delays(),
					func() config.Delay { return p.options.Preferences.IdleState().Idle.After },
					i.SetAfter,
					nil,
				)),
				p.makePage(func() shell.View { return p.Visual(0) }),
				p.makePage(func() shell.View { return p.Source(0) }),
				p.makePage(func() shell.View { return p.Visual(1) }),
				p.makePage(func() shell.View { return p.Source(1) }),
				p.open(Tiles(
					say.T("idle.face"),
					config.IdleFaces(),
					func() config.Face { return p.options.Preferences.IdleState().Idle.Face },
					i.SetFace,
					p.options.Previews.ClockFace,
				)),
			}
			placed, placing := Placing{
				Positions: config.IdlePositions(),
				Aligns:    config.IdleAligns(),
				Read: func() config.Placement {
					return p.options.Preferences.IdleState().Idle.Placement
				},
				Set:     i,
				Preview: p.Placed,
			}.Rows(p.options.Shell)
			rows, taps = append(rows, placed...), append(taps, placing...)
			w := p.options.Preferences.IdleState().Weather
			rows = append(rows,
				menu.Row{
					Label: say.T("weather.title"),
					Kind:  menu.Chevron,
					Value: weatherShown(w.Idle, w.SaverLayout()),
				},
				menu.Row{
					Label: say.T("idle.label"),
					Kind:  menu.Field,
					Value: p.options.Preferences.IdleState().Visual.Label,
					Save:  p.options.Visuals.SetLabel,
				},
			)
			taps = append(taps, p.makePage(func() shell.View { return p.SaverWeather() }), nil)
			rows, taps = p.withSaver(c, rows, taps)
			return append(rows, menu.Row{
					Label: say.T("idle.night"),
					Hint:  say.T("idle.night.hint"),
					Kind:  menu.Chevron,
					Value: c.Night.Label(),
				}),
				append(taps, p.makePage(func() shell.View { return p.Night() }))
		},
	}
}

func (p *IdlePages) Night() *shell.Page {
	return &shell.Page{
		Title: say.T("idle.night"),
		Build: func() ([]menu.Row, []func(int)) {
			c := p.options.Preferences.IdleState().Idle
			i := p.options.Idle
			delays := config.NightDelays()
			wait := menu.Steps(len(delays))
			rows := []menu.Row{
				{Label: say.T("idle.night.mode"), Kind: menu.Chevron, Value: c.Night.Label()},
				{
					Label: say.T("idle.night.lux"),
					Kind:  menu.Slider,
					Level: c.NightLux,
					Value: say.F("idle.night.lux.value", map[string]any{"N": c.NightLux}),
					Dim:   !c.Night.On(),
				},
				{
					Label: say.T("idle.night.after"),
					Kind:  menu.Slider,
					Level: wait.Level(max(slices.Index(delays, c.NightAfter), 0)),
					Snap:  wait.Snap,
					Value: c.NightAfter.Label(),
					Dim:   !c.Night.On(),
				},
			}
			taps := []func(int){
				p.open(Choose(
					say.T("idle.night.mode"),
					config.Nights(),
					func() config.Night { return p.options.Preferences.IdleState().Idle.Night },
					i.SetNight,
					nil,
				)),
				i.SetNightLux,
				func(level int) { i.SetNightAfter(delays[wait.Index(level)]) },
			}
			if !c.Night.On() {
				taps[1], taps[2] = func(int) {}, func(int) {}
			}
			return rows, taps
		},
	}
}

const saverShared = 1

func (p *IdlePages) withSaver(
	c config.Idle,
	rows []menu.Row,
	taps []func(int),
) ([]menu.Row, []func(int)) {
	head, headTaps := slices.Clone(rows[:saverShared]), slices.Clone(taps[:saverShared])
	rest, restTaps := rows[saverShared:], taps[saverShared:]
	if p.options.Sensors != nil {
		r, t := p.options.Sensors().Build()
		head, headTaps = append(head, r...), append(headTaps, t...)
	}
	if p.options.Home != nil && p.options.Home.Access() == ha.Allowed {
		mode := p.options.Marks.Row(say.T("idle.saver"), config.SaverModes(), c.Saver)
		if c.Tessera() {
			mode.Hint = say.T("idle.saver.tessera")
			for n := range rest {
				rest[n].Dim = true
				restTaps[n] = func(int) {}
			}
		}
		head = append(head, mode)
		headTaps = append(
			headTaps,
			p.options.Marks.Pick(config.SaverModes(), p.options.Idle.SetSaver),
		)
	}
	return append(head, rest...), append(headTaps, restTaps...)
}

func (p *IdlePages) SlotVisual(slot int) config.IdleVisual {
	c := p.options.Preferences.IdleState().Idle
	if slot == 0 {
		return c.First
	}
	return c.Second
}

func (p *IdlePages) Visual(slot int) *shell.Page {
	title := say.T("idle.visual1")
	if slot == 1 {
		title = say.T("idle.visual2")
	}
	return p.visualPicker(title, say.T("idle.none"),
		func() { p.options.Idle.SetKind(slot, "") },
		func() string { return p.SlotVisual(slot).Kind },
		func(k visual.Kind) { p.options.Idle.SetKind(slot, string(k)) })
}

func (p *IdlePages) Source(slot int) *shell.Page {
	title := say.T("idle.source1")
	if slot == 1 {
		title = say.T("idle.source2")
	}
	return Choose(title, config.Sources(),
		func() config.Source { return p.SlotVisual(slot).Source },
		func(s config.Source) { p.options.Idle.SetSource(slot, s) }, nil)
}

func (p *IdlePages) Placed(at config.Placement) func(ui.Surface, ui.Rect, theme.Theme) {
	return func(s ui.Surface, box ui.Rect, palette theme.Theme) {
		cfg := p.options.Preferences.IdleState()
		if cfg.Idle.Face == config.FaceNone {
			return
		}
		r := face.Read(time.Now(), cfg.Screen.Hours == config.TwentyFourHour)
		within := dashboard.Place(at.Position, at.Align, at.Size, box.W, box.H)
		within.X += box.X
		within.Y += box.Y
		face.Of(cfg.Idle.Face).Draw(s, within, r.Undated(), cfg.Clock.Ink.Over(palette))
	}
}

func weatherShown(on bool, l config.WeatherLayout) string {
	if !on {
		return say.T("weather.off")
	}
	return l.Look.Label()
}

type weatherSpot struct {
	shown  string
	on     func(config.Weather) bool
	show   func(bool)
	layout func(config.Weather) config.WeatherLayout
	set    WeatherRendering
}

func (p *IdlePages) DashboardWeather() *shell.Page {
	return p.weatherPage(weatherSpot{
		shown:  say.T("weather.dashboard"),
		on:     func(w config.Weather) bool { return w.Dashboard },
		show:   p.options.Weather.SetDashboard,
		layout: func(w config.Weather) config.WeatherLayout { return w.WeatherLayout },
		set:    p.options.Weather,
	})
}

func (p *IdlePages) SaverWeather() *shell.Page {
	return p.weatherPage(weatherSpot{
		shown:  say.T("weather.idle"),
		on:     func(w config.Weather) bool { return w.Idle },
		show:   p.options.Weather.SetIdle,
		layout: config.Weather.SaverLayout,
		set:    p.options.SaverWeather,
	})
}

func (p *IdlePages) weatherPage(at weatherSpot) *shell.Page {
	read := func() config.WeatherLayout {
		return at.layout(p.options.Preferences.IdleState().Weather)
	}
	return &shell.Page{
		Title: say.T("weather.title"),
		Build: func() ([]menu.Row, []func(int)) {
			w := p.options.Preferences.IdleState().Weather
			on, l := at.on(w), at.layout(w)
			ranges := config.RadarRanges()
			steps := menu.Steps(len(ranges))
			rows := []menu.Row{
				{Label: at.shown, Kind: menu.Toggle, On: on},
				{Label: say.T("weather.look"), Kind: menu.Chevron, Value: l.Look.Label()},
				{Label: say.T("weather.animate"), Kind: menu.Toggle, On: l.Animate},
				{
					Label: say.T("weather.setting.alerts"),
					Hint:  say.T("weather.alerts.hint"),
					Kind:  menu.Toggle,
					On:    w.Alerts,
				},
				SegmentRow(
					say.T("weather.setting.radar_source"),
					config.RadarSources(),
					w.RadarSource,
				),
				{
					Label: say.T("weather.setting.radar_range"),
					Kind:  menu.Slider,
					Level: steps.Level(max(slices.Index(ranges, w.RadarRange), 0)),
					Snap:  steps.Snap,
					Value: w.RadarRange.Label(),
				},
				{
					Label: say.T("weather.themed"),
					Hint:  say.T("weather.themed.hint"),
					Kind:  menu.Toggle,
					On:    l.Themed,
				},
			}
			taps := []func(int){
				func(int) { at.show(!on) },
				p.makePage(func() shell.View { return p.WeatherLooks(read, at.set) }),
				func(int) { at.set.SetAnimate(!l.Animate) },
				func(int) { p.options.Weather.SetAlerts(!w.Alerts) },
				SegmentPick(config.RadarSources(), p.options.Weather.SetRadarSource),
				func(level int) { p.options.Weather.SetRadarRange(ranges[steps.Index(level)]) },
				func(int) { at.set.SetThemed(!l.Themed) },
			}
			placed, placing := Placing{
				Positions: config.Positions(),
				Aligns:    config.Aligns(),
				Read:      func() config.Placement { return read().Placement },
				Set:       at.set,
			}.Rows(p.options.Shell)
			return append(rows, placed...), append(taps, placing...)
		},
	}
}

func (p *IdlePages) WeatherEntities() *shell.Page {
	p.options.Weather.Fetch()
	return &shell.Page{
		Title: say.T("weather.entity"),
		Build: func() ([]menu.Row, []func(int)) {
			list, err, fetched := p.options.Weather.Offered()
			switch {
			case !fetched:
				return []menu.Row{{Label: say.T("home.loading")}}, []func(int){nil}
			case err != nil:
				return []menu.Row{
						{Label: say.T("home.failed"), Hint: err.Error()},
						{Label: say.T("home.retry")},
					},
					[]func(int){
						nil,
						func(int) { p.options.Weather.Fetch() },
					}
			case len(list) == 0:
				return []menu.Row{{Label: say.T("weather.none")}}, []func(int){nil}
			}
			now := p.options.Preferences.IdleState().Weather.Entity
			rows := make([]menu.Row, 0, len(list))
			taps := make([]func(int), 0, len(list))
			for _, e := range list {
				rows = append(rows, menu.Row{Label: e.Name, Hint: e.ID, Chosen: e.ID == now})
				taps = append(taps, func(int) { p.options.Weather.SetEntity(e.ID) })
			}
			return rows, taps
		},
	}
}

func (p *IdlePages) WeatherLooks(
	read func() config.WeatherLayout,
	set WeatherRendering,
) *shell.Page {
	return &shell.Page{
		Title: say.T("weather.look"),
		Tiles: func() ([]menu.Cell, []func(int)) {
			now := read()
			var cells []menu.Cell
			var taps []func(int)
			for _, look := range config.WeatherLooks() {
				sample := now
				sample.Look = look
				cells = append(cells, menu.Cell{
					Label:   look.Label(),
					Chosen:  look == now.Look,
					Preview: p.options.Previews.Weather(sample),
				})
				taps = append(taps, func(int) { set.SetLook(look) })
			}
			return cells, taps
		},
	}
}
