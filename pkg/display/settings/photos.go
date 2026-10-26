package settings

import (
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/media/photos"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type PhotoSources interface {
	Settings() *photos.Controls
	State(key string) photos.State
	Providers() []photos.Provider
}

func (p *BasicPages) Photos() *shell.Page {
	return &shell.Page{
		Title: say.T("photos.page"),
		Build: func() ([]menu.Row, []func(int)) {
			cfg := p.options.Preferences.Poster.Read()
			everies := config.PosterEveries()
			steps := menu.Steps(len(everies))
			at := max(slices.Index(everies, cfg.Every), 0)
			rows := []menu.Row{
				{Label: say.T("poster.show"), Kind: menu.Toggle, On: cfg.Enabled},
				{
					Label: say.T("poster.every"),
					Kind:  menu.Slider,
					Level: steps.Level(at),
					Snap:  steps.Snap,
					Value: cfg.Every.Label(),
					Dim:   !cfg.Enabled,
				},
			}
			rows = append(rows, menu.Row{
				Label: say.T("poster.setting.fit"),
				Kind:  menu.Chevron,
				Value: cfg.Fit.Label(),
				Dim:   !cfg.Enabled,
			}, menu.Row{
				Label: say.T("poster.setting.transition"),
				Kind:  menu.Chevron,
				Value: cfg.Change.Label(),
				Dim:   !cfg.Enabled,
			})
			acts := []func(int){
				func(int) { p.options.Poster.SetEnabled(!cfg.Enabled) },
				func(level int) { p.options.Poster.SetEvery(everies[steps.Index(level)]) },
				p.open(func() shell.View {
					return Choose(
						say.T("poster.setting.fit"),
						config.PhotoFits(),
						func() config.PhotoFit { return p.options.Preferences.Poster.Read().Fit },
						p.options.Poster.SetFit,
						nil,
					)
				}),
				p.open(func() shell.View {
					return Choose(
						say.T("poster.setting.transition"),
						config.PhotoTransitions(),
						func() config.PhotoTransition { return p.options.Preferences.Poster.Read().Change },
						p.options.Poster.SetChange,
						nil,
					)
				}),
			}
			if p.options.Photos == nil {
				return rows, acts
			}
			c := p.options.Photos.Settings()
			change := func(name, v string) {
				if err := c.Change(name, v); err != nil {
					slog.Error("saving a setting failed", "setting", "photos."+name, "err", err)
				}
			}
			spans := config.PhotoAppSpans()
			app := menu.Steps(len(spans))
			now := c.Read().AppEvery
			rows = append(rows, menu.Row{
				Label: say.T("photos.setting.app.every"),
				Kind:  menu.Slider,
				Level: app.Level(max(slices.Index(spans, now), 0)),
				Snap:  app.Snap,
				Value: now.Label(),
			})
			acts = append(
				acts,
				func(level int) { change("app.every", string(spans[app.Index(level)])) },
			)
			rows = append(rows, menu.Row{
				Label: say.T("photos.setting.app.fit"),
				Kind:  menu.Chevron,
				Value: c.Read().AppFit.Label(),
			})
			acts = append(acts, p.open(func() shell.View {
				return Choose(
					say.T("photos.setting.app.fit"),
					config.PhotoFits(),
					func() config.PhotoFit { return c.Read().AppFit },
					func(f config.PhotoFit) { change("app.fit", string(f)) },
					nil,
				)
			}))
			rows = append(rows, menu.Row{
				Label: say.T("photos.setting.app.transition"),
				Kind:  menu.Chevron,
				Value: c.Read().AppChange.Label(),
			})
			acts = append(acts, p.open(func() shell.View {
				return Choose(
					say.T("photos.setting.app.transition"),
					config.PhotoTransitions(),
					func() config.PhotoTransition { return c.Read().AppChange },
					func(t config.PhotoTransition) { change("app.transition", string(t)) },
					nil,
				)
			}))
			for _, src := range p.options.Photos.Providers() {
				rows = append(rows, menu.Row{
					Label: src.Name(),
					Kind:  menu.Chevron,
					Value: p.options.Photos.State(src.Key()).Says(),
				})
				acts = append(acts, p.open(func() shell.View { return p.PhotoSource(src) }))
			}
			return rows, acts
		},
	}
}

func (p *BasicPages) PhotoSource(src photos.Provider) *shell.Page {
	lib := p.options.Photos
	return &shell.Page{
		Title: src.Name(),
		Build: func() ([]menu.Row, []func(int)) {
			c := lib.Settings()
			at := c.Read()
			rows := []menu.Row{{
				Label: say.T("photos.status"),
				Value: lib.State(src.Key()).Says(),
			}}
			acts := []func(int){nil}
			for _, s := range c.Table.Rows() {
				if !strings.HasPrefix(s.Name, src.Key()+".") {
					continue
				}
				name := "photos." + s.Name
				save := func(v string) {
					if err := c.Change(s.Name, v); err != nil {
						slog.Error("saving a setting failed", "setting", name, "err", err)
					}
				}
				row := menu.Row{Label: s.Title()}
				var tap func(int)
				switch s.Kind {
				case setting.Toggle:
					on, _ := setting.Boolean(s.Read(&at))
					row.Kind, row.On = menu.Toggle, on
					tap = func(int) { save(setting.OnOff(!on)) }
				case setting.Number:
					n, _ := strconv.Atoi(s.Read(&at))
					steps := menu.Steps(s.Max - s.Min + 1)
					row.Kind, row.Snap, row.Value = menu.Slider, steps.Snap, strconv.Itoa(n)
					row.Level = steps.Level(n - s.Min)
					tap = func(level int) { save(strconv.Itoa(s.Min + steps.Index(level))) }
				case setting.Choice:
					now := s.Read(&at)
					labels := make([]string, len(s.Options))
					for n, o := range s.Options {
						labels[n] = o.Label
						if o.Value == now {
							row.Level = n
						}
					}
					row.Kind, row.Options = menu.Segments, labels
					tap = func(n int) { save(s.Options[n].Value) }
				case setting.Secret, setting.Credential:
					row.Kind, row.Value, row.Save = menu.Password, s.Read(&at), save
				default:
					row.Kind, row.Value, row.Save = menu.Field, s.Read(&at), save
				}
				rows = append(rows, row)
				acts = append(acts, tap)
			}
			return rows, acts
		},
	}
}
