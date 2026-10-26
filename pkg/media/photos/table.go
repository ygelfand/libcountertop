package photos

import (
	"fmt"

	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	Group       setting.Group = "Photos"
	ImmichGroup setting.Group = "Immich"
	GoogleGroup setting.Group = "Google"
)

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
		Setting{Name: "immich.key", Group: ImmichGroup, Icon: "mdi:key"},
		textMost,
		func(c *schema.Photos) *string { return &c.Immich.Key },
	)
	key.Kind = setting.Secret
	rows := []Setting{
		setting.Pick(
			Setting{Name: "app.every", Group: Group, Icon: "mdi:image-sync"},
			schema.PhotoAppSpans(),
			func(c *schema.Photos) *schema.Delay { return &c.AppEvery },
		),
		setting.Pick(
			Setting{Name: "app.fit", Group: Group, Icon: "mdi:fit-to-screen"},
			schema.PhotoFits(),
			func(c *schema.Photos) *schema.PhotoFit { return &c.AppFit },
		),
		setting.Pick(
			Setting{Name: "app.transition", Group: Group, Icon: "mdi:transition"},
			schema.PhotoTransitions(),
			func(c *schema.Photos) *schema.PhotoTransition { return &c.AppChange },
		),
		setting.Flag(
			Setting{Name: "immich.enabled", Group: ImmichGroup, Icon: "mdi:image-multiple"},
			func(c *schema.Photos) *bool { return &c.Immich.Enabled },
		),
		setting.Words(
			Setting{Name: "immich.server", Group: ImmichGroup, Icon: "mdi:server"},
			textMost,
			func(c *schema.Photos) *string { return &c.Immich.Server },
		),
		key,
		setting.Words(
			Setting{Name: "immich.albums", Group: ImmichGroup, Icon: "mdi:image-album"},
			listMost,
			func(c *schema.Photos) *string { return &c.Immich.Albums },
		),
		setting.Words(
			Setting{Name: "immich.tags", Group: ImmichGroup, Icon: "mdi:tag-multiple"},
			listMost,
			func(c *schema.Photos) *string { return &c.Immich.Tags },
		),
		setting.Words(
			Setting{Name: "immich.people", Group: ImmichGroup, Icon: "mdi:account-group"},
			listMost,
			func(c *schema.Photos) *string { return &c.Immich.People },
		),
		setting.Flag(
			Setting{Name: "immich.favorites", Group: ImmichGroup, Icon: "mdi:heart"},
			func(c *schema.Photos) *bool { return &c.Immich.Favorites },
		),
		setting.Flag(
			Setting{Name: "immich.memories", Group: ImmichGroup, Icon: "mdi:calendar-heart"},
			func(c *schema.Photos) *bool { return &c.Immich.Memories },
		),
		setting.Pick(
			Setting{Name: "immich.quality", Group: ImmichGroup, Icon: "mdi:quality-high"},
			schema.ImmichQualities(),
			func(c *schema.Photos) *schema.ImmichQuality { return &c.Immich.Quality },
		),
		setting.Flag(
			Setting{Name: "google.enabled", Group: GoogleGroup, Icon: "mdi:google-photos"},
			func(c *schema.Photos) *bool { return &c.Google.Enabled },
		),
	}
	for i := range schema.GoogleAlbums {
		album := func(c *schema.Photos) *schema.GoogleAlbum { return &c.Google.Albums[i] }
		name := fmt.Sprintf("google.album%d", i+1)
		rows = append(rows,
			setting.Words(
				Setting{Name: name, Group: GoogleGroup, Icon: "mdi:link-variant"},
				textMost,
				func(c *schema.Photos) *string { return &album(c).URL },
			),
			setting.Count(Setting{
				Name: name + ".weight", Group: GoogleGroup, Icon: "mdi:scale-balance",
				Min: 0, Max: schema.GoogleWeightMost, Slider: true,
			}, func(c *schema.Photos) *int { return &album(c).Weight }),
		)
	}
	return setting.NewTable("photos", []setting.Group{Group, ImmichGroup, GoogleGroup}, rows,
		setting.Messages{Text: say.T, Missing: say.Missing})
}
