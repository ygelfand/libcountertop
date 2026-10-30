package thermostatview

import (
	"strings"
	"time"
	"unicode"

	"github.com/ygelfand/libcountertop/pkg/homeassistant/thermostat"
	"github.com/ygelfand/libcountertop/pkg/say"
)

const easeFor = 450 * time.Millisecond

type ease struct {
	from, to float64
	at       time.Time
}

func (v *View) eased(key string, to float64, now time.Time) (float64, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	e, ok := v.eases[key]
	if !ok {
		v.eases[key] = &ease{from: to, to: to}
		return to, false
	}
	if e.to != to {
		shown, _ := e.value(now)
		*e = ease{from: shown, to: to, at: now}
	}
	return e.value(now)
}

func (e *ease) value(now time.Time) (float64, bool) {
	k := float64(now.Sub(e.at)) / float64(easeFor)
	if k >= 1 || e.at.IsZero() {
		return e.to, false
	}
	rest := 1 - k
	k = 1 - rest*rest*rest
	return e.from + (e.to-e.from)*k, true
}

type pick int

const (
	pickMode pick = iota
	pickPreset
	pickFan
)

type chooser struct {
	entity string
	kind   pick
}

func (v *View) open(entity string, kind pick) {
	v.mu.Lock()
	v.choosing = &chooser{entity: entity, kind: kind}
	v.mu.Unlock()
	v.o.Shell.Redraw()
}

func (v *View) close() {
	v.mu.Lock()
	open := v.choosing != nil
	v.choosing = nil
	v.mu.Unlock()
	if open {
		v.o.Shell.Redraw()
	}
}

func (v *View) chosen() *chooser {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.choosing == nil {
		return nil
	}
	c := *v.choosing
	return &c
}

type option struct {
	value   string
	label   string
	current bool
}

func options(st thermostat.State, kind pick) []option {
	var out []option
	switch kind {
	case pickMode:
		for _, m := range st.Modes {
			out = append(out, option{string(m), modeLabel(m), m == st.Mode})
		}
	case pickPreset:
		for _, p := range st.Presets {
			out = append(out, option{p, presetLabel(p), p == st.Preset})
		}
	case pickFan:
		for _, f := range st.FanModes {
			out = append(out, option{f, fanLabel(f), f == st.Fan})
		}
	}
	return out
}

func (k pick) title() string {
	switch k {
	case pickPreset:
		return say.T("thermostat.choose.preset")
	case pickFan:
		return say.T("thermostat.choose.fan")
	}
	return say.T("thermostat.choose.mode")
}

func modeLabel(m thermostat.Mode) string { return named("thermostat.mode.", string(m)) }

func presetLabel(p string) string {
	if p == "" {
		return named("thermostat.preset.", "none")
	}
	return named("thermostat.preset.", p)
}

func fanLabel(f string) string { return named("thermostat.fanmode.", f) }

func named(prefix, value string) string {
	id := prefix + strings.ToLower(value)
	if s := say.T(id); s != id {
		return s
	}
	r := []rune(strings.ReplaceAll(value, "_", " "))
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}

func (v *View) apply(c chooser, value string) {
	switch c.kind {
	case pickPreset:
		v.setPreset(c.entity, value)
	case pickFan:
		v.setFan(c.entity, value)
	default:
		v.setMode(c.entity, thermostat.Mode(value))
	}
}

func frac(v, lo, hi float64) float64 {
	if hi <= lo {
		return 0
	}
	return min(max((v-lo)/(hi-lo), 0), 1)
}
