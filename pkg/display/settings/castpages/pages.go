package castpages

import (
	"slices"
	"strings"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/media/sponsorblock"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

type Receiver interface {
	SetReceiver(on bool)
	SetSkip(categories string)
}

type Dependencies struct {
	Settings storage.Section[schema.Cast]
	Shell    *shell.Shell
	Receiver Receiver
}
type Pages struct{ options Dependencies }

func New(o Dependencies) *Pages { return &Pages{options: o} }

func (p *Pages) Cast() *shell.Page {
	return &shell.Page{
		Title: say.T("features.cast"),
		Build: func() ([]menu.Row, []func(int)) {
			c := p.options.Settings.Read()
			return []menu.Row{
					{Label: say.T("cast.enable"), Kind: menu.Toggle, On: c.Receiver},
					{
						Label: say.T("youtube.skip"),
						Open:  func() shell.View { return p.Sponsor() },
						Kind:  menu.Chevron,
						Value: SkipSays(c.YouTube.Skip),
					},
				}, []func(int){
					func(int) { p.options.Receiver.SetReceiver(!c.Receiver) },
					nil,
				}
		},
	}
}

func (p *Pages) Sponsor() *shell.Page {
	return &shell.Page{
		Title: say.T("youtube.skip"),
		Build: func() ([]menu.Row, []func(int)) {
			chosen := p.options.Settings.Read().YouTube.Skip
			rows := make([]menu.Row, 0, len(sponsorblock.Categories))
			acts := make([]func(int), 0, len(sponsorblock.Categories))
			for _, c := range sponsorblock.Categories {
				on := slices.Contains(chosen, c)
				rows = append(
					rows,
					menu.Row{Label: say.T("youtube.category." + c), Kind: menu.Toggle, On: on},
				)
				acts = append(acts, func(int) {
					next := slices.DeleteFunc(
						slices.Clone(chosen),
						func(x string) bool { return x == c },
					)
					if !on {
						next = append(next, c)
					}
					p.options.Receiver.SetSkip(strings.Join(next, ","))
				})
			}
			return rows, acts
		},
	}
}

func SkipSays(skip []string) string {
	if len(skip) == 0 {
		return say.T("youtube.skip.none")
	}
	return say.F("youtube.skip.some", map[string]any{"N": len(skip)})
}
