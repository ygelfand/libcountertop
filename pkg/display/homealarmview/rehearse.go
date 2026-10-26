package homealarmview

import (
	"fmt"
	"slices"

	"github.com/ygelfand/libcountertop/pkg/homeassistant/homealarm"
)

type rehearsal struct {
	status homealarm.Status
	open   []int
	sound  bool
}

var rehearsable = []homealarm.Status{
	homealarm.Disarmed, homealarm.ArmedHome, homealarm.ArmedAway, homealarm.ArmedNight,
	homealarm.ArmedVacation, homealarm.ArmedBypass, homealarm.Arming, homealarm.Disarming,
	homealarm.Pending, homealarm.Triggered, homealarm.Unavailable,
}

func Rehearsable() []homealarm.Status { return slices.Clone(rehearsable) }

func (v *View) Rehearse(status homealarm.Status, open []int, sound bool) error {
	if !slices.Contains(rehearsable, status) {
		return fmt.Errorf("homealarm: %q is not a state to rehearse", status)
	}
	from := v.shown()
	v.mu.Lock()
	v.rehearsed = &rehearsal{status: status, open: slices.Clone(open), sound: sound}
	v.mu.Unlock()
	to := v.shown()
	if from.Status != to.Status {
		v.react(from, to, sound)
	}
	v.o.Shell.Redraw()
	return nil
}

func (v *View) Open() {
	if v.o.Wake != nil {
		v.o.Wake()
	}
	v.o.Shell.Push(v.app)
}

func (v *View) StopRehearsal() {
	v.mu.Lock()
	was := v.rehearsed != nil
	v.rehearsed = nil
	v.mu.Unlock()
	if !was {
		return
	}
	v.o.Quiet()
	v.lower()
	v.tell("")
}

func (v *View) Rehearsing() (homealarm.Status, []int, bool) {
	r := v.rehearsal()
	if r == nil {
		return "", nil, false
	}
	return r.status, slices.Clone(r.open), true
}

func (v *View) rehearsal() *rehearsal {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.rehearsed
}

func (v *View) shown() homealarm.State {
	s := v.o.Alarm.State()
	if r := v.rehearsal(); r != nil {
		s.Status = r.status
	}
	return s
}

func (v *View) shownDevice() homealarm.Device {
	d := v.o.Alarm.Device()
	r := v.rehearsal()
	if r == nil {
		return d
	}
	d.Zones = slices.Clone(d.Zones)
	for i := range d.Zones {
		n := d.Zones[i].Number
		if n == 0 {
			n = i + 1
		}
		d.Zones[i].Open = slices.Contains(r.open, n)
		d.Zones[i].Known = true
	}
	return d
}
