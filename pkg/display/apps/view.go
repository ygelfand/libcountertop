package apps

import (
	"log/slog"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/icons"
	"github.com/ygelfand/libcountertop/pkg/display/navigation"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/say"
)

const (
	settle = 400 * time.Millisecond
	lift   = 1.1
)

func (a *Apps) Screens(look *widgets.UI) func(shell.View) *navigation.Screen {
	return func(v shell.View) *navigation.Screen {
		if own, ok := v.(*View); !ok || own.apps != a {
			return nil
		}
		return &navigation.Screen{
			Title: say.T("apps.title"),
			View:  v,
			Build: func(w *gogui.Window) gogui.View { return a.body(w, look) },
		}
	}
}

type grid struct {
	cols      int
	side, gap float32
}

func layout(width float32) grid {
	gap := style.Reach() * 0.25
	cols := max(3, int(width/(style.Reach()*4)))
	return grid{cols: cols, side: (width - gap*float32(cols-1)) / float32(cols), gap: gap}
}

func (g grid) cell(n int) ui.Rect {
	step := g.side + g.gap
	return ui.Rect{
		X: int(float32(n%g.cols) * step),
		Y: int(float32(n/g.cols) * step),
		W: int(g.side),
		H: int(g.side),
	}
}

func (g grid) at(x, y float32, count int) int {
	step := g.side + g.gap
	col, row := int(x/step), int(y/step)
	if x < 0 || y < 0 || col >= g.cols || x-float32(col)*step > g.side ||
		y-float32(row)*step > g.side {
		return -1
	}
	if n := row*g.cols + col; n < count {
		return n
	}
	return -1
}

func (g grid) height(count int) float32 {
	rows := (count + g.cols - 1) / g.cols
	return max(0, float32(rows)*(g.side+g.gap)-g.gap)
}

func (g grid) drop(from int, dx, dy float32, count int) int {
	at := g.cell(from)
	step := g.side + g.gap
	x := float32(at.X) + g.side/2 + dx
	y := float32(at.Y) + g.side/2 + dy
	col := min(max(int(x/step), 0), g.cols-1)
	row := max(int(y/step), 0)
	return min(row*g.cols+col, count-1)
}

func (a *Apps) body(w *gogui.Window, look *widgets.UI) gogui.View {
	width := widgets.PageWidth(w)
	g := layout(width)
	list := a.List()

	a.mu.Lock()
	d := a.drag
	a.mu.Unlock()

	var tiles []gogui.View
	slot := 0
	target := -1
	if d.from >= 0 {
		target = g.drop(d.from, d.dx, d.dy, len(list))
	}
	for n, app := range list {
		if n == d.from {
			continue
		}
		if slot == target {
			slot++
		}
		tiles = append(tiles, widgets.Placed(g.cell(slot), a.tile(look, app, g.side)))
		slot++
	}
	if d.from >= 0 {
		grown := g.side * lift
		at := g.cell(d.from)
		at.X += int(d.dx - (grown-g.side)/2)
		at.Y += int(d.dy - (grown-g.side)/2)
		at.W, at.H = int(grown), int(grown)
		tiles = append(tiles, widgets.Placed(at, a.tile(look, list[d.from], grown)))
	}

	content := []gogui.View{gogui.Column(gogui.ContainerCfg{
		ID:      "apps",
		Width:   width,
		Height:  g.height(len(list)),
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Content: tiles,
	})}
	note := say.T("apps.hint")
	if len(list) == 0 {
		note = say.T("apps.empty")
	}
	content = append(content, gogui.Label(note, widgets.Secondary()))

	return gogui.Column(gogui.ContainerCfg{
		Width:   width,
		Sizing:  gogui.FixedFit,
		Padding: gogui.NoPadding,
		Spacing: gogui.SpacingPx(g.gap),
		Content: content,
		OnGesture: func(e gogui.EventCtx) {
			slog.Debug(
				"apps gesture",
				"type",
				e.Event.GestureType,
				"phase",
				e.Event.GesturePhase,
				"x",
				e.Event.CentroidX,
				"y",
				e.Event.CentroidY,
			)
			switch e.Event.GestureType {
			case gogui.GesturePan:
				if !a.pan(e, g, len(list)) {
					return
				}
			case gogui.GestureSwipe:
				if !a.dragging() {
					return
				}
				a.held.End(e.Window, 0)
			default:
				return
			}
			e.Consume()
			e.Window.InvalidateLayout()
		},
	})
}

