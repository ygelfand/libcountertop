package settings

import (
	"cmp"
	"slices"
	"strings"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/say"
)

type settingsHit struct {
	label, hint, glyph, path string
	reveal                   string
	route                    []func() shell.View
	root                     *shell.Page
}

func indexSettings(root *shell.Page) []settingsHit {
	var hits []settingsHit
	var visit func(*shell.Page, []string, []func() shell.View, []*shell.Page)
	visit = func(page *shell.Page, path []string, route []func() shell.View, parents []*shell.Page) {
		if page == nil || len(parents) >= 8 || slices.Contains(parents, page) {
			return
		}
		var rows []menu.Row
		if page.Build != nil {
			rows, _ = page.Build()
		} else if page.Tiles != nil {
			cells, _ := page.Tiles()
			for _, cell := range cells {
				if cell.Editor != nil {
					rows = append(rows, menu.Row{Label: cell.Label, Open: cell.Editor})
				}
			}
		}
		for _, row := range rows {
			if row.Label == "" || row.Label == say.T("settings.search") {
				continue
			}
			hit := settingsHit{
				label:  row.Label,
				hint:   row.Hint,
				glyph:  row.Glyph,
				path:   strings.Join(path, " › "),
				route:  slices.Clone(route),
				root:   root,
				reveal: row.Label,
			}
			if row.Open != nil && !row.Dim {
				hit.route = append(hit.route, row.Open)
				hit.reveal = ""
			}
			hits = append(hits, hit)
			if row.Open != nil && !row.Dim {
				if next := shell.SettingsPage(row.Open()); next != nil {
					visit(
						next,
						append(slices.Clone(path), row.Label),
						append(slices.Clone(route), row.Open),
						append(slices.Clone(parents), page),
					)
				}
			}
		}
	}
	visit(root, nil, nil, nil)
	return hits
}

func findSettings(index []settingsHit, query string) []settingsHit {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil
	}
	var hits []settingsHit
	for _, hit := range index {
		text := strings.ToLower(hit.label + " " + hit.hint + " " + hit.path)
		if slices.ContainsFunc(
			terms,
			func(term string) bool { return !strings.Contains(text, term) },
		) {
			continue
		}
		hits = append(hits, hit)
	}
	query = strings.Join(terms, " ")
	rank := func(h settingsHit) int {
		label := strings.ToLower(h.label)
		if label == query {
			return 0
		}
		if strings.HasPrefix(label, query) {
			return 1
		}
		if strings.Contains(label, query) {
			return 2
		}
		return 3
	}
	slices.SortStableFunc(hits, func(a, b settingsHit) int {
		if c := cmp.Compare(rank(a), rank(b)); c != 0 {
			return c
		}
		if c := cmp.Compare(a.label, b.label); c != 0 {
			return c
		}
		return cmp.Compare(a.path, b.path)
	})
	return hits
}

func (hit settingsHit) open(stack *shell.Shell) {
	var pages []shell.View
	for _, next := range hit.route {
		page := next()
		if page == nil {
			return
		}
		pages = append(pages, page)
	}
	var target shell.View = hit.root
	if len(pages) > 0 {
		target = pages[len(pages)-1]
	}
	if page := shell.SettingsPage(target); page != nil {
		page.Reveal = hit.reveal
	}
	for stack.Top() != nil && stack.Top() != hit.root {
		stack.Pop()
	}
	for _, next := range pages {
		stack.Push(next)
	}
}

func searchPage(root *shell.Page, stack *shell.Shell, search *shell.Search) *shell.Page {
	index := indexSettings(root)
	return &shell.Page{
		Title: say.T("settings.search"),
		Build: func() ([]menu.Row, []func(int)) {
			var rows []menu.Row
			var acts []func(int)
			for _, hit := range findSettings(index, *search.Query) {
				rows = append(
					rows,
					menu.Row{
						Glyph: hit.glyph,
						Label: hit.label,
						Hint:  hit.path,
						Kind:  menu.Chevron,
					},
				)
				acts = append(acts, func(int) { hit.open(stack) })
			}
			if len(rows) == 0 {
				label := "settings.search.empty"
				if strings.TrimSpace(*search.Query) != "" {
					label = "settings.search.none"
				}
				rows = append(rows, menu.Row{Label: say.T(label), Dim: true})
				acts = append(acts, nil)
			}
			return rows, acts
		},
	}
}
