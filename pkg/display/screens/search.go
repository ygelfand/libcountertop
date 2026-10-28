package screens

import (
	gui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/say"
)

const searchFieldID = "settings-search"

func (a *App) searchField() gui.View {
	search := a.o.Shell.Search()
	if search == nil {
		return nil
	}
	return a.ui.Input(gui.InputCfg{
		ID:            searchFieldID,
		Text:          *search.Query,
		Placeholder:   say.T("settings.search"),
		Sizing:        gui.FillFit,
		OnTextChanged: func(value string, ev gui.EventCtx) { *search.Query = value; ev.Window.InvalidateLayout() },
	}, nil)
}

func (a *App) searchResults(w *gui.Window) gui.View {
	search := a.o.Shell.Search()
	if search == nil {
		return nil
	}
	return widgets.PageBody(w, search.Results(), widgets.PageRenderer{
		Row: func(id, page string, row menu.Row, tap func(int), glyphs bool) gui.View {
			if tap != nil {
				next := tap
				tap = func(level int) { w.ClearFocus(); next(level); w.InvalidateLayout() }
			}
			return a.ui.Row(id, page, row, tap, glyphs)
		},
	})
}
