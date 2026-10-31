package doorbell

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const Group setting.Group = "Doorbell"

const textMost = 256

type (
	Setting  = setting.Setting[schema.Doorbell]
	Table    = setting.Table[schema.Doorbell]
	Controls = setting.Controls[schema.Doorbell]
)

var doorKey = regexp.MustCompile(`^(doorbell\.[a-z]+\.door)(\d+)(\..+)$`)

func NewTable(chimes func() []setting.Option) *Table {
	rows := []Setting{
		setting.Pick(
			Setting{Name: "chime", Group: Group, Icon: "mdi:bell-ring-outline", More: chimes},
			schema.DoorbellChimes(),
			func(c *schema.Doorbell) *schema.Chime { return &c.Chime },
		),
		setting.Pick(
			Setting{Name: "ring_for", Group: Group, Icon: "mdi:timer-outline"},
			schema.DoorbellRingSpans(),
			func(c *schema.Doorbell) *schema.Delay { return &c.RingFor },
		),
		setting.Pick(
			Setting{Name: "live_for", Group: Group, Icon: "mdi:cctv"},
			schema.DoorbellLiveSpans(),
			func(c *schema.Doorbell) *schema.Delay { return &c.LiveFor },
		),
		setting.Pick(
			Setting{Name: "notify", Group: Group, Icon: "mdi:bell-outline", More: chimes},
			schema.AlertChimes(),
			func(c *schema.Doorbell) *schema.Chime { return &c.Notify },
		),
		setting.Pick(
			Setting{Name: "style", Group: Group, Icon: "mdi:message-badge-outline"},
			schema.DoorbellStyles(),
			func(c *schema.Doorbell) *schema.AlertStyle { return &c.Style },
		),
		setting.Pick(
			Setting{Name: "visitor", Group: Group, Icon: "mdi:doorbell"},
			schema.VisitorStyles(),
			func(c *schema.Doorbell) *schema.AlertStyle { return &c.Visitor },
		),
		setting.Flag(
			Setting{Name: "tab", Group: Group, Icon: "mdi:tab"},
			func(c *schema.Doorbell) *bool { return &c.Tab },
		),
	}
	for i := range schema.DoorbellSlots {
		rows = append(rows, slotRows(i)...)
	}
	return setting.NewTable("doorbell", []setting.Group{Group}, rows,
		setting.Messages{Text: doorText, Missing: doorMissing})
}

func doorText(key string) string {
	if m := doorKey.FindStringSubmatch(key); m != nil {
		n, _ := strconv.Atoi(m[2])
		return say.F(m[1]+m[3], map[string]any{"N": n})
	}
	return say.T(key)
}

func doorMissing(key string) bool {
	if m := doorKey.FindStringSubmatch(key); m != nil {
		return say.Missing(m[1] + m[3])
	}
	return say.Missing(key)
}

func slotRows(i int) []Setting {
	door := func(c *schema.Doorbell) *schema.DoorbellDoor { return &c.Doors[i] }
	name := func(field string) string { return fmt.Sprintf("door%d.%s", i+1, field) }
	return []Setting{
		setting.Words(
			Setting{Name: name("name"), Group: Group, Icon: "mdi:door"},
			textMost,
			func(c *schema.Doorbell) *string { return &door(c).Name },
		),
		setting.Pick(
			Setting{Name: name("video"), Group: Group, Icon: "mdi:video"},
			schema.DoorbellVideos(),
			func(c *schema.Doorbell) *schema.DoorbellVideo { return &door(c).Video },
		),
		setting.Pick(
			Setting{Name: name("events"), Group: Group, Icon: "mdi:bell-ring"},
			schema.DoorbellFeeds(),
			func(c *schema.Doorbell) *schema.DoorbellFeed { return &door(c).Events },
		),
		setting.Pick(
			Setting{Name: name("talk"), Group: Group, Icon: "mdi:microphone-message"},
			schema.DoorbellTalks(),
			func(c *schema.Doorbell) *schema.DoorbellTalk { return &door(c).Talk },
		),
	}
}
