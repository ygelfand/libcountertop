package sendspin

import (
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type (
	Setting  = setting.Setting[schema.Sendspin]
	Table    = setting.Table[schema.Sendspin]
	Controls = setting.Controls[schema.Sendspin]
)

const Group setting.Group = "Sendspin"

func NewTable() *Table {
	var t *Table
	t = setting.NewTable("media", []setting.Group{Group}, []Setting{
		{
			Name: "sendspin", Group: Group, ID: "sendspin", Icon: "mdi:speaker-multiple",
			Kind: setting.Toggle,
			Read: func(c *schema.Sendspin) string { return setting.OnOff(c.Enabled) },
			Write: func(c *schema.Sendspin, v string) error {
				on, ok := setting.Boolean(v)
				if !ok {
					return t.MustRow("sendspin").Bad(v, "on or off")
				}
				c.Enabled = on
				return nil
			},
		},
	}, setting.Messages{Text: say.T, Missing: say.Missing})
	return t
}
