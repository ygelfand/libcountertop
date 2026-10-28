package screens

import (
	"fmt"
	"math"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/homecontrol"
	"github.com/ygelfand/libcountertop/pkg/say"

	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
)

func (app *App) tileBoard(at ui.Rect, s homecontrol.Selection, pal theme.Theme) gogui.View {
	return widgets.Placed(at, app.tileList(float32(at.W), s, pal))
}

func (app *App) tileList(width float32, s homecontrol.Selection, pal theme.Theme) gogui.View {
	areas, loading := app.o.Home.Dash().Tiles(s)
	gap := reach() * 0.25
	cols := max(2, int(width/(reach()*6)))
	side := cell(width-gap, gap, cols)

	var views []gogui.View
	switch {
	case loading:
		views = append(views, gogui.Label(say.T("home.loading"), widgets.Secondary()))
	case len(areas) == 0:
		views = append(views, gogui.Label(say.T("home.empty"), widgets.Secondary()))
	}
	stacks := make([][]gogui.View, cols)
	for i, a := range areas {
		stacks[i%cols] = append(
			stacks[i%cols],
			app.card(fmt.Sprintf("tile-%d", i), s, a, side, pal),
		)
	}
	var columns []gogui.View
	for _, stack := range stacks {
		columns = append(
			columns,
			gogui.Column(
				gogui.ContainerCfg{
					Width:   side,
					Sizing:  gogui.FixedFit,
					Padding: gogui.NoPadding,
					Spacing: gogui.SpacingPx(gap),
					Content: stack,
				},
			),
		)
	}
	if len(areas) > 0 {
		views = append(
			views,
			gogui.Row(
				gogui.ContainerCfg{
					Sizing:  gogui.FillFit,
					Padding: gogui.NoPadding,
					Spacing: gogui.SpacingPx(gap),
					Content: columns,
				},
			),
		)
	}

	return gogui.Column(gogui.ContainerCfg{
		ID:         "tiles",
		Sizing:     gogui.FillFill,
		Padding:    gogui.NoPadding,
		Spacing:    gogui.SpacingPx(gap),
		Scrollable: true,
		OnGesture:  app.ui.HoldStill,
		Content:    views,
	})
}

func (app *App) fullScreen(f *homecontrol.Full) *Screen {
	return &Screen{
		Title:    f.Sel.Name(),
		View:     f,
		Fixed:    true,
		Settings: func() { app.o.Shell.Push(app.o.Home.NewPicker(f.Sel)) },
		Build: func(w *gogui.Window) gogui.View {
			s, ok := app.o.Home.Dash().Find(f.Sel.Key)
			if !ok {
				return gogui.Label(say.T("home.empty"), widgets.Secondary())
			}
			return app.tileList(widgets.PageWidth(w), s, app.ui.Palette())
		},
	}
}

func areaName(a homecontrol.AreaTiles) string {
	if a.ID == "" {
		return say.T("home.noarea")
	}
	return a.Name
}

func (app *App) card(
	id string,
	s homecontrol.Selection,
	a homecontrol.AreaTiles,
	side float32,
	pal theme.Theme,
) gogui.View {
	if a.Solo {
		t := a.Tiles[0]
		return app.ui.Entity(widgets.EntityTile{
			ID:        id,
			Width:     side,
			Glyph:     widgets.DomainGlyph(t.Domain()),
			Name:      t.Name,
			Status:    deviceStatus(t),
			On:        t.On,
			Available: t.Available,
			Tap:       func(gogui.EventCtx) { app.o.Home.Dash().ToggleEntity(t) },
		})
	}
	var chips []gogui.View
	if a.Open {
		for j, t := range a.Tiles {
			chips = append(
				chips,
				app.chip(
					fmt.Sprintf("%s-%d", id, j),
					widgets.DomainGlyph(t.Domain()),
					t.Name,
					deviceStatus(t),
					t.On,
					t.Available,
					pal,
					func(gogui.EventCtx) { app.o.Home.Dash().ToggleEntity(t) },
				),
			)
		}
	}
	return app.ui.Entity(widgets.EntityTile{
		ID:        id,
		Width:     side,
		Glyph:     glyphFor(s),
		Name:      areaName(a),
		Status:    areaStatus(a),
		On:        a.On > 0,
		Available: true,
		Corner:    app.chevron(id, s, a, pal),
		More:      chips,
		Tap:       func(gogui.EventCtx) { app.o.Home.Dash().ToggleArea(a) },
	})
}

