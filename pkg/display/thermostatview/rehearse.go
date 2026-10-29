package thermostatview

import (
	"slices"

	"github.com/ygelfand/libcountertop/pkg/homeassistant/thermostat"
)

func (v *View) Rehearse(n int) {
	v.mu.Lock()
	v.rehearsed = thermostat.Samples(n, v.o.Thermostats.Unit())
	clear(v.pending)
	clear(v.notes)
	v.mu.Unlock()
	v.o.Shell.Redraw()
}

func (v *View) StopRehearsal() {
	v.mu.Lock()
	v.rehearsed = nil
	clear(v.pending)
	v.mu.Unlock()
	v.o.Shell.Redraw()
}

func (v *View) Rehearsing() ([]thermostat.State, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.Clone(v.rehearsed), v.rehearsed != nil
}

func rehearsedAction(m thermostat.Mode) thermostat.Action {
	switch m {
	case thermostat.Heat:
		return thermostat.Heating
	case thermostat.Cool:
		return thermostat.Cooling
	case thermostat.Off:
		return thermostat.Stopped
	}
	return thermostat.Idle
}
