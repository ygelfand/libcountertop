package castreceiver

import (
	"strings"

	"github.com/ygelfand/libcountertop/pkg/media/sponsorblock"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type (
	Setting  = setting.Setting[schema.Cast]
	Table    = setting.Table[schema.Cast]
	Controls = setting.Controls[schema.Cast]
)

const (
	Group = setting.Group("Cast")

	loungeAlways = "always"
	loungeDemand = "demand"
)

func NewTable() *Table {
	var t *Table
	t = setting.NewTable(
		"cast",
		[]setting.Group{Group},
		rows(func() *Table { return t }),
		setting.Messages{Text: say.T, Missing: say.Missing},
	)
	return t
}

func rows(table func() *Table) []Setting {
	toggle := func(name string, group setting.Group, icon string, field func(*schema.Cast) *bool) Setting {
		return Setting{
			Name: name, Group: group, Icon: icon, Kind: setting.Toggle,
			Read: func(c *schema.Cast) string { return setting.OnOff(*field(c)) },
			Write: func(c *schema.Cast, v string) error {
				on, ok := setting.Boolean(v)
				if !ok {
					return table().MustRow(name).Bad(v, "on or off")
				}
				*field(c) = on
				return nil
			},
		}
	}
	return []Setting{
		toggle("receiver", Group, "mdi:cast", func(c *schema.Cast) *bool { return &c.Receiver }),
		{
			Name: "youtube.skip", Group: Group, ID: "youtube_sponsorblock",
			Icon: "mdi:debug-step-over", Kind: setting.Text,
			Read: func(c *schema.Cast) string { return strings.Join(c.YouTube.Skip, ",") },
			Write: func(c *schema.Cast, v string) error {
				c.YouTube.Skip = sponsorblock.ParseCategories(v)
				if c.YouTube.Skip == nil {
					c.YouTube.Skip = []string{}
				}
				return nil
			},
		},
		{
			Name: "youtube.lounge", Group: Group, Icon: "mdi:youtube", Kind: setting.Choice,
			Options: []setting.Option{
				{Value: loungeAlways, Key: loungeAlways},
				{Value: loungeDemand, Key: loungeDemand},
			},
			Read: func(c *schema.Cast) string {
				if c.YouTube.OnDemand {
					return loungeDemand
				}
				return loungeAlways
			},
			Write: func(c *schema.Cast, v string) error {
				switch v {
				case loungeAlways:
					c.YouTube.OnDemand = false
				case loungeDemand:
					c.YouTube.OnDemand = true
				default:
					return table().MustRow("youtube.lounge").Bad(v, "")
				}
				return nil
			},
		},
	}
}
