package settings

import (
	"testing"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
)

func TestSearchFindsNestedSettingsWithoutRunningActions(t *testing.T) {
	changed := 0
	opened := 0
	night := &shell.Page{
		Title:  "Night",
		OnOpen: func() { opened++ },
		Build: func() ([]menu.Row, []func(int)) {
			return []menu.Row{
				{Label: "Light threshold", Hint: "Ambient brightness", Kind: menu.Slider},
				{Label: "Password", Value: "secret-value", Kind: menu.Password},
			}, []func(int){func(int) { changed++ }, nil}
		},
	}
	display := &shell.Page{Title: "Display", Build: func() ([]menu.Row, []func(int)) {
		return []menu.Row{
			{Label: "Night", Kind: menu.Chevron, Open: func() shell.View { return night }},
		}, nil
	}}
	root := &shell.Page{Title: "Settings", Build: func() ([]menu.Row, []func(int)) {
		return []menu.Row{
			{Label: "Display", Kind: menu.Chevron, Open: func() shell.View { return display }},
		}, nil
	}}
	index := indexSettings(root)
	for _, query := range []string{"light", "NIGHT threshold", "ambient brightness", "display light"} {
		results := findSettings(index, query)
		if len(results) != 1 || results[0].label != "Light threshold" ||
			results[0].path != "Display › Night" {
			t.Fatalf("%q = %+v", query, results)
		}
	}
	if results := findSettings(index, "secret-value"); len(results) != 0 {
		t.Fatal("searched a saved password")
	}
	if results := findSettings(index, " "); len(results) != 0 {
		t.Fatal("empty query returned settings")
	}
	if changed != 0 || opened != 0 {
		t.Fatalf("index ran %d actions and opened %d pages", changed, opened)
	}
	stack := shell.New()
	stack.Push(root)
	stack.Push(&shell.Page{Title: "Search"})
	findSettings(index, "threshold")[0].open(stack)
	if stack.Top() != night || night.Reveal != "Light threshold" {
		t.Fatalf("opened %+v", stack.Top())
	}
	if changed != 0 {
		t.Fatal("opening a result changed a setting")
	}
	if opened != 1 {
		t.Fatalf("opened=%d", opened)
	}
	stack.Pop()
	if stack.Top() != display {
		t.Fatalf("back opened %+v", stack.Top())
	}
}

func TestSearchPageFiltersFieldsAsYouType(t *testing.T) {
	root := &shell.Page{Title: "Settings", Build: func() ([]menu.Row, []func(int)) {
		return []menu.Row{
			{Label: "Brightness", Kind: menu.Slider},
			{Label: "Device name", Kind: menu.Field},
		}, nil
	}}
	query := ""
	search := &shell.Search{Query: &query}
	page := searchPage(root, shell.New(), search)
	query = "device"
	rows, _ := page.Build()
	if len(rows) != 1 || rows[0].Label != "Device name" {
		t.Fatalf("results = %+v", rows)
	}
}

func TestSearchQuerySurvivesNavigationAndReopening(t *testing.T) {
	query := "brightness"
	stack := shell.New()
	o := RootDependencies{Shell: stack, SearchQuery: &query}
	first := RootPage(o)
	stack.Push(first)
	stack.Push(&shell.Page{Title: "Display"})
	if stack.Search() != first.Search || *stack.Search().Query != query {
		t.Fatal("child page lost global search")
	}
	stack.Close()
	if stack.Search() != nil {
		t.Fatal("search escaped the settings stack")
	}
	second := RootPage(o)
	stack.Push(second)
	if first.Search == second.Search || *stack.Search().Query != "brightness" {
		t.Fatal("query or page context was not retained correctly")
	}
}

func TestSearchOpensThePageResultItself(t *testing.T) {
	child := &shell.Page{Title: "Display"}
	root := &shell.Page{Title: "Settings", Build: func() ([]menu.Row, []func(int)) {
		return []menu.Row{{Label: "Display", Open: func() shell.View { return child }}}, nil
	}}
	stack := shell.New()
	stack.Push(root)
	stack.Push(&shell.Page{})
	findSettings(indexSettings(root), "display")[0].open(stack)
	if stack.Top() != child {
		t.Fatalf("opened %+v", stack.Top())
	}
}

func TestSearchStopsCyclesAndUsesVisibleRows(t *testing.T) {
	var page *shell.Page
	page = &shell.Page{Title: "Loop", Build: func() ([]menu.Row, []func(int)) {
		return []menu.Row{{Label: "Loop", Open: func() shell.View { return page }}}, nil
	}}
	if got := indexSettings(page); len(got) > 2 {
		t.Fatalf("cycle produced %d results", len(got))
	}
}

type settingsView struct{ page *shell.Page }

func (*settingsView) Covers() bool                { return true }
func (v *settingsView) SettingsPage() *shell.Page { return v.page }

func TestSearchUsesTheSameModelForCustomSettingsViews(t *testing.T) {
	view := &settingsView{
		page: &shell.Page{Title: "Dashboard", Build: func() ([]menu.Row, []func(int)) {
			return []menu.Row{{Label: "Clock face", Kind: menu.Chevron}}, nil
		}},
	}
	root := &shell.Page{Title: "Settings", Build: func() ([]menu.Row, []func(int)) {
		return []menu.Row{{Label: "Dashboard", Open: func() shell.View { return view }}}, nil
	}}
	results := findSettings(indexSettings(root), "clock")
	if len(results) != 1 {
		t.Fatalf("results=%+v", results)
	}
	stack := shell.New()
	stack.Push(root)
	stack.Push(&shell.Page{})
	results[0].open(stack)
	if stack.Top() != view || view.page.Reveal != "Clock face" {
		t.Fatalf("opened=%+v, reveal=%q", stack.Top(), view.page.Reveal)
	}
}

func TestSearchFindsFieldsBehindATileEditor(t *testing.T) {
	editor := &shell.Page{Title: "Custom theme", Build: func() ([]menu.Row, []func(int)) {
		return []menu.Row{{Label: "Accent", Kind: menu.Field}}, nil
	}}
	root := &shell.Page{Title: "Themes", Tiles: func() ([]menu.Cell, []func(int)) {
		return []menu.Cell{{Label: "Custom", Editor: func() shell.View { return editor }}}, nil
	}}
	results := findSettings(indexSettings(root), "accent")
	if len(results) != 1 || results[0].path != "Custom" {
		t.Fatalf("results=%+v", results)
	}
}
