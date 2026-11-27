package timerview

import (
	"fmt"
	"log/slog"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/assistant/timer"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
)

// DefaultTimerDomain is the Home Assistant integration whose pause, resume and cancel
// actions the Split keys call, each with a timer_id.
const DefaultTimerDomain = "voice_timer_control"

const (
	opPause  = "pause"
	opCancel = "cancel"
	splitMax = 4
)

// Cell is one timer's place on the screen.
type Cell struct{ X, Y, W, H float32 }

// splitCells lays out n timers: 1 fills the screen, 2 split it left and right, 3 or 4 fill
// a 2x2 grid in reading order. More than 4 still get 4 cells.
func splitCells(n int, w, h float32) []Cell {
	switch {
	case n <= 0:
		return nil
	case n == 1:
		return []Cell{{0, 0, w, h}}
	case n == 2:
		return []Cell{{0, 0, w / 2, h}, {w / 2, 0, w / 2, h}}
	}
	grid := []Cell{{0, 0, w / 2, h / 2}, {w / 2, 0, w / 2, h / 2}, {0, h / 2, w / 2, h / 2}, {w / 2, h / 2, w / 2, h / 2}}
	return grid[:min(n, splitMax)]
}

// plusMark says how many timers did not get a cell.
func plusMark(n int) string {
	if n <= splitMax {
		return ""
	}
	return fmt.Sprintf("+%d", n-splitMax)
}

// shownEntries is what the Split style draws. When the display forgot its timers but a
// ring goes on, it still shows one finished cell, so the ring can be stopped by touch.
func shownEntries(c timer.Card) []timer.Entry {
	if len(c.All) > 0 {
		return c.All
	}
	if c.Ringing {
		return []timer.Entry{{Name: c.Name, Done: true}}
	}
	return nil
}

func (v *View) domain() string {
	if v.Domain != nil {
		if d := v.Domain(); d != "" {
			return d
		}
	}
	return DefaultTimerDomain
}

// press is what a Split key does. A finished timer is already gone in Home Assistant, so
// its keys only stop the ring. Everything else asks Home Assistant; the screen follows the
// timer event that comes back.
func (v *View) press(e timer.Entry, op string) func() {
	return func() {
		if e.Done {
			v.Timers.Stop()
			return
		}
		if op == opPause && e.Paused {
			op = "resume"
		}
		action := v.domain() + "." + op
		if v.Act == nil {
			slog.Warn("timer key has no way to reach Home Assistant", "action", action)
			return
		}
		act := v.Act
		// Off the screen's thread: a slow Home Assistant must not freeze the display.
		v.goSpawn("timer key", func() {
			if err := act(action, e.ID); err != nil {
				slog.Warn("timer key failed", "action", action, "timer", e.ID, "err", err)
			}
		})
	}
}

func (v *View) goSpawn(what string, f func()) {
	if v.spawn != nil {
		v.spawn(what, f)
		return
	}
	safe.Go(what, f)
}

// cellTap stops the ring when a finished cell is tapped anywhere; other cells do nothing.
func (v *View) cellTap(e timer.Entry) func() {
	if !e.Done {
		return nil
	}
	return func() { v.Timers.Stop() }
}

// cellLines is the text under a cell's dial. A finished dial shows a bell, so the cell
// names the timer itself.
func cellLines(e timer.Entry) []string {
	switch {
	case e.Done:
		return []string{e.Name, timer.Remaining(0) + " · " + say.T("timer.up")}
	}
	return nil
}

// dialNote is written inside the dial, under the name, so the dial does not move when a
// timer is paused.
func dialNote(e timer.Entry) string {
	if e.Paused && !e.Done {
		return say.T("timer.paused")
	}
	return ""
}

func cellID(i int) string { return fmt.Sprintf("timer-split-%d-cell", i) }
func dialID(i int) string { return fmt.Sprintf("timer-split-%d", i) }

func (v *View) split(w *gogui.Window, c timer.Card) gogui.View {
	vw, vh := w.WindowSize()
	fw, fh := float32(vw), float32(vh)
	entries := shownEntries(c)
	cells := splitCells(len(entries), fw, fh)
	views := make([]gogui.View, len(cells))
	for i, cell := range cells {
		mark := ""
		if i == len(cells)-1 {
			mark = plusMark(len(entries))
		}
		views[i] = v.splitCell(w, i, entries[i], cell, mark)
	}

	var rows []gogui.View
	switch len(views) {
	case 1:
		rows = views
	case 2:
		rows = []gogui.View{splitRow(views)}
	default:
		rows = []gogui.View{splitRow(views[:2]), splitRow(views[2:])}
	}
	return gogui.Column(widgets.Blocking(splitScreenCfg(rows, fw, fh, widgets.Color(v.UI.Palette().Background))))
}

