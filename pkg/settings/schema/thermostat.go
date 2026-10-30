package schema

import (
	"slices"

	"github.com/ygelfand/libcountertop/pkg/say"
)

type ThermostatLook string

const (
	ThermostatDial     ThermostatLook = "dial"
	ThermostatColumn   ThermostatLook = "column"
	ThermostatWheel    ThermostatLook = "wheel"
	ThermostatBars     ThermostatLook = "bars"
	ThermostatSplit    ThermostatLook = "split"
	ThermostatTerminal ThermostatLook = "terminal"
	ThermostatBrass    ThermostatLook = "brass"
	ThermostatMission  ThermostatLook = "mission"
)

func ThermostatLooks() []ThermostatLook {
	return []ThermostatLook{
		ThermostatDial,
		ThermostatColumn,
		ThermostatWheel,
		ThermostatBars,
		ThermostatSplit,
		ThermostatTerminal,
		ThermostatBrass,
		ThermostatMission,
	}
}

func (l ThermostatLook) Label() string { return say.T("thermostat.look." + string(l)) }

const ThermostatsMost = 4

type Thermostat struct {
	Entities []string       `json:"entities"`
	Look     ThermostatLook `json:"look"`
	Tab      bool           `json:"tab"`
}

func (t Thermostat) Clone() Thermostat {
	t.Entities = slices.Clone(t.Entities)
	return t
}

func DefaultThermostat() Thermostat { return Thermostat{Look: ThermostatDial} }
