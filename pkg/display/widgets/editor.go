package widgets

import (
	"crypto/rand"
	"time"

	gui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/interaction"
	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/input/textentry"
)

type FieldStyle interface {
	Field(cfg *gui.InputCfg)
}

type Editor struct {
	drafts    map[string]string
	revealed  map[string]struct{}
	Presses   *interaction.Presses
	leaf      string
	save      func()
	Top       float32
	Shift     bool
	Symbols   bool
	remote    textentry.View
	pairURL   string
	pairUntil time.Time
	pairError string
}

func NewEditor() *Editor {
	return &Editor{drafts: make(map[string]string), revealed: make(map[string]struct{})}
}

func focused(w *gui.Window, leaf string) bool {
	for _, id := range w.ResolveID(leaf) {
		if w.IsFocus(id) {
			return true
		}
	}
	return false
}

func (e *Editor) Typing(w *gui.Window) bool { return e.leaf != "" && focused(w, e.leaf) }

func (e *Editor) Editing(w *gui.Window, id string) bool { return focused(w, id) }

func (e *Editor) Save() {
	if e.save != nil {
		e.save()
	}
}

func (e *Editor) Cancel() { delete(e.drafts, e.leaf) }

func (e *Editor) keep(fid string, save func(string)) {
	s, ok := e.drafts[fid]
	delete(e.drafts, fid)
	if ok && save != nil {
		save(s)
	}
}

func (e *Editor) Clear(w *gui.Window, bottom float32) {
	if e.Top <= 0 || bottom <= e.Top-style.Reach()*0.3 {
		return
	}
	by := bottom - e.Top + style.Reach()*0.5
	w.QueueCommand(
		func(w *gui.Window) { w.ScrollVerticalTo("page", w.ScrollVerticalOffset("page")-by) },
	)
}

func (e *Editor) Field(kit FieldStyle, cfg gui.InputCfg, save func()) gui.View {
	kit.Field(&cfg)
	leaf, blur, changed := cfg.ID, cfg.OnBlur, cfg.OnTextChanged
	cfg.OnTextChanged = func(text string, ev gui.EventCtx) {
		if e.leaf == leaf && e.remote.Text != text {
			e.remote.Text = text
			e.remote.Revision++
		}
		if changed != nil {
			changed(text, ev)
		}
	}
	cfg.OnBlur = func(ev gui.EventCtx) {
		if e.leaf == leaf {
			e.leaf, e.save = "", nil
			e.remote, e.pairURL = textentry.View{}, ""
			e.pairError = ""
		}
		if blur != nil {
			blur(ev)
		}
	}
	return gui.Column(gui.ContainerCfg{
		Sizing: cfg.Sizing, Padding: gui.NoPadding,
		AmendLayout: func(ev gui.EventCtx) {
			if focused(ev.Window, leaf) {
				if e.leaf != leaf || e.remote.Target == "" {
					e.remote = textentry.View{
						Target:   rand.Text(),
						Text:     cfg.Text,
						Password: cfg.IsPassword,
					}
					e.pairURL = ""
					e.pairError = ""
				}
				if cfg.ReadOnly {
					e.remote.Target = ""
				}
				if e.remote.Text != cfg.Text {
					e.remote.Text = cfg.Text
					e.remote.Revision++
				}
				e.leaf, e.save = leaf, save
				e.Clear(ev.Window, ev.Layout.Shape.Y+ev.Layout.Shape.Height)
			}
		},
		Content: []gui.View{gui.Input(cfg)},
	})
}

func (e *Editor) Row(kit FieldStyle, id string, r menu.Row) gui.View {
	fid := id + "-field"
	text, ok := e.drafts[fid]
	if !ok {
		text = r.Value
	}
	_, shown := e.revealed[fid]
	field := e.Field(kit, gui.InputCfg{
		ID:         fid,
		Text:       text,
		IsPassword: r.Kind == menu.Password && !shown,
		Sizing:     gui.FillFit,
		OnTextChanged: func(s string, ev gui.EventCtx) {
			e.drafts[fid] = s
			ev.Window.InvalidateLayout()
		},
		OnTextCommit: func(_ string, _ gui.InputCommitReason, ev gui.EventCtx) {
			e.keep(fid, r.Save)
			ev.Window.InvalidateLayout()
		},
	}, func() { e.keep(fid, r.Save) })
	if r.Kind != menu.Password {
		return field
	}
	return gui.Row(gui.ContainerCfg{
		Sizing:  gui.FillFit,
		VAlign:  gui.VAlignMiddle,
		Padding: gui.NoPadding,
		Spacing: gui.SpacingSmall,
		Content: []gui.View{field, e.eye(fid, shown)},
	})
}

func (e *Editor) eye(fid string, shown bool) gui.View {
	glyph := "mdi:eye"
	if shown {
		glyph = "mdi:eye-off"
	}
	return e.Presses.Pressable(gui.Row, gui.ContainerCfg{
		ID:      fid + "-eye",
		Width:   style.Reach(),
		Height:  style.Reach(),
		Sizing:  gui.FixedFixed,
		HAlign:  gui.HAlignCenter,
		VAlign:  gui.VAlignMiddle,
		Padding: gui.NoPadding,
		Content: []gui.View{icon(glyph, gui.CurrentTheme().Cfg.ColorTextSecondary)},
	}, func(ev gui.EventCtx) {
		if shown {
			delete(e.revealed, fid)
		} else {
			e.revealed[fid] = struct{}{}
		}
		ev.Window.InvalidateLayout()
	})
}
