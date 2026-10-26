package homealarm

import (
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const Group setting.Group = "HomeAlarm"

const entityMost = 128

type (
	Setting  = setting.Setting[schema.HomeAlarm]
	Table    = setting.Table[schema.HomeAlarm]
	Controls = setting.Controls[schema.HomeAlarm]
)

type cue struct {
	name  string
	icon  string
	field func(*schema.HomeAlarm) *schema.HomeAlarmCue
}

var cues = []cue{
	{
		"triggered",
		"mdi:alarm-light",
		func(c *schema.HomeAlarm) *schema.HomeAlarmCue { return &c.Triggered },
	},
	{
		"pending",
		"mdi:timer-sand",
		func(c *schema.HomeAlarm) *schema.HomeAlarmCue { return &c.Pending },
	},
	{
		"arming",
		"mdi:shield-sync",
		func(c *schema.HomeAlarm) *schema.HomeAlarmCue { return &c.Arming },
	},
	{
		"settled",
		"mdi:shield-check",
		func(c *schema.HomeAlarm) *schema.HomeAlarmCue { return &c.Settled },
	},
}

func NewTable(chimes func() []setting.Option) *Table {
	rows := []Setting{
		setting.Words(
			Setting{Name: "entity", Group: Group, Icon: "mdi:shield-home", NoEntity: true},
			entityMost,
			func(c *schema.HomeAlarm) *string { return &c.Entity },
		),
		setting.Pick(
			Setting{Name: "look", Group: Group, Icon: "mdi:palette"},
			schema.HomeAlarmLooks(),
			func(c *schema.HomeAlarm) *schema.HomeAlarmLook { return &c.Look },
		),
		setting.Flag(
			Setting{Name: "tab", Group: Group, Icon: "mdi:tab"},
			func(c *schema.HomeAlarm) *bool { return &c.Tab },
		),
	}
	for _, c := range cues {
		rows = append(rows,
			setting.Flag(
				Setting{Name: c.name + ".show", Group: Group, Icon: c.icon},
				func(a *schema.HomeAlarm) *bool { return &c.field(a).Show },
			),
		)
		if c.name == "settled" {
			rows = append(rows, setting.Pick(
				Setting{Name: "settled.for", Group: Group, Icon: "mdi:timer-outline"},
				schema.HomeAlarmHideSpans(),
				func(a *schema.HomeAlarm) *schema.Delay { return &a.SettledFor },
			))
		}
		rows = append(rows,
			setting.Pick(
				Setting{
					Name:  c.name + ".sound",
					Group: Group,
					Icon:  "mdi:volume-high",
					More:  chimes,
				},
				schema.AlertChimes(),
				func(a *schema.HomeAlarm) *schema.Chime { return &c.field(a).Sound },
			),
		)
	}
	return setting.NewTable("homealarm", []setting.Group{Group}, rows,
		setting.Messages{Text: say.T, Missing: say.Missing})
}
