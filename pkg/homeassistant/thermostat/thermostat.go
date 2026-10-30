// Package thermostat follows up to four Home Assistant climate entities and changes their mode and setpoints.
package thermostat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/states"
	"github.com/ygelfand/libcountertop/pkg/hook"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const (
	domain       = "climate"
	discoverWait = 15 * time.Second
)

var ErrUnknown = errors.New("thermostat: not one of the chosen thermostats")

type Mode string

const (
	Off      Mode = "off"
	Heat     Mode = "heat"
	Cool     Mode = "cool"
	HeatCool Mode = "heat_cool"
	Auto     Mode = "auto"
	Dry      Mode = "dry"
	FanOnly  Mode = "fan_only"
)

type Action string

const (
	Idle       Action = "idle"
	Heating    Action = "heating"
	Cooling    Action = "cooling"
	Drying     Action = "drying"
	Fan        Action = "fan"
	Preheating Action = "preheating"
	Defrosting Action = "defrosting"
	Stopped    Action = "off"
)

const (
	featureTarget = 1
	featureRange  = 2
)

type Limits struct {
	Modes    []Mode
	FanModes []string
	Presets  []string
	Min, Max float64
	Step     float64
	features int
}

type State struct {
	Entity   string
	Name     string
	Mode     Mode
	Action   Action
	Fan      string
	Preset   string
	Current  *float64
	Target   *float64
	Low      *float64
	High     *float64
	Humidity *float64
	Limits
}

func (s State) Known() bool { return s.Mode != "" }

func (s State) Available() bool {
	return s.Known() && s.Mode != "unavailable" && s.Mode != "unknown"
}

func (s State) Ranged() bool {
	return s.features&featureRange != 0 && s.Low != nil && s.High != nil &&
		(s.Mode == HeatCool || s.Mode == Auto)
}

func (s State) Settable() bool {
	return s.Ranged() || s.features&featureTarget != 0 && s.Target != nil
}

func (s State) Clamp(v float64) float64 {
	if s.Max > s.Min {
		v = min(max(v, s.Min), s.Max)
	}
	return v
}

type Controller interface {
	Template(ctx context.Context, tmpl string) (json.RawMessage, error)
	Entities(ctx context.Context, f homeassistant.Filter) ([]homeassistant.Entity, error)
	Call(ctx context.Context, action string, data map[string]string) error
	Watch(changed func(homeassistant.Access)) func()
}

type Follower interface {
	Follow(entity, attribute string) *states.Value
}

type Options struct {
	Controller Controller
	States     Follower
	Settings   storage.Section[schema.Thermostat]
	DeviceID   uint32
	Unit       func() string
}

type Thermostats struct {
	Updated hook.Hook[struct{}]

	o     Options
	knobs *Controls

	mu        sync.Mutex
	following []string
	climates  map[string]*climate
}

type climate struct {
	entity string
	values attributes
	limits Limits
	stops  []func()
}

type attributes struct {
	mode, name, action, fan, preset, current, target, low, high, humidity *states.Value
}

func New(o Options) *Thermostats {
	t := &Thermostats{o: o, climates: map[string]*climate{}}
	t.knobs = &Controls{
		Table:  NewTable(),
		Device: o.DeviceID,
		Read:   o.Settings.Read,
		Save: func(s Setting, v string) error {
			err := setting.Store(o.Settings, s, v)
			t.Updated.Emit(struct{}{})
			return err
		},
	}
	o.Controller.Watch(func(access homeassistant.Access) {
		if access == homeassistant.Allowed {
			safe.Go("thermostat limits", t.discover)
		}
	})
	t.follow()
	return t
}

func (t *Thermostats) Name() string { return "thermostats" }

func (t *Thermostats) Entities() []esphome.Entity { return t.knobs.Entities() }

func (t *Thermostats) Settings() *Controls { return t.knobs }

func (t *Thermostats) Restore(c schema.Thermostat) {
	t.knobs.PublishFrom(c)
	t.follow()
}

func (t *Thermostats) States() []State {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]State, 0, len(t.following))
	for _, e := range t.following {
		if c, ok := t.climates[e]; ok {
			out = append(out, c.read())
		}
	}
	return out
}

