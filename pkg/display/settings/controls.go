package settings

import (
	"log/slog"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/say"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
	zonepolicy "github.com/ygelfand/libcountertop/pkg/settings/timezone"
)

func (p *BasicPages) Poster() *shell.Page {
	return &shell.Page{
		Title: say.T("display.poster"),
		Build: func() ([]menu.Row, []func(int)) {
			cfg := p.options.Preferences.Poster.Read()
			return []menu.Row{
					{Label: say.T("poster.show"), Kind: menu.Toggle, On: cfg.Enabled},
					{Label: say.T("poster.next"), Dim: !cfg.Enabled},
					{
						Label: say.T("poster.every"),
						Kind:  menu.Chevron,
						Value: cfg.Every.Label(),
						Dim:   !cfg.Enabled,
					},
				}, []func(int){
					func(int) { p.options.Poster.SetEnabled(!cfg.Enabled) },
					func(int) { p.options.Poster.Next() },
					p.open(func() shell.View { return p.PosterEvery() }),
				}
		},
	}
}
func (p *BasicPages) PosterEvery() *shell.Page {
	return &shell.Page{
		Title: say.T("poster.every"),
		Build: func() ([]menu.Row, []func(int)) {
			now := p.options.Preferences.Poster.Read().Every
			var rows []menu.Row
			var acts []func(int)
			for _, e := range config.PosterEveries() {
				rows = append(rows, menu.Row{Label: e.Label(), Chosen: e == now})
				acts = append(acts, func(int) { p.options.Poster.SetEvery(e) })
			}
			return rows, acts
		},
	}
}
func PosterSays(p config.Poster) string {
	if !p.Enabled {
		return say.T("poster.off")
	}
	return p.Every.Label()
}
func (p *BasicPages) System() *shell.Page {
	return &shell.Page{Title: say.T("system.title"), Build: func() ([]menu.Row, []func(int)) {
		c := p.state()
		rows, acts := []menu.Row{
			{
				Glyph: gogui.IconTextSize,
				Label: say.T("settings.language"),
				Kind:  menu.Chevron,
				Value: say.Name(say.Chosen()),
			},
			{
				Glyph: gogui.IconClock,
				Label: say.T("datetime.title"),
				Kind:  menu.Chevron,
				Value: zonepolicy.Label(p.options.Zone.ChosenZone()),
			},
			{
				Glyph: gogui.IconKeyboard,
				Label: say.T("keyboard.size"),
				Kind:  menu.Chevron,
				Value: c.Screen.Keyboard.Label(),
			},
			{Glyph: gogui.IconSync, Label: say.T("settings.power"), Kind: menu.Chevron},
			{Glyph: gogui.IconExport, Label: say.T("settings.sync"), Kind: menu.Chevron},
		}, []func(int){
			p.open(func() shell.View { return p.Language() }),
			p.open(func() shell.View { return p.DateTime() }),
			p.open(func() shell.View { return p.Keyboard() }),
			p.open(p.options.PowerPage),
			p.open(p.options.SyncPage),
		}
		if p.options.Updates != nil {
			rows = append(rows, menu.Row{
				Glyph: gogui.IconDownload,
				Label: say.T("update.title"),
				Kind:  menu.Chevron,
				Value: UpdateSays(p.options.Updates.Offer()),
			})
			acts = append(acts, p.open(func() shell.View { return p.Update() }))
		}
		if p.options.Lock != nil {
			value := say.T("lock.off")
			if p.options.Lock.Locked() {
				value = say.T("lock.on")
			}
			rows = append(rows, menu.Row{
				Glyph: gogui.IconLock,
				Label: say.T("screen.setting.lock_pin"),
				Kind:  menu.Chevron,
				Value: value,
			})
			acts = append(acts, p.open(func() shell.View { return p.Lock() }))
		}
		if k := p.options.HomeKey; k != nil {
			row := menu.Row{
				Glyph: gogui.IconHome,
				Label: say.T("network.setting.home_key"),
				Kind:  menu.Password,
				Value: k.HomeKey(),
				Save: func(v string) {
					if err := k.SetHomeKey(v); err != nil {
						slog.Warn("saving the home key failed", "err", err)
					}
				},
			}
			rows = append(rows, row)
			acts = append(acts, nil)
		}
		return rows, acts
	}}
}

func (p *BasicPages) Lock() *shell.Page {
	l := p.options.Lock
	return &shell.Page{
		Title: say.T("screen.setting.lock_pin"),
		Build: func() ([]menu.Row, []func(int)) {
			if !l.Locked() {
				return []menu.Row{{Label: say.T("lock.set")}}, []func(int){func(int) { l.Change() }}
			}
			return []menu.Row{
					{Label: say.T("lock.change")},
					{Label: say.T("lock.clear"), Kind: menu.Chevron},
				}, []func(int){
					func(int) { l.Change() },
					p.open(func() shell.View {
						return ConfirmPage(say.T("lock.clear"), say.T("lock.clear.what"), func() {
							l.Clear()
							p.options.Shell.Pop()
						})
					}),
				}
		},
	}
}
func (p *BasicPages) DateTime() *shell.Page {
	return &shell.Page{Title: say.T("datetime.title"), Build: func() ([]menu.Row, []func(int)) {
		c := p.state()
		return []menu.Row{
				{
					Label: say.T("clock.zone"),
					Kind:  menu.Chevron,
					Value: zonepolicy.Label(p.options.Zone.ChosenZone()),
				},
				{
					Label: say.T("clock.hours"),
					Kind:  menu.Toggle,
					On:    c.Screen.Hours == config.TwentyFourHour,
				},
			}, []func(int){
				p.open(func() shell.View { return p.Zone() }),
				func(int) { p.toggleHours(c.Screen.Hours) },
			}
	}}
}
func (p *BasicPages) Keyboard() *shell.Page {
	return &shell.Page{Title: say.T("keyboard.size"), Build: func() ([]menu.Row, []func(int)) {
		now := p.state().Screen.Keyboard
		rows := []menu.Row{}
		acts := []func(int){}
		for _, k := range config.KeyboardSizes() {
			rows = append(rows, menu.Row{Label: k.Label(), Chosen: k == now})
			acts = append(acts, func(int) { p.setKeyboard(k) })
		}
		return rows, acts
	}}
}
