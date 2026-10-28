package settings

import (
	"log/slog"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
)

func ServicePage[T any](title string, c *setting.Controls[T]) *shell.Page {
	return &shell.Page{Title: title, Build: func() ([]menu.Row, []func(int)) {
		at := c.Read()
		var (
			rows []menu.Row
			taps []func(int)
		)
		for _, s := range c.Table.Rows() {
			row := menu.Row{Label: s.Title()}
			var tap func(int)
			save := func(v string) {
				if err := c.Change(s.Name, v); err != nil {
					slog.Error(
						"saving a setting failed",
						"setting",
						c.Domain()+"."+s.Name,
						"err",
						err,
					)
				}
			}
			switch s.Kind {
			case setting.Toggle:
				row.Kind, row.On = menu.Toggle, s.On(&at)
				tap = func(int) { save(setting.OnOff(!row.On)) }
			case setting.Choice:
				now, choices := s.Read(&at), s.Choices()
				row.Kind, row.Options = menu.Segments, make([]string, len(choices))
				for n, o := range choices {
					row.Options[n] = s.Label(o)
					if o.Value == now {
						row.Level = n
					}
				}
				tap = func(n int) { save(choices[n].Value) }
			case setting.Text:
				row.Kind, row.Value, row.Save = menu.Field, s.Read(&at), save
			case setting.Secret, setting.Credential:
				row.Kind, row.Value, row.Save = menu.Password, s.Read(&at), save
			default:
				continue
			}
			rows = append(rows, row)
			taps = append(taps, tap)
		}
		return rows, taps
	}}
}
