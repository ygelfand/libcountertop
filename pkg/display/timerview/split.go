package timerview

import (
	"fmt"
	"log/slog"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/assistant/timer"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
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
		if err := v.Act(action, e.ID); err != nil {
			slog.Warn("timer key failed", "action", action, "timer", e.ID, "err", err)
		}
	}
}

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
	return gogui.Column(widgets.Blocking(gogui.ContainerCfg{
		ID:      "timer-split",
		Width:   fw,
		Height:  fh,
		Sizing:  gogui.FixedFixed,
		Color:   widgets.Color(v.UI.Palette().Background),
		Radius:  gogui.NoRadius,
		Padding: gogui.NoPadding,
		Content: rows,
	}))
}

func splitRow(views []gogui.View) gogui.View {
	return gogui.Row(gogui.ContainerCfg{Sizing: gogui.FillFit, Padding: gogui.NoPadding, Content: views})
}

func (v *View) splitCell(w *gogui.Window, i int, e timer.Entry, cell Cell, mark string) gogui.View {
	t := gogui.CurrentTheme()
	pal := v.UI.Palette()
	id := fmt.Sprintf("timer-split-%d", i)
	card := timer.Card{Showing: true, Ringing: e.Done, Name: e.Name, Left: e.Left, Of: e.Of}

	side := min(cell.W, cell.H) * 0.62
	content := []gogui.View{v.dialAs(w, card, side, id)}

	status := ""
	switch {
	case e.Done:
		status = say.T("timer.up")
	case e.Paused:
		status = say.T("timer.paused")
	}
	if status != "" {
		st := t.TextStyleTitle
		st.Color, st.Align = widgets.Color(pal.Muted), gogui.TextAlignCenter
		content = append(content, gogui.Label(status, st))
	}

	keyStyle := t.TextStyleTitle
	tall := style.Reach() * 1.2
	pauseText := say.T("timer.pause")
	if e.Paused {
		pauseText = say.T("timer.resume")
	}
	var keys []gogui.View
	if !e.Done {
		keys = append(keys, v.splitKey(w, id+"-pause", pauseText, keyStyle, tall, v.press(e, opPause)))
	}
	keys = append(keys, v.splitKey(w, id+"-cancel", "×", keyStyle, tall, v.press(e, opCancel)))
	content = append(content, gogui.Row(gogui.ContainerCfg{
		Sizing:  gogui.FillFit,
		HAlign:  gogui.HAlignCenter,
		Padding: gogui.NoPadding,
		Spacing: gogui.SpacingLarge,
		Content: keys,
	}))
	if mark != "" {
		st := t.TextStyleTitle
		st.Color = widgets.Color(pal.Muted)
		content = append(content, gogui.Label(mark, st))
	}

	return gogui.Column(gogui.ContainerCfg{
		ID:      id,
		Width:   cell.W,
		Height:  cell.H,
		Sizing:  gogui.FixedFixed,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Padding: gogui.PaddingLarge,
		Spacing: gogui.SpacingMedium,
		Clip:    true,
		Content: content,
	})
}

func (v *View) splitKey(
	w *gogui.Window,
	id, text string,
	st gogui.TextStyle,
	tall float32,
	do func(),
) gogui.View {
	cfg := stopShape(w, text, st, tall, tall*2.4)
	cfg.ID = id
	cfg.Color = widgets.KeyFill(v.UI.Palette())
	return v.UI.Toolkit.Keyed(cfg, text, st, style.Rest, func(gogui.EventCtx) { do() })
}
