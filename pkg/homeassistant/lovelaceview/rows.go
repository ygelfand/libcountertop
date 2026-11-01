package lovelaceview

import (
	"log/slog"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/say"
)

type pick struct {
	name  string
	glyph string
}

func (f *Feature) Rows(saver bool) ([]menu.Row, []func(int)) {
	picks := []pick{
		{"user", gogui.IconUser},
		{"dashboard", gogui.IconLayout},
		{"view", gogui.IconPictureSquare},
	}
	if saver {
		picks = append(picks,
			pick{"saver_dashboard", gogui.IconMoon},
			pick{"saver_view", gogui.IconPicture},
		)
	}
	rows := make([]menu.Row, 0, len(picks))
	taps := make([]func(int), 0, len(picks))
	for _, p := range picks {
		value, _ := f.knobs.Value(p.name)
		rows = append(rows, menu.Row{
			Glyph: p.glyph,
			Label: say.T("lovelace.setting." + p.name),
			Kind:  menu.Chevron,
			Value: value,
			Open:  func() shell.View { return f.page(p.name) },
		})
		taps = append(taps, nil)
	}
	return rows, taps
}

func (f *Feature) page(name string) *shell.Page {
	return &shell.Page{
		Title: say.T("lovelace.setting." + name),
		Build: func() ([]menu.Row, []func(int)) {
			s, ok := f.knobs.Table.Find(name)
			if !ok {
				return nil, nil
			}
			now := s.Read(new(f.o.Settings.Read()))
			choices := s.Choices()
			rows := make([]menu.Row, 0, len(choices))
			taps := make([]func(int), 0, len(choices))
			for _, o := range choices {
				rows = append(rows, menu.Row{Label: s.Label(o), Chosen: o.Value == now})
				taps = append(taps, func(int) {
					if err := f.knobs.Change(name, o.Value); err != nil {
						slog.Warn(
							"the dashboard setting could not be saved",
							"setting",
							name,
							"err",
							err,
						)
					}
				})
			}
			return rows, taps
		},
	}
}