func areaStatus(a homecontrol.AreaTiles) string {
	switch a.On {
	case 0:
		return say.T("home.tile.all.off")
	case len(a.Tiles):
		return say.T("home.tile.all.on")
	}
	return say.F("home.tile.area", map[string]any{"N": a.On, "Of": len(a.Tiles)})
}

func cell(width, gap float32, n int) float32 {
	return float32(math.Floor(float64((width - gap*float32(n-1)) / float32(n))))
}

func deviceStatus(t homecontrol.Tile) string {
	switch {
	case !t.Available:
		return say.T("home.tile.unavailable")
	case t.On && t.Level >= 0:
		return say.F("home.tile.level", map[string]any{"N": t.Level})
	case t.On:
		return say.T("home.tile.on")
	}
	return say.T("home.tile.off")
}

func (app *App) chip(
	id string,
	glyph, name, status string,
	on, available bool,
	pal theme.Theme,
	tap func(gogui.EventCtx),
) gogui.View {
	t := gogui.CurrentTheme().Cfg
	badgeFill, badgeInk, fill := widgets.Tint(on, pal)
	if !on {
		fill = widgets.Color(pal.Background).WithOpacity(0.35)
	}
	mark := widgets.IconStyle(badgeInk)
	mark.Size *= 0.7
	small := t.TextStyleDef
	small.Size *= 0.8
	note := widgets.Secondary()
	note.Size *= 0.75
	badge := gogui.Row(gogui.ContainerCfg{
		Width:   reach() * 0.7,
		Height:  reach() * 0.7,
		Sizing:  gogui.FixedFixed,
		Radius:  gogui.RadiusMedium,
		Color:   badgeFill,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Padding: gogui.NoPadding,
		Content: []gogui.View{gogui.Label(glyph, mark)},
	})
	cfg := gogui.ContainerCfg{
		ID:       id,
		Sizing:   gogui.FillFit,
		Padding:  gogui.PaddingSmall,
		Spacing:  gogui.SpacingSmall,
		Radius:   gogui.RadiusMedium,
		Color:    fill,
		VAlign:   gogui.VAlignMiddle,
		Disabled: !available,
		Content: []gogui.View{
			badge,
			gogui.Column(
				gogui.ContainerCfg{
					Sizing:  gogui.FillFit,
					Padding: gogui.NoPadding,
					Clip:    true,
					Content: []gogui.View{
						gogui.Label(name, small),
						gogui.Label(status, note),
					},
				},
			),
		},
	}
	app.ui.Kit().Tile(&cfg, widgets.Chosen(on))
	return app.ui.Presses.Pressable(gogui.Row, cfg, tap)
}

func glyphFor(s homecontrol.Selection) string {
	if s.Key == "lights" {
		return gogui.IconSunnyO
	}
	return gogui.IconPlug
}

func (app *App) chevron(
	id string,
	s homecontrol.Selection,
	a homecontrol.AreaTiles,
	pal theme.Theme,
) gogui.View {
	glyph := gogui.IconArrowDown
	if a.Open {
		glyph = gogui.IconArrowUp
	}
	return app.ui.Toolkit.IconKey(gogui.ContainerCfg{
		ID:      id + "-chevron",
		Width:   reach(),
		Height:  reach(),
		Sizing:  gogui.FixedFixed,
		Radius:  gogui.RadiusLarge,
		Color:   widgets.Color(pal.Background).WithOpacity(0.35),
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Padding: gogui.NoPadding,
	}, glyph, widgets.IconStyle(widgets.Color(pal.Text)), style.Partial, func(gogui.EventCtx) { app.o.Home.Dash().Expand(s, a.ID) })
}
