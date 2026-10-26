package homealarm

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/ygelfand/libcountertop/pkg/homeassistant/states"
)

const partsTemplate = `{%- set panel = device_id(ENTITY) -%}
{%- set ns = namespace(out=[]) -%}
{%- if panel -%}
{%- for s in states -%}
{%- set d = device_id(s.entity_id) -%}
{%- if d and s.entity_id != ENTITY and (d == panel or device_attr(d, 'via_device_id') == panel) -%}
{%- set ns.out = ns.out + [{'id': s.entity_id, 'name': s.name, 'domain': s.domain, 'class': s.attributes.get('device_class') or '', 'zone': s.attributes.get('zone') | string, 'device': device_attr(panel, 'name_by_user') or device_attr(panel, 'name') or '', 'attributes': s.attributes.keys() | list}] -%}
{%- endif -%}
{%- endfor -%}
{%- endif -%}
{{ ns.out | tojson }}`

var zoneClasses = []string{
	"door", "window", "garage_door", "opening", "motion", "occupancy", "presence", "vibration",
	"tamper", "smoke", "moisture", "gas", "carbon_monoxide", "sound", "safety",
}

var zoneFlags = []string{"bypassed", "low_battery", "tamper", "fault"}

var goodWhenOn = []string{"power", "connectivity", "plug", "running"}

type Templater interface {
	Template(ctx context.Context, tmpl string) (json.RawMessage, error)
}

type part struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Domain     string   `json:"domain"`
	Class      string   `json:"class"`
	Zone       string   `json:"zone"`
	Device     string   `json:"device"`
	Attributes []string `json:"attributes"`
}

type Zone struct {
	Entity     string
	Name       string
	Class      string
	Number     int
	Open       bool
	Known      bool
	Bypassed   bool
	LowBattery bool
	Tamper     bool
	Fault      bool
}

func (z Zone) Trouble() bool { return z.LowBattery || z.Tamper || z.Fault }

type Indicator struct {
	Entity string
	Name   string
	Class  string
	On     bool
	Known  bool
}

func (i Indicator) Bad() bool {
	if !i.Known || i.Class == "" {
		return false
	}
	return i.On != slices.Contains(goodWhenOn, i.Class)
}

type Control struct {
	Entity string
	Name   string
	On     bool
	Known  bool
}

type Device struct {
	Zones      []Zone
	Indicators []Indicator
	Buttons    []Control
	Toggles    []Control
	Message    string
}

func (d Device) Open() []Zone {
	var out []Zone
	for _, z := range d.Zones {
		if z.Open && !z.Bypassed {
			out = append(out, z)
		}
	}
	return out
}

type zoneWatch struct {
	zone  Zone
	state *states.Value
	flags map[string]*states.Value
}

type device struct {
	zones      []*zoneWatch
	indicators []watched
	buttons    []watched
	toggles    []watched
	message    *watched
}

type watched struct {
	entity, name, class string
	value               *states.Value
}

func (w watched) on() (bool, bool) {
	v, ok := w.value.Get()
	return strings.EqualFold(v, "on"), ok
}

func discover(ctx context.Context, t Templater, entity string) ([]part, error) {
	tmpl := strings.ReplaceAll(partsTemplate, "ENTITY", strconv.Quote(entity))
	raw, err := t.Template(ctx, tmpl)
	if err != nil {
		return nil, err
	}
	var parts []part
	if err := json.Unmarshal(raw, &parts); err != nil {
		var wrapped string
		if json.Unmarshal(raw, &wrapped) != nil || json.Unmarshal([]byte(wrapped), &parts) != nil {
			return nil, fmt.Errorf("homealarm: the panel's entities: %w", err)
		}
	}
	return parts, nil
}

