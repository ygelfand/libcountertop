package cameraview

import (
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const Group setting.Group = "Cameras"

const urlMost = 512

type Setting = setting.Setting[schema.Cameras]

var (
	refreshSteps = []int{0, 1, 2, 3, 5, 10, 15, 30, 60}
	rotateSteps  = []int{0, 5, 10, 15, 30, 60, 120}
)

func NewTable() *setting.Table[schema.Cameras] {
	return setting.NewTable("cameras", []setting.Group{Group}, []Setting{
		setting.Flag(
			Setting{Name: "tab", Group: Group, Icon: "mdi:tab"},
			func(c *schema.Cameras) *bool { return &c.Tab },
		),
		setting.Count(Setting{
			Name: "scale", Group: Group, Icon: "mdi:magnify-plus-outline",
			Min: schema.CameraScaleLeast, Max: schema.CameraScaleMost, Slider: true,
		}, func(c *schema.Cameras) *int { return &c.Scale }),
		setting.Count(Setting{
			Name: "refresh", Group: Group, Icon: "mdi:refresh", Unit: "s",
			Max: schema.CameraRefreshMost, Slider: true,
		}, func(c *schema.Cameras) *int { return &c.Refresh }),
		setting.Count(Setting{
			Name: "rotate", Group: Group, Icon: "mdi:page-next-outline", Unit: "s",
			Max: schema.CameraRotateMost, Slider: true,
		}, func(c *schema.Cameras) *int { return &c.Rotate }),
		setting.Words(
			Setting{Name: "url", Group: Group, Icon: "mdi:web"},
			urlMost,
			func(c *schema.Cameras) *string { return &c.URL },
		),
	}, setting.Messages{Text: say.T, Missing: say.Missing})
}
