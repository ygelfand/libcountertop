package photos

import (
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const Group setting.Group = "Photos"

const (
	textMost = 1024
	listMost = 4096
)

type (
	Setting  = setting.Setting[schema.Photos]
	Table    = setting.Table[schema.Photos]
	Controls = setting.Controls[schema.Photos]
)

func NewTable() *Table {
	key := setting.Words(
		Setting{Name: "key", Group: Group, Icon: "mdi:key"},
		textMost,
		func(c *schema.Photos) *string { return &c.Key },
	)
	key.Kind = setting.Secret
	return setting.NewTable("photos", []setting.Group{Group}, []Setting{
		setting.Words(
			Setting{Name: "server", Group: Group, Icon: "mdi:server"},
			textMost,
			func(c *schema.Photos) *string { return &c.Server },
		),
		key,
		setting.Words(
			Setting{Name: "google", Group: Group, Icon: "mdi:google-photos"},
			listMost,
			func(c *schema.Photos) *string { return &c.Google },
		),
		setting.Words(
			Setting{Name: "albums", Group: Group, Icon: "mdi:image-album"},
			listMost,
			func(c *schema.Photos) *string { return &c.Albums },
		),
		setting.Words(
			Setting{Name: "tags", Group: Group, Icon: "mdi:tag-multiple"},
			listMost,
			func(c *schema.Photos) *string { return &c.Tags },
		),
		setting.Words(
			Setting{Name: "people", Group: Group, Icon: "mdi:account-group"},
			listMost,
			func(c *schema.Photos) *string { return &c.People },
		),
		setting.Flag(
			Setting{Name: "favorites", Group: Group, Icon: "mdi:heart"},
			func(c *schema.Photos) *bool { return &c.Favorites },
		),
		setting.Flag(
			Setting{Name: "memories", Group: Group, Icon: "mdi:calendar-heart"},
			func(c *schema.Photos) *bool { return &c.Memories },
		),
		setting.Pick(
			Setting{Name: "interval", Group: Group, Icon: "mdi:timer-outline"},
			schema.PhotoIntervals(),
			func(c *schema.Photos) *schema.Delay { return &c.Interval },
		),
		setting.Flag(
			Setting{Name: "motion.dashboard", Group: Group, Icon: "mdi:pan"},
			func(c *schema.Photos) *bool { return &c.Motion.Dashboard },
		),
		setting.Flag(
			Setting{Name: "motion.saver", Group: Group, Icon: "mdi:pan"},
			func(c *schema.Photos) *bool { return &c.Motion.Saver },
		),
		setting.Flag(
			Setting{Name: "motion.app", Group: Group, Icon: "mdi:pan"},
			func(c *schema.Photos) *bool { return &c.Motion.App },
		),
	}, setting.Messages{Text: say.T, Missing: say.Missing})
}
