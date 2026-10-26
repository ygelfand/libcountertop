package settings

import (
	"slices"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
)

func Choose[T interface {
	comparable
	Label() string
}](title string, values []T, read func() T, save func(T), preview func(T) menu.Preview) *shell.Page {
	values = slices.Clone(values)
	return &shell.Page{Title: title, Build: func() ([]menu.Row, []func(int)) {
		selected := read()
		rows := make([]menu.Row, 0, len(values))
		actions := make([]func(int), 0, len(values))
		for _, value := range values {
			row := menu.Row{Label: value.Label(), Chosen: value == selected}
			if preview != nil {
				row.Preview = preview(value)
			}
			rows = append(rows, row)
			actions = append(actions, func(int) { save(value) })
		}
		return rows, actions
	}}
}

func Tiles[T labeled](
	title string,
	values []T,
	read func() T,
	save func(T),
	preview func(T) menu.Preview,
) *shell.Page {
	return &shell.Page{Title: title, Tiles: func() ([]menu.Cell, []func(int)) {
		now := read()
		cells := make([]menu.Cell, 0, len(values))
		taps := make([]func(int), 0, len(values))
		for _, v := range values {
			cells = append(
				cells,
				menu.Cell{Label: v.Label(), Chosen: v == now, Preview: preview(v)},
			)
			taps = append(taps, func(int) { save(v) })
		}
		return cells, taps
	}}
}
