package widgets

import (
	"errors"
	"testing"
	"time"

	gui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/theme"

	"github.com/ygelfand/libcountertop/pkg/input/textentry"
)

func TestRemoteFieldRejectsStaleEdits(t *testing.T) {
	v := textentry.View{Target: "first", Text: "draft", Revision: 2}
	for _, r := range []textentry.Request{
		{Target: "other", Revision: 2, Action: "replace"},
		{Target: "first", Revision: 1, Action: "replace"},
	} {
		if err := validateEdit(v, r); err == nil {
			t.Fatal("stale edit accepted")
		}
	}
	if err := validateEdit(
		v,
		textentry.Request{Target: "first", Revision: 2, Action: "replace"},
	); err != nil {
		t.Fatal(err)
	}
	if err := validateEdit(
		v,
		textentry.Request{Target: "first", Revision: 2, Action: "unknown"},
	); !errors.Is(
		err,
		textentry.ErrInvalid,
	) {
		t.Fatalf("unknown action = %v", err)
	}
}

func TestRemoteEditingUsesFocusedField(t *testing.T) {
	e := NewEditor()
	w := gui.SimpleWindow("remote", 800, 600, &struct{}{}, func(*gui.Window) {})
	text := "old"
	saved := ""
	view := func(w *gui.Window) gui.View {
		return e.Field(style.ByName("standard").Kit, gui.InputCfg{
			ID: "field", Text: text, Sizing: gui.FillFit,
			OnTextChanged: func(s string, _ gui.EventCtx) { text = s },
		}, func() { saved = text })
	}
	w.TestRender(view)
	if err := w.TestClick("field"); err != nil {
		t.Fatal(err)
	}
	w.TestRender(nil)
	v, err := e.Remote(w, textentry.Request{})
	if err != nil || v.Target == "" {
		t.Fatalf("focused = %+v, %v", v, err)
	}
	v, err = e.Remote(
		w,
		textentry.Request{
			Target:   v.Target,
			Revision: v.Revision,
			Action:   "replace",
			Text:     "hello 🦋",
		},
	)
	if err != nil || text != "hello 🦋" || v.Text != text {
		t.Fatalf("replace = %q, %+v, %v", text, v, err)
	}
	w.TestRender(nil)
	_, err = e.Remote(w, textentry.Request{Action: "key", Key: "Backspace"})
	if err != nil || text != "hello " {
		t.Fatalf("backspace = %q, %v", text, err)
	}
	w.TestRender(nil)
	v, err = e.Remote(w, textentry.Request{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Remote(w, textentry.Request{Target: v.Target, Revision: v.Revision, Action: "save"})
	if err != nil || saved != "hello " {
		t.Fatalf("save = %q, %v", saved, err)
	}
	if _, err := e.Remote(w, textentry.Request{}); !errors.Is(err, textentry.ErrUnavailable) {
		t.Fatalf("closed = %v", err)
	}
	w.TestRender(nil)
	if err := w.TestClick("field"); err != nil {
		t.Fatal(err)
	}
	w.TestRender(nil)
	reopened, err := e.Remote(w, textentry.Request{})
	if err != nil || reopened.Target == v.Target {
		t.Fatalf("reopened = %+v, %v", reopened, err)
	}
	if _, err := e.Remote(
		w,
		textentry.Request{Target: v.Target, Revision: v.Revision, Action: "replace", Text: "stale"},
	); !errors.Is(
		err,
		textentry.ErrTarget,
	) {
		t.Fatalf("old field link = %v", err)
	}
}

func TestKeyboardQRKeepsTheFieldFocused(t *testing.T) {
	e := NewEditor()
	w := gui.SimpleWindow("keyboard", 800, 600, &struct{}{}, func(*gui.Window) {})
	kit := style.ByName("standard").Kit
	paired := ""
	view := func(w *gui.Window) gui.View {
		field := e.Field(
			kit,
			gui.InputCfg{ID: "field", Text: "text", Sizing: gui.FillFit},
			func() {},
		)
		content := []gui.View{field}
		if e.Typing(w) {
			_, keys := e.Keyboard(w, KeyboardOptions{
				Size: KeyboardStandard, Palette: theme.Theme{}, Kit: kit,
				Cancel: "Cancel", Save: "Save", Type: func(rune) {}, Press: func(gui.KeyCode) {},
				Pair: func(target string) (string, time.Time, error) {
					paired = target
					return "https://192.168.1.25/#control=code", time.Now().Add(-time.Second), nil
				},
			})
			content = append(content, keys)
		}
		return gui.Column(gui.ContainerCfg{Sizing: gui.FillFill, Content: content})
	}
	w.TestRender(view)
	if err := w.TestClick("field"); err != nil {
		t.Fatal(err)
	}
	w.TestRender(nil)
	w.TestRender(nil)
	ids := w.ResolveID("key-phone")
	if len(ids) != 1 {
		t.Fatalf("phone keys = %v, typing=%v, target=%q", ids, e.Typing(w), e.remote.Target)
	}
	if err := w.TestClick(ids[0]); err != nil {
		t.Fatal(err)
	}
	if !e.Typing(w) || paired == "" || paired != e.remote.Target || e.pairURL == "" {
		t.Fatalf(
			"QR lost its field: paired=%q, target=%q, typing=%v",
			paired,
			e.remote.Target,
			e.Typing(w),
		)
	}
	ids = w.ResolveID("key-keyboard")
	if len(ids) != 1 {
		t.Fatalf("back keys = %v", ids)
	}
	if err := w.TestClick(ids[0]); err != nil {
		t.Fatal(err)
	}
	if !e.Typing(w) || e.pairURL != "" {
		t.Fatal("returning to keyboard lost its field")
	}
}