func build(parts []part, follow Follower) device {
	numbered := slices.ContainsFunc(parts, func(p part) bool {
		return p.Domain == "binary_sensor" && slices.Contains(zoneClasses, p.Class) &&
			zoneNumber(p) > 0
	})
	var d device
	for _, p := range parts {
		name := strings.TrimSpace(strings.TrimPrefix(p.Name, p.Device))
		if name == "" {
			name = p.Name
		}
		w := watched{entity: p.ID, name: name, class: p.Class}
		switch p.Domain {
		case "binary_sensor":
			isZone := slices.Contains(zoneClasses, p.Class) && (!numbered || zoneNumber(p) > 0)
			switch {
			case isZone:
				z := &zoneWatch{
					zone:  Zone{Entity: p.ID, Name: p.Name, Class: p.Class, Number: zoneNumber(p)},
					state: follow.Follow(p.ID, ""),
					flags: map[string]*states.Value{},
				}
				for _, f := range zoneFlags {
					if slices.Contains(p.Attributes, f) {
						z.flags[f] = follow.Follow(p.ID, f)
					}
				}
				d.zones = append(d.zones, z)
			case numbered && zoneNumber(p) > 0 && p.Class == "":
			default:
				w.value = follow.Follow(p.ID, "")
				d.indicators = append(d.indicators, w)
			}
		case "button":
			w.value = follow.Follow(p.ID, "")
			d.buttons = append(d.buttons, w)
		case "switch":
			w.value = follow.Follow(p.ID, "")
			d.toggles = append(d.toggles, w)
		case "sensor":
			if d.message == nil && p.Class == "" {
				w.value = follow.Follow(p.ID, "")
				d.message = &w
			}
		}
	}
	slices.SortStableFunc(d.zones, func(a, b *zoneWatch) int {
		if a.zone.Number != b.zone.Number {
			return cmp.Compare(a.zone.Number, b.zone.Number)
		}
		return strings.Compare(a.zone.Name, b.zone.Name)
	})
	return d
}

func zoneNumber(p part) int {
	n, err := strconv.Atoi(strings.TrimSpace(p.Zone))
	if err != nil {
		return 0
	}
	return n
}

func (d device) values() []*states.Value {
	var out []*states.Value
	for _, z := range d.zones {
		out = append(out, z.state)
		for _, f := range z.flags {
			out = append(out, f)
		}
	}
	for _, list := range [][]watched{d.indicators, d.buttons, d.toggles} {
		for _, w := range list {
			out = append(out, w.value)
		}
	}
	if d.message != nil {
		out = append(out, d.message.value)
	}
	return out
}

func (d device) read(hidden []string) Device {
	var out Device
	for _, z := range d.zones {
		if slices.Contains(hidden, z.zone.Entity) {
			continue
		}
		zone := z.zone
		v, ok := z.state.Get()
		zone.Open, zone.Known = strings.EqualFold(v, "on"), ok
		flag := func(name string) bool {
			f, ok := z.flags[name]
			if !ok {
				return false
			}
			s, _ := f.Get()
			return strings.EqualFold(strings.TrimSpace(s), "true")
		}
		zone.Bypassed, zone.LowBattery = flag("bypassed"), flag("low_battery")
		zone.Tamper, zone.Fault = flag("tamper"), flag("fault")
		out.Zones = append(out.Zones, zone)
	}
	for _, w := range d.indicators {
		on, known := w.on()
		out.Indicators = append(
			out.Indicators,
			Indicator{Entity: w.entity, Name: w.name, Class: w.class, On: on, Known: known},
		)
	}
	for _, w := range d.buttons {
		out.Buttons = append(out.Buttons, Control{Entity: w.entity, Name: w.name})
	}
	for _, w := range d.toggles {
		on, known := w.on()
		out.Toggles = append(
			out.Toggles,
			Control{Entity: w.entity, Name: w.name, On: on, Known: known},
		)
	}
	if d.message != nil {
		if v, ok := d.message.value.Get(); ok && v != "unknown" && v != "unavailable" {
			out.Message = strings.TrimSpace(v)
		}
	}
	return out
}

func (d device) zoneNames() []Zone {
	out := make([]Zone, 0, len(d.zones))
	for _, z := range d.zones {
		out = append(out, z.zone)
	}
	return out
}

func logParts(entity string, d device) {
	slog.Info(
		"home alarm device",
		"entity",
		entity,
		"zones",
		len(d.zones),
		"indicators",
		len(d.indicators),
		"buttons",
		len(d.buttons),
		"toggles",
		len(d.toggles),
		"message",
		d.message != nil,
	)
}
