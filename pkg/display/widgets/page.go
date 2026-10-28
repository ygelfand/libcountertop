package widgets

import (
	"fmt"
	"slices"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
)

type PageRenderer struct {
	Row     func(string, string, menu.Row, func(int), bool) gogui.View
	Tile    func(*gogui.Window, string, string, menu.Cell, func(int)) gogui.View
	Preview func(*gogui.Window, func(ui.Rect) shell.Feed) gogui.View
	Beside  func(*gogui.Window, gogui.View, gogui.View) gogui.View
}

func PageBody(w *gogui.Window, p *shell.Page, render PageRenderer) gogui.View {
	var views []gogui.View
	switch {
	case p.Build != nil:
		rows, taps := p.Build()
		glyphs := slices.ContainsFunc(rows, func(r menu.Row) bool { return r.Glyph != "" })
		for i, r := range rows {
			var tap func(int)
			if i < len(taps) {
				tap = taps[i]
			}
			id := fmt.Sprintf("row-%d", i)
			view := render.Row(id, p.Title, r, tap, glyphs)
			if r.Label == p.Reveal {
				view = revealRow(w, p, r.Label, view)
			}
			views = append(views, view)
		}
	case p.Tiles != nil:
		cells, taps := p.Tiles()
		var tiles []gogui.View
		for i, c := range cells {
			var tap func(int)
			if i < len(taps) {
				tap = taps[i]
			}
			tiles = append(tiles, render.Tile(w, fmt.Sprintf("tile-%d", i), p.Title, c, tap))
		}
		views = append(
			views,
			gogui.Row(
				gogui.ContainerCfg{
					Sizing:  gogui.FillFit,
					Wrap:    true,
					Spacing: gogui.SpacingMedium,
					Content: tiles,
				},
			),
		)
	}
	list := gogui.Column(
		gogui.ContainerCfg{
			Sizing:  gogui.FillFit,
			Padding: gogui.NoPadding,
			Spacing: gogui.SpacingTight,
			Content: views,
		},
	)
	switch {
	case p.Side != nil:
		if side := p.Side(w); side != nil {
			return render.Beside(w, side, list)
		}
	case p.Preview != nil:
		return render.Beside(w, render.Preview(w, p.Preview), list)
	}
	return list
}

func revealRow(w *gogui.Window, p *shell.Page, label string, view gogui.View) gogui.View {
	return gogui.Column(gogui.ContainerCfg{
		Sizing: gogui.FillFit, Padding: gogui.NoPadding, Content: []gogui.View{view},
		AmendLayout: func(ev gogui.EventCtx) {
			top := ev.Layout.Shape.Y
			if p.Reveal != label {
				return
			}
			p.Reveal = ""
			w.QueueCommand(func(w *gogui.Window) {
				if _, y, ok := w.LocalToWindow("page", 0, 0); ok {
					w.ScrollVerticalTo("page", w.ScrollVerticalOffset("page")-top+y)
				}
				w.InvalidateLayout()
			})
		},
	})
}