func (t *Thermostats) State(entity string) (State, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.climates[entity]
	if !ok {
		return State{}, false
	}
	return c.read(), true
}

func (t *Thermostats) Unit() string {
	if t.o.Unit == nil {
		return ""
	}
	return t.o.Unit()
}

func (t *Thermostats) Climates(ctx context.Context) ([]homeassistant.Entity, error) {
	return t.o.Controller.Entities(ctx, homeassistant.Filter{Domains: []string{domain}})
}

func (t *Thermostats) Chosen() []string { return slices.Clone(t.o.Settings.Read().Entities) }

func (t *Thermostats) Choose(entity string, on bool) error {
	err := t.o.Settings.Update(func(c *schema.Thermostat) {
		c.Entities = slices.DeleteFunc(c.Entities, func(e string) bool { return e == entity })
		if on && len(c.Entities) < schema.ThermostatsMost {
			c.Entities = append(c.Entities, entity)
		}
	})
	t.follow()
	return err
}

func (t *Thermostats) SetMode(ctx context.Context, entity string, m Mode) error {
	return t.call(ctx, entity, "set_hvac_mode", map[string]string{"hvac_mode": string(m)})
}

func (t *Thermostats) SetTemperature(ctx context.Context, entity string, v float64) error {
	return t.call(ctx, entity, "set_temperature", map[string]string{"temperature": number(v)})
}

func (t *Thermostats) SetPreset(ctx context.Context, entity, preset string) error {
	return t.call(ctx, entity, "set_preset_mode", map[string]string{"preset_mode": preset})
}

func (t *Thermostats) SetFan(ctx context.Context, entity, fan string) error {
	return t.call(ctx, entity, "set_fan_mode", map[string]string{"fan_mode": fan})
}

func (t *Thermostats) SetRange(ctx context.Context, entity string, low, high float64) error {
	return t.call(ctx, entity, "set_temperature", map[string]string{
		"target_temp_low":  number(low),
		"target_temp_high": number(high),
	})
}

func (t *Thermostats) call(
	ctx context.Context,
	entity, action string,
	data map[string]string,
) error {
	if !slices.Contains(t.Chosen(), entity) {
		return ErrUnknown
	}
	data["entity_id"] = entity
	err := t.o.Controller.Call(ctx, domain+"."+action, data)
	if err != nil {
		slog.Warn("thermostat action failed", "entity", entity, "action", action, "err", err)
	}
	return err
}

func number(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func (t *Thermostats) follow() {
	want := t.o.Settings.Read().Entities
	t.mu.Lock()
	if slices.Equal(want, t.following) {
		t.mu.Unlock()
		return
	}
	for e, c := range t.climates {
		if !slices.Contains(want, e) {
			for _, stop := range c.stops {
				stop()
			}
			delete(t.climates, e)
		}
	}
	for _, e := range want {
		if _, ok := t.climates[e]; ok {
			continue
		}
		c := &climate{entity: e, values: attributes{
			mode:     t.o.States.Follow(e, ""),
			name:     t.o.States.Follow(e, "friendly_name"),
			action:   t.o.States.Follow(e, "hvac_action"),
			fan:      t.o.States.Follow(e, "fan_mode"),
			preset:   t.o.States.Follow(e, "preset_mode"),
			current:  t.o.States.Follow(e, "current_temperature"),
			target:   t.o.States.Follow(e, "temperature"),
			low:      t.o.States.Follow(e, "target_temp_low"),
			high:     t.o.States.Follow(e, "target_temp_high"),
			humidity: t.o.States.Follow(e, "current_humidity"),
		}}
		for _, v := range c.values.all() {
			c.stops = append(c.stops, v.Changed.Listen(func(string) { t.Updated.Emit(struct{}{}) }))
		}
		t.climates[e] = c
	}
	t.following = slices.Clone(want)
	t.mu.Unlock()
	t.Updated.Emit(struct{}{})
	safe.Go("thermostat limits", t.discover)
}

const limitsTemplate = `{%- set ns = namespace(out={}) -%}
{%- for e in ENTITIES -%}
{%- set s = states[e] -%}
{%- if s -%}
{%- set ns.out = dict(ns.out, **{e: {'modes': s.attributes.get('hvac_modes') or [], 'fans': s.attributes.get('fan_modes') or [], 'presets': s.attributes.get('preset_modes') or [], 'min': s.attributes.get('min_temp'), 'max': s.attributes.get('max_temp'), 'step': s.attributes.get('target_temp_step'), 'features': s.attributes.get('supported_features') or 0}}) -%}
{%- endif -%}
{%- endfor -%}
{{ ns.out | tojson }}`

type limits struct {
	Modes    []string `json:"modes"`
	Fans     []string `json:"fans"`
	Presets  []string `json:"presets"`
	Min      *float64 `json:"min"`
	Max      *float64 `json:"max"`
	Step     *float64 `json:"step"`
	Features int      `json:"features"`
}

func (t *Thermostats) discover() {
	entities := t.Chosen()
	if len(entities) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), discoverWait)
	defer cancel()
	found, err := fetchLimits(ctx, t.o.Controller, entities)
	if err != nil {
		slog.Info("thermostat limits", "err", err)
		return
	}
	t.mu.Lock()
	for e, l := range found {
		if c, ok := t.climates[e]; ok {
			c.limits = l.read(t.Unit())
		}
	}
	t.mu.Unlock()
	t.Updated.Emit(struct{}{})
}