func (a *Apps) tile(look *widgets.UI, app App, side float32) gogui.View {
	docked := a.Docked(app.ID)
	mark := gogui.IconStarO
	if docked {
		mark = gogui.IconStar
	}
	st := gogui.CurrentTheme().TextStyleIconLarge
	st.Color = gogui.CurrentTheme().Cfg.TextStyleDef.Color
	st.Size = side * 0.2
	star := look.Presses.Pressable(gogui.Row, gogui.ContainerCfg{
		ID:      "app-star-" + app.ID,
		Padding: gogui.PadAll(side * 0.06),
		Content: []gogui.View{gogui.Label(mark, st)},
	}, func(e gogui.EventCtx) {
		a.SetDocked(app.ID, !docked)
		e.Consume()
	})

	return look.Entity(widgets.EntityTile{
		ID:        "app-" + app.ID,
		Width:     side,
		Height:    side,
		Name:      app.Label(),
		Available: true,
		Body: func(width, height float32) gogui.View {
			return face(app, star, width, height)
		},
		Tap: func(gogui.EventCtx) {
			if !a.settling() {
				app.Open()
			}
		},
	})
}

func face(app App, star gogui.View, width, height float32) gogui.View {
	t := gogui.CurrentTheme()
	icon := t.TextStyleIconLarge
	icon.Family = icons.Family
	icon.Color = t.Cfg.TextStyleDef.Color
	icon.Size = min(width, height) * 0.45
	return gogui.Column(gogui.ContainerCfg{
		Width:   width,
		Height:  height,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Spacing: gogui.SpacingPx(height * 0.04),
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Content: []gogui.View{
			gogui.Label(icons.Glyph(icons.Named(app.Icon)), icon),
			gogui.Label(app.Label(), t.Cfg.TextStyleDef),
			gogui.Column(gogui.ContainerCfg{
				Float:       true,
				FloatAnchor: gogui.FloatTopRight,
				FloatTieOff: gogui.FloatTopRight,
				Padding:     gogui.NoPadding,
				Content:     []gogui.View{star},
			}),
		},
	})
}

func (a *Apps) settling() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.drag.from >= 0 || time.Since(a.drag.dropped) < settle
}

func (a *Apps) dragging() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.drag.from >= 0
}

func (a *Apps) pan(e gogui.EventCtx, g grid, count int) bool {
	switch e.Event.GesturePhase {
	case gogui.GesturePhaseBegan:
		s := e.Layout.Shape
		from := g.at(e.Event.CentroidX-s.X, e.Event.CentroidY-s.Y, count)
		if from < 0 {
			return false
		}
		a.mu.Lock()
		a.drag = drag{from: from, dx: e.Event.GestureDX, dy: e.Event.GestureDY}
		a.mu.Unlock()
		a.held.Begin(e.Window, func(*gogui.Window, float32) { a.end(g, count) })
	case gogui.GesturePhaseChanged:
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.drag.from < 0 {
			return false
		}
		a.drag.dx += e.Event.GestureDX
		a.drag.dy += e.Event.GestureDY
	default:
		if !a.dragging() {
			return false
		}
		a.held.End(e.Window, 0)
	}
	return true
}

func (a *Apps) end(g grid, count int) {
	a.mu.Lock()
	d := a.drag
	a.drag = drag{from: -1, dropped: time.Now()}
	a.mu.Unlock()
	if d.from >= 0 {
		a.Move(d.from, g.drop(d.from, d.dx, d.dy, count))
	}
}
