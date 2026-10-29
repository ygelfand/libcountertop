package schema

import (
	"slices"

	"github.com/ygelfand/libcountertop/pkg/say"
)

type ThermostatLook string

const (
	ThermostatMinimal   ThermostatLook = "minimal"
	ThermostatEditorial ThermostatLook = "editorial"
	ThermostatPill      ThermostatLook = "pill"
	ThermostatVault     ThermostatLook = "vault"
	ThermostatVaultNV   ThermostatLook = "vault-amber"
	ThermostatShrine    ThermostatLook = "shrine"
	ThermostatHUD       ThermostatLook = "hud"
	ThermostatMatrix    ThermostatLook = "matrix"
	ThermostatTarget    ThermostatLook = "target"
	ThermostatLab       ThermostatLook = "lab"
	ThermostatGrid      ThermostatLook = "grid"
	ThermostatHAL       ThermostatLook = "hal"
	ThermostatTop       ThermostatLook = "top"
	ThermostatTARS      ThermostatLook = "tars"
	ThermostatBrass     ThermostatLook = "brass"
)

func ThermostatLooks() []ThermostatLook {
	return []ThermostatLook{
		ThermostatEditorial,
		ThermostatPill,
		ThermostatTop,
		ThermostatVault,
		ThermostatVaultNV,
		ThermostatShrine,
		ThermostatHUD,
		ThermostatGrid,
		ThermostatTARS,
		ThermostatHAL,
		ThermostatMatrix,
		ThermostatTarget,
		ThermostatLab,
		ThermostatBrass,
		ThermostatMinimal,
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

func DefaultThermostat() Thermostat { return Thermostat{Look: ThermostatEditorial} }
