package widgets

import (
	"runtime"
	"strings"
	"unicode/utf8"

	gui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/input/textentry"
)

func validateEdit(v textentry.View, r textentry.Request) error {
	if v.Target == "" || r.Target != v.Target {
		return textentry.ErrTarget
	}
	if r.Revision != v.Revision {
		return textentry.ErrConflict
	}
	if len(r.Text) > textentry.TextMost || !utf8.ValidString(r.Text) ||
		strings.ContainsRune(r.Text, 0) {
		return textentry.ErrInvalid
	}
	switch r.Action {
	case "replace", "save", "cancel":
		return nil
	default:
		return textentry.ErrInvalid
	}
}

var remoteKeys = map[string]gui.KeyCode{
	"SelectAll": gui.KeyA,
	"Backspace": gui.KeyBackspace, "Delete": gui.KeyDelete,
	"Enter": gui.KeyEnter, "Escape": gui.KeyEscape,
	"ArrowLeft": gui.KeyLeft, "ArrowRight": gui.KeyRight,
	"ArrowUp": gui.KeyUp, "ArrowDown": gui.KeyDown,
	"Home": gui.KeyHome, "End": gui.KeyEnd,
}

func shortcut() gui.Modifier {
	if runtime.GOOS == "darwin" {
		return gui.ModSuper
	}
	return gui.ModCtrl
}

func pressRemote(w *gui.Window, key gui.KeyCode, mod gui.Modifier) {
	w.EventFn(&gui.Event{Type: gui.EventKeyDown, KeyCode: key, Modifiers: mod})
	w.EventFn(&gui.Event{Type: gui.EventKeyUp, KeyCode: key, Modifiers: mod})
}

func (e *Editor) Remote(w *gui.Window, r textentry.Request) (textentry.View, error) {
	if !e.Typing(w) || e.remote.Target == "" {
		return textentry.View{}, textentry.ErrUnavailable
	}
	if r.Action == "" {
		if r.Target != "" && r.Target != e.remote.Target {
			return textentry.View{}, textentry.ErrTarget
		}
		return e.remote, nil
	}
	if r.Action == "type" || r.Action == "key" {
		if len(r.Text) > textentry.TextMost || !utf8.ValidString(r.Text) ||
			strings.ContainsRune(r.Text, 0) {
			return textentry.View{}, textentry.ErrInvalid
		}
		if r.Action == "key" {
			key, ok := remoteKeys[r.Key]
			if !ok {
				return textentry.View{}, textentry.ErrInvalid
			}
			switch key {
			case gui.KeyEnter:
				e.Save()
				w.ClearFocus()
			case gui.KeyEscape:
				e.Cancel()
				w.ClearFocus()
			default:
				mod := gui.ModNone
				if r.Key == "SelectAll" {
					mod = shortcut()
				}
				pressRemote(w, key, mod)
			}
		} else {
			for _, ch := range r.Text {
				w.EventFn(&gui.Event{Type: gui.EventChar, CharCode: uint32(ch)})
			}
		}
		w.InvalidateLayout()
		return e.remote, nil
	}
	if err := validateEdit(e.remote, r); err != nil {
		return textentry.View{}, err
	}
	switch r.Action {
	case "replace":
		pressRemote(w, gui.KeyA, shortcut())
		pressRemote(w, gui.KeyBackspace, gui.ModNone)
		for _, ch := range r.Text {
			w.EventFn(&gui.Event{Type: gui.EventChar, CharCode: uint32(ch)})
		}
	case "save":
		e.Save()
		w.ClearFocus()
	case "cancel":
		e.Cancel()
		w.ClearFocus()
	}
	w.InvalidateLayout()
	return e.remote, nil
}
