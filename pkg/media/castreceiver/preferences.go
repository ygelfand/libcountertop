package castreceiver

import (
	"github.com/ygelfand/libcountertop/pkg/media/cast/preferences"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

func Preferences(requestIDPrefix string, s storage.Section[schema.Cast]) preferences.Provider {
	return preferences.Provider{
		Prefix: requestIDPrefix,
		Read: func() preferences.Settings {
			c := s.Read()
			return preferences.Settings{
				YouTube: preferences.YouTube{
					Device:   c.YouTube.Device,
					Music:    c.YouTube.Music,
					Video:    c.YouTube.Video,
					Skip:     c.YouTube.Skip,
					OnDemand: c.YouTube.OnDemand,
				},
			}
		},
		Save: func(device, music, video string) error {
			return schema.NewCastWriter(s).Screens(device, music, video)
		},
	}
}
