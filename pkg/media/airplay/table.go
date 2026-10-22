package airplay

import (
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type (
	Setting  = setting.Setting[schema.AirPlay]
	Table    = setting.Table[schema.AirPlay]
	Controls = setting.Controls[schema.AirPlay]
)

const Group setting.Group = "AirPlay"

func NewTable() *Table {
	var t *Table
	t = setting.NewTable("media", []setting.Group{Group}, []Setting{
		{
			Name: "airplay", Group: Group, ID: "airplay", Icon: "mdi:apple-airplay",
			Kind: setting.Toggle,
			Read: func(c *schema.AirPlay) string { return setting.OnOff(c.Enabled) },
			Write: func(c *schema.AirPlay, v string) error {
				on, ok := setting.Boolean(v)
				if !ok {
					return t.MustRow("airplay").Bad(v, "on or off")
				}
				c.Enabled = on
				return nil
			},
		},
	}, setting.Messages{Text: say.T, Missing: say.Missing})
	return t
}