// splitScreenCfg and splitRowCfg set no spacing: an unset spacing is the theme's gap, which
// would push the right and bottom cells past the screen edge.
func splitScreenCfg(rows []gogui.View, w, h float32, back gogui.Color) gogui.ContainerCfg {
	return gogui.ContainerCfg{
		ID:      "timer-split",
		Width:   w,
		Height:  h,
		Sizing:  gogui.FixedFixed,
		Color:   back,
		Radius:  gogui.NoRadius,
		Padding: gogui.NoPadding,
		Spacing: gogui.NoSpacing,
		Content: rows,
	}
}

func splitRowCfg(views []gogui.View) gogui.ContainerCfg {
	return gogui.ContainerCfg{
		Sizing:  gogui.FillFit,
		Padding: gogui.NoPadding,
		Spacing: gogui.NoSpacing,
		Content: views,
	}
}

func splitRow(views []gogui.View) gogui.View { return gogui.Row(splitRowCfg(views)) }

func (v *View) splitCell(w *gogui.Window, i int, e timer.Entry, cell Cell, mark string) gogui.View {
	t := gogui.CurrentTheme()
	pal := v.UI.Palette()
	id := dialID(i)
	card := timer.Card{Showing: true, Ringing: e.Done, Name: e.Name, Left: e.Left, Of: e.Of}

	side, key := cellSizes(cell)
	left, right := cellKeys(e)
	dialRow := []gogui.View{
		v.roundKey(id+"-"+left, keyGlyph(e, left), key, v.press(e, left)),
		v.dialAs(w, card, side, id, dialNote(e)),
	}
	if right != "" {
		dialRow = append(dialRow, v.roundKey(id+"-"+right, keyGlyph(e, right), key, v.press(e, right)))
	} else {
		dialRow = append(dialRow, gogui.Row(gogui.ContainerCfg{Width: key, Height: key, Sizing: gogui.FixedFixed}))
	}
	content := []gogui.View{gogui.Row(gogui.ContainerCfg{
		Sizing:  gogui.FitFit,
		VAlign:  gogui.VAlignMiddle,
		Padding: gogui.NoPadding,
		Spacing: gogui.SpacingLarge,
		Content: dialRow,
	})}

	for _, line := range cellLines(e) {
		st := t.TextStyleTitle
		st.Color, st.Align = widgets.Color(pal.Muted), gogui.TextAlignCenter
		content = append(content, gogui.Label(line, st))
	}
	if mark != "" {
		st := t.TextStyleTitle
		st.Color = widgets.Color(pal.Muted)
		content = append(content, gogui.Label(mark, st))
	}

	cfg := gogui.ContainerCfg{
		ID:      cellID(i),
		Width:   cell.W,
		Height:  cell.H,
		Sizing:  gogui.FixedFixed,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Padding: gogui.PaddingLarge,
		Spacing: gogui.SpacingMedium,
		Clip:    true,
		Content: content,
	}
	if tap := v.cellTap(e); tap != nil {
		return v.UI.Presses.Pressable(gogui.Column, cfg, func(gogui.EventCtx) { tap() })
	}
	return gogui.Column(cfg)
}

// cellSizes fits the dial and the two keys beside it inside a cell, with room for the
// gaps between them: the dial gets at most 46 % of the width and 58 % of the height.
func cellSizes(cell Cell) (dial, key float32) {
	short := min(cell.W, cell.H)
	key = max(short*0.13, 72)
	dial = min(cell.H*0.58, cell.W*0.46)
	return dial, key
}

// cellKeys says which key sits left and right of a cell's dial: × on the left, pause on
// the right. A finished timer has only the ×.
func cellKeys(e timer.Entry) (left, right string) {
	if e.Done {
		return opCancel, ""
	}
	return opCancel, opPause
}

// keyGlyph is the icon on a key: × to cancel, ‖ to pause, ▶ to resume a paused timer.
func keyGlyph(e timer.Entry, op string) string {
	switch {
	case op == opCancel:
		return gogui.IconClose
	case e.Paused:
		return gogui.IconPlay
	}
	return gogui.IconPause
}

// roundKeyFill is the circle behind a key: the dial's own surface colour, solid, so it
// shows on the background (widgets.KeyFill is the background at 40 %, which does not).
func roundKeyFill(pal theme.Theme) gogui.Color { return widgets.Color(pal.Surface) }

// roundKey is an icon in a filled circle, like the music player's keys.
func (v *View) roundKey(id, glyph string, side float32, do func()) gogui.View {
	pal := v.UI.Palette()
	st := widgets.IconStyle(widgets.Color(pal.Text))
	st.Size = side * 0.5
	return v.UI.Presses.Inked(gogui.Column, gogui.ContainerCfg{
		ID:      id,
		Width:   side,
		Height:  side,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Radius:  gogui.RadiusPx(side / 2),
		Color:   roundKeyFill(pal),
		Content: []gogui.View{gogui.Label(glyph, st)},
	}, widgets.Color(pal.Text), func(gogui.EventCtx) { do() })
}
