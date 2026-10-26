package idleview

import (
	"fmt"
	"strings"

	"github.com/ygelfand/libcountertop/pkg/display/clockview"
	"github.com/ygelfand/libcountertop/pkg/display/visual"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type (
	Setting = setting.Setting[schema.Idle]
	Table   = setting.Table[schema.Idle]
)

const (
	Group setting.Group = "Idle"

	noKind = "none"
)

func Faces() []schema.Face { return append([]schema.Face{schema.FaceNone}, clockview.Names()...) }

func NewTable() *Table {
	rows := []Setting{
		setting.Pick(Setting{Name: "saver", Group: Group, Icon: "mdi:monitor-shimmer"},
			schema.SaverModes(), func(c *schema.Idle) *schema.SaverMode { return &c.Saver }),
		setting.Pick(Setting{Name: "after", Group: Group, Icon: "mdi:timer-outline"},
			schema.Delays(), func(c *schema.Idle) *schema.Delay { return &c.After }),
		setting.Pick(Setting{Name: "media", Group: Group, Icon: "mdi:timer-pause-outline"},
			schema.MediaDelays(), func(c *schema.Idle) *schema.Delay { return &c.Media }),
		setting.Pick(Setting{Name: "face", Group: Group, Icon: "mdi:clock-digital"},
			Faces(), func(c *schema.Idle) *schema.Face { return &c.Face }),
		setting.Pick(Setting{Name: "position", Group: Group, Icon: "mdi:align-vertical-center"},
			schema.IdlePositions(), func(c *schema.Idle) *schema.Position { return &c.Position }),
		setting.Pick(Setting{Name: "align", Group: Group, Icon: "mdi:align-horizontal-center"},
			schema.IdleAligns(), func(c *schema.Idle) *schema.Align { return &c.Align }),
		setting.Pick(Setting{Name: "size", Group: Group, Icon: "mdi:resize"},
			schema.Sizes(), func(c *schema.Idle) *schema.Size { return &c.Size }),
		setting.Pick(
			Setting{Name: "night", Group: Group, ID: "night_mode", Icon: "mdi:weather-night"},
			schema.Nights(),
			func(c *schema.Idle) *schema.Night { return &c.Night },
		),
		setting.Count(Setting{
			Name: "night.lux", Group: Group, ID: "night_lux", Icon: "mdi:brightness-4",
			Min: 0, Max: 100, Unit: "lx",
		}, func(c *schema.Idle) *int { return &c.NightLux }),
		setting.Pick(
			Setting{Name: "night.after", Group: Group, ID: "night_after", Icon: "mdi:timer-sand"},
			schema.NightDelays(),
			func(c *schema.Idle) *schema.Delay { return &c.NightAfter },
		),
	}
	rows = append(rows, setting.Pick(
		Setting{Name: "photo_every", Group: Group, Icon: "mdi:image-sync"},
		schema.IdlePhotoSpans(),
		func(c *schema.Idle) *schema.Delay { return &c.PhotoEvery },
	), setting.Pick(
		Setting{Name: "photo_fit", Group: Group, Icon: "mdi:fit-to-screen"},
		schema.PhotoFits(),
		func(c *schema.Idle) *schema.PhotoFit { return &c.PhotoFit },
	), setting.Pick(
		Setting{Name: "photo_transition", Group: Group, Icon: "mdi:transition"},
		schema.PhotoTransitions(),
		func(c *schema.Idle) *schema.PhotoTransition { return &c.PhotoChange },
	))
	for n, id := range []string{"1", "2"} {
		rows = append(rows,
			kindRow(Setting{
				Name: "visual" + id, Group: Group, ID: "idle_visual_" + id, Icon: "mdi:waveform",
			}, n),
			setting.Pick(Setting{
				Name: "visual" + id + ".source", Group: Group,
				ID: "idle_visual_" + id + "_source", Icon: "mdi:microphone-settings",
			}, schema.Sources(), func(c *schema.Idle) *schema.Source { return &slot(c, n).Source }),
		)
	}
	return setting.NewTable("idle", []setting.Group{Group}, rows,
		setting.Messages{Text: say.T, Missing: say.Missing})
}

func kindRow(s Setting, n int) Setting {
	s.Kind = setting.Choice
	s.Options = []setting.Option{
		{Value: noKind, Label: KindLabel("")},
		{Value: schema.IdlePhotos, Label: KindLabel(schema.IdlePhotos)},
	}
	for _, k := range visual.Built() {
		s.Options = append(s.Options, setting.Option{Value: string(k), Label: k.Label()})
	}
	s.Read = func(c *schema.Idle) string {
		if k := slot(c, n).Kind; k != "" {
			return k
		}
		return noKind
	}
	s.Write = func(c *schema.Idle, v string) error {
		for _, o := range s.Options {
			if strings.EqualFold(o.Value, v) || strings.EqualFold(o.Label, v) {
				slot(c, n).Kind = o.Value
				if o.Value == noKind {
					slot(c, n).Kind = ""
				}
				return nil
			}
		}
		return fmt.Errorf("%s takes %s, not %q", s.Name, strings.Join(s.Values(), " or "), v)
	}
	return s
}
