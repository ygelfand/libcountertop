package poster

import (
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type (
	Setting  = setting.Setting[config.Poster]
	Table    = setting.Table[config.Poster]
	Controls = setting.Controls[config.Poster]
)

const Group setting.Group = "Poster"

func NewTable() *Table {
	var t *Table
	t = setting.NewTable(
		"poster",
		[]setting.Group{Group},
		rows(func() *Table { return t }),
		setting.Messages{Text: say.T, Missing: say.Missing},
	)
	return t
}

func rows(table func() *Table) []Setting {
	return []Setting{
		{
			Name: "on", Group: Group, ID: "poster", Icon: "mdi:image-frame",
			Kind: setting.Toggle,
			Read: func(c *config.Poster) string { return setting.OnOff(c.Enabled) },
			Write: func(c *config.Poster, v string) error {
				on, ok := setting.Boolean(v)
				if !ok {
					return table().MustRow("on").Bad(v, "on or off")
				}
				c.Enabled = on
				return nil
			},
		},
		setting.Pick(
			Setting{Name: "every", Group: Group, Icon: "mdi:image-sync"},
			config.PosterEveries(),
			func(c *config.Poster) *config.PosterEvery { return &c.Every },
		),
		setting.Pick(
			Setting{Name: "fit", Group: Group, Icon: "mdi:fit-to-screen"},
			config.PhotoFits(),
			func(c *config.Poster) *config.PhotoFit { return &c.Fit },
		),
		setting.Pick(
			Setting{Name: "transition", Group: Group, Icon: "mdi:transition"},
			config.PhotoTransitions(),
			func(c *config.Poster) *config.PhotoTransition { return &c.Change },
		),
	}
}
