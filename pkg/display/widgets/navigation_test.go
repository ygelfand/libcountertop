package widgets

import (
	"fmt"
	"testing"

	gui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type navigationSettings struct{}

func (navigationSettings) Read() schema.Screen               { return schema.Screen{} }
func (navigationSettings) Update(func(*schema.Screen)) error { return nil }

func TestNavigationRowOwnsItsDestination(t *testing.T) {
	u := NewUI(navigationSettings{})
	u.Toolkit.Pressable = func(layout func(gui.ContainerCfg) gui.View, cfg gui.ContainerCfg, click func(gui.EventCtx)) gui.View {
		if click != nil {
			cfg.OnClick = func(e gui.EventCtx) { click(e); e.Consume() }
		}
		return layout(cfg)
	}
	stack := shell.New()
	u.Open = stack.Push
	created := 0
	next := &shell.Page{Title: "Display"}
	page := &shell.Page{Title: "Settings", Build: func() ([]menu.Row, []func(int)) {
		return []menu.Row{
			{Label: "Display", Open: func() shell.View { created++; return next }},
		}, nil
	}}
	w := gui.SimpleWindow("navigation", 800, 600, &struct{}{}, func(*gui.Window) {})
	w.TestRender(
		func(w *gui.Window) gui.View { return PageBody(w, page, PageRenderer{Row: u.Row}) },
	)
	if created != 0 {
		t.Fatal("rendering opened a page")
	}
	if err := w.TestClick("row-0"); err != nil {
		t.Fatal(err)
	}
	if stack.Top() != next || created != 1 {
		t.Fatalf("opened=%v, created=%d", stack.Top(), created)
	}
}

func TestSearchRevealScrollsToTheMatchingRow(t *testing.T) {
	u := NewUI(navigationSettings{})
	page := &shell.Page{
		Title:  "Settings",
		Reveal: "Setting 19",
		Build: func() ([]menu.Row, []func(int)) {
			var rows []menu.Row
			for i := range 20 {
				rows = append(rows, menu.Row{Label: fmt.Sprintf("Setting %d", i)})
			}
			return rows, nil
		},
	}
	w := gui.SimpleWindow("reveal", 800, 480, &struct{}{}, func(*gui.Window) {})
	w.TestRender(func(w *gui.Window) gui.View {
		return u.Toolkit.PageFrame(
			w,
			FrameOptions{Title: page.Title, Back: func() {}, Build: func(w *gui.Window) gui.View {
				return PageBody(w, page, PageRenderer{Row: u.Row})
			}},
		)
	})
	w.TestRender(nil)
	w.FrameFn()
	w.TestRender(nil)
	if page.Reveal != "" || w.ScrollVerticalOffset("page") >= 0 {
		t.Fatalf("reveal=%q, offset=%v", page.Reveal, w.ScrollVerticalOffset("page"))
	}
	ids := w.ResolveID("row-19")
	if len(ids) != 1 {
		t.Fatalf("target rows = %v", ids)
	}
	if _, y, ok := w.LocalToWindow(ids[0], 0, 0); !ok || y < 0 || y >= 480 {
		t.Fatalf("target y=%v, found=%v", y, ok)
	}
}

func TestSearchHeaderUsesAPlaceholderAndTheNativeEditor(t *testing.T) {
	u := NewUI(navigationSettings{})
	query := ""
	w := gui.SimpleWindow("search", 800, 600, &struct{}{}, func(*gui.Window) {})
	w.TestRender(func(w *gui.Window) gui.View {
		return u.Toolkit.PageFrame(w, FrameOptions{
			Title: "Display",
			Back:  func() {},
			Search: u.Input(
				gui.InputCfg{
					ID:            "settings-search",
					Text:          query,
					Placeholder:   "Search settings",
					Sizing:        gui.FillFit,
					OnTextChanged: func(value string, _ gui.EventCtx) { query = value },
				},
				nil,
			),
			Build: func(*gui.Window) gui.View { return gui.Label("Brightness", gui.TextStyle{}) },
		})
	})
	if err := w.TestClick("settings-search"); err != nil {
		t.Fatal(err)
	}
	w.TestRender(nil)
	if !u.Editor.Typing(w) {
		t.Fatal("search did not focus the text field")
	}
	if err := w.TestType("settings-search", "brightness"); err != nil {
		t.Fatal(err)
	}
	if query != "brightness" {
		t.Fatalf("query=%q", query)
	}
}
