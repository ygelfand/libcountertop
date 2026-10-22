package homeview

import (
	"slices"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/say"
	schema "github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func (view *Renderer) embeddedRows() ([]menu.Row, []func(int)) {
	engine := view.options.Engine
	themed, turn, bottom, follow := engine.Themed(), engine.Turn(), engine.Bottom(), engine.Follow()
	bottoms := schema.TesseraBottoms()
	labels := make([]string, len(bottoms))
	for i, b := range bottoms {
		labels[i] = b.Label()
	}
	rows := []menu.Row{
		{
			Label:   say.T("home.tessera.bottom"),
			Kind:    menu.Segments,
			Options: labels,
			Level:   max(slices.Index(bottoms, bottom), 0),
		},
		{Label: say.T("home.tessera.themed"), Kind: menu.Toggle, On: themed},
		{Label: say.T("home.tessera.turn"), Kind: menu.Chevron, Value: turn.Label()},
		{Label: say.T("home.tessera.follow"), Kind: menu.Toggle, On: follow},
	}
	taps := []func(int){
		func(i int) { engine.SetBottom(bottoms[i]) },
		func(int) { engine.SetThemed(!themed) },
		func(int) { view.options.Shell.Push(view.turnPage()) },
		func(int) { engine.SetFollow(!follow) },
	}
	return rows, taps
}

func (view *Renderer) gridPage() *shell.Page {
	return &shell.Page{
		Title: say.T("home.tessera.grid"),
		Build: func() ([]menu.Row, []func(int)) {
			engine := view.options.Engine
			g := view.grid.shown(engine.Grid())
			rows := []menu.Row{
				gridRow(say.T("home.tessera.columns"), g.Columns),
				gridRow(say.T("home.tessera.rows"), g.Rows),
			}
			taps := []func(int){
				func(level int) {
					c := gridOf(level)
					r := min(g.Rows, schema.TesseraCells/c)
					view.grid.move(schema.TesseraGrid{Columns: c, Rows: r}, engine.SetGrid)
				},
				func(level int) {
					r := gridOf(level)
					c := min(g.Columns, schema.TesseraCells/r)
					view.grid.move(schema.TesseraGrid{Columns: c, Rows: r}, engine.SetGrid)
				},
			}
			return rows, taps
		},
		Side: view.gridPreview,
	}
}

func (view *Renderer) gridPreview(w *gogui.Window) gogui.View {
	g := view.grid.shown(view.options.Engine.Grid())
	vw, vh := w.WindowSize()
	across, down := g.Columns, g.Rows
	if vh > vw {
		across, down = g.Rows, g.Columns
	}
	width := float32(vw) * 0.42
	height := width * float32(vh) / float32(vw)
	if most := float32(vh) * 0.7; height > most {
		width, height = width*most/height, most
	}
	pal := view.options.UI.Palette()
	gap := max(width/float32(across*24), 2)
	lines := make([]gogui.View, 0, down)
	for range down {
		cells := make([]gogui.View, 0, across)
		for range across {
			cells = append(cells, gogui.Column(gogui.ContainerCfg{
				Sizing:  gogui.FillFill,
				Padding: gogui.NoPadding,
				Radius:  gogui.RadiusMedium,
				Color:   widgets.Color(pal.Surface),
			}))
		}
		lines = append(lines, gogui.Row(gogui.ContainerCfg{
			Sizing:  gogui.FillFill,
			Padding: gogui.NoPadding,
			Spacing: gogui.SpacingPx(gap),
			Content: cells,
		}))
	}
	return gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FixedFixed,
		Width:   width,
		Height:  height,
		Padding: gogui.PaddingSmall,
		Spacing: gogui.SpacingPx(gap),
		Radius:  gogui.RadiusLarge,
		Color:   widgets.Color(pal.Background),
		Content: lines,
	})
}

func (view *Renderer) turnPage() *shell.Page {
	return &shell.Page{
		Title: say.T("home.tessera.turn"),
		Build: func() ([]menu.Row, []func(int)) {
			engine := view.options.Engine
			now := engine.Turn()
			var (
				rows []menu.Row
				taps []func(int)
			)
			for _, t := range schema.TesseraTurns() {
				rows = append(rows, menu.Row{Label: t.Label(), Chosen: t == now})
				taps = append(taps, func(int) { engine.SetTurn(t) })
			}
			return rows, taps
		},
	}
}