func fetchLimits(ctx context.Context, c Controller, entities []string) (map[string]limits, error) {
	quoted, err := json.Marshal(entities)
	if err != nil {
		return nil, err
	}
	raw, err := c.Template(ctx, strings.ReplaceAll(limitsTemplate, "ENTITIES", string(quoted)))
	if err != nil {
		return nil, err
	}
	var out map[string]limits
	if err := json.Unmarshal(raw, &out); err != nil {
		var wrapped string
		if json.Unmarshal(raw, &wrapped) != nil || json.Unmarshal([]byte(wrapped), &out) != nil {
			return nil, fmt.Errorf("thermostat: the climate limits: %w", err)
		}
	}
	return out, nil
}

func (l limits) read(unit string) Limits {
	out := Limits{FanModes: l.Fans, Presets: l.Presets, features: l.Features}
	for _, m := range l.Modes {
		out.Modes = append(out.Modes, Mode(m))
	}
	if l.Min != nil && l.Max != nil {
		out.Min, out.Max = *l.Min, *l.Max
	}
	switch {
	case l.Step != nil && *l.Step > 0:
		out.Step = *l.Step
	case unit == "°F":
		out.Step = 1
	default:
		out.Step = 0.5
	}
	return out
}

func (c *climate) read() State {
	s := State{Entity: c.entity, Limits: c.limits}
	if s.Step == 0 {
		s.Step = 0.5
	}
	if v, ok := c.values.mode.Get(); ok {
		s.Mode = Mode(strings.TrimSpace(v))
	}
	if v, ok := c.values.name.Get(); ok {
		s.Name = strings.TrimSpace(v)
	}
	if v, ok := c.values.action.Get(); ok && !missing(v) {
		s.Action = Action(strings.TrimSpace(v))
	}
	if v, ok := c.values.fan.Get(); ok && !missing(v) {
		s.Fan = strings.TrimSpace(v)
	}
	if v, ok := c.values.preset.Get(); ok && !missing(v) {
		s.Preset = strings.TrimSpace(v)
	}
	s.Current = float(c.values.current)
	s.Target = float(c.values.target)
	s.Low = float(c.values.low)
	s.High = float(c.values.high)
	s.Humidity = float(c.values.humidity)
	return s
}

func missing(v string) bool {
	v = strings.TrimSpace(v)
	return v == "" || v == "None" || v == "unknown" || v == "unavailable"
}

func float(v *states.Value) *float64 {
	f, ok := v.Float()
	if !ok {
		return nil
	}
	return &f
}

func (at attributes) all() []*states.Value {
	return []*states.Value{
		at.mode,
		at.name,
		at.action,
		at.fan,
		at.preset,
		at.current,
		at.target,
		at.low,
		at.high,
		at.humidity,
	}
}
