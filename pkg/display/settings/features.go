package settings

import (
	"cmp"
	"slices"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/say"
)

type Feature struct {
	Key  string
	On   bool
	Set  func(bool)
	Page func() shell.View
}

func FeaturesPage(read func() []Feature, push func(shell.View)) *shell.Page {
	return &shell.Page{Title: say.T("features.title"), Build: func() ([]menu.Row, []func(int)) {
		all := slices.Clone(read())
		order := map[string]int{
			"features.sendspin": 1,
			"features.cast":     2,
			"features.airplay":  3,
			"call.settings":     4,
			"features.proxy":    5,
			"features.speaker":  6,
			"features.rtsp":     7,
		}
		slices.SortStableFunc(all, func(a, b Feature) int {
			return cmp.Or(cmp.Compare(order[a.Key], order[b.Key]), cmp.Compare(a.Key, b.Key))
		})
		rows := []menu.Row{}
		acts := []func(int){}
		for _, f := range all {
			row := menu.Row{
				Label: say.T(f.Key),
				Hint:  say.T(f.Key + ".hint"),
				Kind:  menu.Toggle,
				On:    f.On,
			}
			act := func(int) { f.Set(!f.On) }
			if f.Page != nil {
				row.Kind = menu.Chevron
				row.Open = f.Page
				act = nil
			}
			rows = append(rows, row)
			acts = append(acts, act)
		}
		return rows, acts
	}}
}
