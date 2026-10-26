package homealarmview

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ygelfand/libcountertop/pkg/homeassistant/homealarm"
	"github.com/ygelfand/libcountertop/pkg/say"
)

var (
	activeClasses = []string{"motion", "occupancy", "presence", "vibration", "sound"}
	hazardClasses = []string{"smoke", "moisture", "gas", "carbon_monoxide", "safety", "tamper"}
)

type zoneLine struct {
	number string
	name   string
	kind   string
	state  string
	alert  bool
	muted  bool
}

func (f *face) zones() []zoneLine {
	out := make([]zoneLine, 0, len(f.dev.Zones))
	for i, z := range f.dev.Zones {
		n := z.Number
		if n == 0 {
			n = i + 1
		}
		out = append(out, zoneLine{
			number: fmt.Sprintf("%02d", n),
			name:   z.Name,
			kind:   say.T("homealarm.class." + z.Class),
			state:  zoneState(z),
			alert:  z.Known && (z.Open || z.Trouble()) && !z.Bypassed,
			muted:  z.Bypassed || !z.Known,
		})
	}
	return out
}

func zoneState(z homealarm.Zone) string {
	switch {
	case !z.Known:
		return say.T("homealarm.zone.lost")
	case z.Bypassed:
		return say.T("homealarm.zone.bypassed")
	case z.Open && slices.Contains(activeClasses, z.Class):
		return say.T("homealarm.zone.active")
	case z.Open && slices.Contains(hazardClasses, z.Class):
		return say.T("homealarm.zone.fault")
	case z.Open:
		return say.T("homealarm.zone.open")
	case z.LowBattery:
		return say.T("homealarm.zone.battery")
	case z.Tamper:
		return say.T("homealarm.zone.tamper")
	case z.Fault:
		return say.T("homealarm.zone.trouble")
	case slices.Contains(activeClasses, z.Class):
		return say.T("homealarm.zone.idle")
	case slices.Contains(hazardClasses, z.Class):
		return say.T("homealarm.zone.clear")
	}
	return say.T("homealarm.zone.closed")
}

func (f *face) ready() string {
	if f.state.Status != homealarm.Disarmed {
		return ""
	}
	open := f.dev.Open()
	switch len(open) {
	case 0:
		return ""
	case 1:
		return say.F("homealarm.notready.one", map[string]any{"Zone": open[0].Name})
	}
	return say.F("homealarm.notready.many", map[string]any{"N": len(open)})
}

func (f *face) counts() (secure, open int) {
	for _, z := range f.dev.Zones {
		if z.Known && z.Open && !z.Bypassed {
			open++
		} else {
			secure++
		}
	}
	return secure, open
}

func (f *face) toggles() []action {
	out := make([]action, 0, len(f.dev.Toggles))
	for _, t := range f.dev.Toggles {
		state := say.T("homealarm.off")
		if t.On {
			state = say.T("homealarm.on")
		}
		out = append(out, action{
			id:      "toggle-" + t.Entity,
			label:   t.Name + ": " + state,
			glyph:   "mdi:toggle-switch-outline",
			run:     f.v.toggle(t.Entity),
			current: t.On,
		})
	}
	return out
}

var panicGlyphs = []struct{ word, glyph string }{
	{"fire", "mdi:fire"},
	{"smoke", "mdi:fire"},
	{"ambulance", "mdi:ambulance"},
	{"medical", "mdi:ambulance"},
	{"police", "mdi:police-badge"},
}

func panicGlyph(name string) string {
	lower := strings.ToLower(name)
	for _, p := range panicGlyphs {
		if strings.Contains(lower, p.word) {
			return p.glyph
		}
	}
	return "mdi:alarm-light"
}

func shortNames(controls []homealarm.Control) []string {
	words := make([][]string, len(controls))
	for i, c := range controls {
		words[i] = strings.Fields(c.Name)
	}
	common := 0
	for len(controls) > 1 {
		ok := true
		for _, w := range words {
			if len(w) <= common+1 || w[common] != words[0][common] {
				ok = false
				break
			}
		}
		if !ok {
			break
		}
		common++
	}
	out := make([]string, len(controls))
	for i, w := range words {
		out[i] = strings.Join(w[common:], " ")
	}
	return out
}

func (f *face) troubles() []string {
	var out []string
	for _, i := range f.dev.Indicators {
		if i.Bad() {
			out = append(out, i.Name)
		}
	}
	for _, z := range f.dev.Zones {
		if z.Known && z.Trouble() && !z.Open {
			out = append(out, z.Name+" · "+strings.ToLower(zoneState(z)))
		}
	}
	return out
}
