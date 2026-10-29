package thermostat

import (
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const Group setting.Group = "Thermostat"

type (
	Setting  = setting.Setting[schema.Thermostat]
	Table    = setting.Table[schema.Thermostat]
	Controls = setting.Controls[schema.Thermostat]
)

func NewTable() *Table {
	return setting.NewTable("thermostat", []setting.Group{Group}, []Setting{
		setting.Pick(
			Setting{Name: "look", Group: Group, Icon: "mdi:palette"},
			schema.ThermostatLooks(),
			func(c *schema.Thermostat) *schema.ThermostatLook { return &c.Look },
		),
		setting.Flag(
			Setting{Name: "tab", Group: Group, Icon: "mdi:tab"},
			func(c *schema.Thermostat) *bool { return &c.Tab },
		),
	}, setting.Messages{Text: say.T, Missing: say.Missing})
}
