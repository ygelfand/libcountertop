// Package homealarm follows one Home Assistant alarm_control_panel entity and arms or disarms it.
package homealarm

import (
	"context"
	"errors"
	"log/slog"
	"slices"
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
	domain         = "alarm_control_panel"
	triggerFeature = 8
	discoverWait   = 15 * time.Second
)

var ErrNoEntity = errors.New("homealarm: no alarm panel chosen")

type Status string

const (
	Unknown       Status = ""
	Disarmed      Status = "disarmed"
	ArmedHome     Status = "armed_home"
	ArmedAway     Status = "armed_away"
	ArmedNight    Status = "armed_night"
	ArmedVacation Status = "armed_vacation"
	ArmedBypass   Status = "armed_custom_bypass"
	Arming        Status = "arming"
	Disarming     Status = "disarming"
	Pending       Status = "pending"
	Triggered     Status = "triggered"
	Unavailable   Status = "unavailable"
)

type mode struct {
	status  Status
	feature int
	action  string
}

var modes = []mode{
	{ArmedHome, 1, "alarm_arm_home"},
	{ArmedAway, 2, "alarm_arm_away"},
	{ArmedNight, 4, "alarm_arm_night"},
	{ArmedVacation, 32, "alarm_arm_vacation"},
	{ArmedBypass, 16, "alarm_arm_custom_bypass"},
}

func (s Status) Armed() bool {
	for _, m := range modes {
		if m.status == s {
			return true
		}
	}
	return false
}

func (s Status) Urgent() bool { return s == Pending || s == Triggered }

func (s Status) Settled() bool { return s == Disarmed || s.Armed() }

type Code string

const (
	NoCode     Code = ""
	NumberCode Code = "number"
	TextCode   Code = "text"
)

type State struct {
	Entity    string
	Name      string
	Status    Status
	Code      Code
	CodeToArm bool
	ChangedBy string
	features  int
}

func (s State) Modes() []Status {
	var out []Status
	for _, m := range modes {
		if s.features&m.feature != 0 {
			out = append(out, m.status)
		}
	}
	return out
}

func (s State) Known() bool { return s.Status != Unknown }

func (s State) CanTrigger() bool { return s.features&triggerFeature != 0 }

func (s State) NeedsCode(arming bool) bool {
	return s.Code != NoCode && (!arming || s.CodeToArm)
}

type Change struct{ From, To State }

type Controller interface {
	Templater
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
	Settings   storage.Section[schema.HomeAlarm]
	DeviceID   uint32
	Chimes     func() []setting.Option
}

type Alarm struct {
	Changed hook.Hook[Change]
	Updated hook.Hook[struct{}]

	o     Options
	knobs *Controls

	mu         sync.Mutex
	following  string
	values     attributes
	stops      []func()
	state      State
	parts      device
	partsStops []func()
}

type attributes struct{ state, name, features, code, codeToArm, changedBy *states.Value }

func New(o Options) *Alarm {
	a := &Alarm{o: o}
	a.knobs = &Controls{
		Table:  NewTable(o.Chimes),
		Device: o.DeviceID,
		Read:   o.Settings.Read,
		Save: func(s Setting, v string) error {
			err := setting.Store(o.Settings, s, v)
			a.follow()
			return err
		},
	}
	o.Controller.Watch(func(access homeassistant.Access) {
		if access == homeassistant.Allowed {
			safe.Go("home alarm device", a.discover)
		}
	})
	a.follow()
	return a
}

func (a *Alarm) Name() string { return "home alarm" }

func (a *Alarm) Device() Device {
	hidden := a.o.Settings.Read().Hide
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.parts.read(hidden)
}

func (a *Alarm) Zones() []Zone {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.parts.zoneNames()
}

func (a *Alarm) Hide(entity string, hidden bool) error {
	err := a.o.Settings.Update(func(c *schema.HomeAlarm) {
		c.Hide = slices.DeleteFunc(c.Hide, func(e string) bool { return e == entity })
		if hidden {
			c.Hide = append(c.Hide, entity)
		}
	})
	a.Updated.Emit(struct{}{})
	return err
}

func (a *Alarm) Press(ctx context.Context, entity string) error {
	return a.act(ctx, "button.press", entity)
}

func (a *Alarm) Toggle(ctx context.Context, entity string) error {
	return a.act(ctx, "switch.toggle", entity)
}

func (a *Alarm) act(ctx context.Context, action, entity string) error {
	err := a.o.Controller.Call(ctx, action, map[string]string{"entity_id": entity})
	if err != nil {
		slog.Warn("home alarm action failed", "entity", entity, "action", action, "err", err)
	}
	return err
}

func (a *Alarm) discover() {
	entity := strings.TrimSpace(a.o.Settings.Read().Entity)
	if entity == "" {
		a.choose()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), discoverWait)
	defer cancel()
	parts, err := discover(ctx, a.o.Controller, entity)
	if err != nil {
		slog.Info("home alarm device", "entity", entity, "err", err)
		return
	}
	d := build(parts, a.o.States)
	a.mu.Lock()
	if a.following != entity {
		a.mu.Unlock()
		return
	}
	for _, stop := range a.partsStops {
		stop()
	}
	a.parts, a.partsStops = d, nil
	for _, v := range d.values() {
		a.partsStops = append(
			a.partsStops,
			v.Changed.Listen(func(string) { a.Updated.Emit(struct{}{}) }),
		)
	}
	a.mu.Unlock()
	logParts(entity, d)
	a.Updated.Emit(struct{}{})
}

func (a *Alarm) choose() {
	ctx, cancel := context.WithTimeout(context.Background(), discoverWait)
	defer cancel()
	panels, err := a.Panels(ctx)
	if err != nil || len(panels) != 1 {
		return
	}
	if err := a.Choose(panels[0].ID); err != nil {
		slog.Error("saving the home alarm panel failed", "err", err)
	}
}

func (a *Alarm) Entities() []esphome.Entity { return a.knobs.Entities() }

func (a *Alarm) Settings() *Controls { return a.knobs }

func (a *Alarm) Restore(c schema.HomeAlarm) {
	a.knobs.PublishFrom(c)
	a.follow()
}

func (a *Alarm) State() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state
}

func (a *Alarm) Panels(ctx context.Context) ([]homeassistant.Entity, error) {
	return a.o.Controller.Entities(ctx, homeassistant.Filter{Domains: []string{domain}})
}

func (a *Alarm) Choose(entity string) error {
	err := a.o.Settings.Update(func(c *schema.HomeAlarm) { c.Entity = strings.TrimSpace(entity) })
	a.knobs.Publish()
	a.follow()
	return err
}

func (a *Alarm) Arm(ctx context.Context, to Status, code string) error {
	for _, m := range modes {
		if m.status == to {
			return a.call(ctx, m.action, code)
		}
	}
	return errors.New("homealarm: " + string(to) + " is not an arm mode")
}

func (a *Alarm) Disarm(ctx context.Context, code string) error {
	return a.call(ctx, "alarm_disarm", code)
}

func (a *Alarm) Trigger(ctx context.Context, code string) error {
	return a.call(ctx, "alarm_trigger", code)
}

func (a *Alarm) call(ctx context.Context, action, code string) error {
	entity := a.State().Entity
	if entity == "" {
		return ErrNoEntity
	}
	data := map[string]string{"entity_id": entity}
	if code != "" {
		data["code"] = code
	}
	err := a.o.Controller.Call(ctx, domain+"."+action, data)
	if err != nil {
		slog.Warn("home alarm action failed", "entity", entity, "action", action, "err", err)
	}
	return err
}

func (a *Alarm) follow() {
	entity := strings.TrimSpace(a.o.Settings.Read().Entity)
	a.mu.Lock()
	if entity == a.following {
		a.mu.Unlock()
		return
	}
	for _, stop := range a.stops {
		stop()
	}
	for _, stop := range a.partsStops {
		stop()
	}
	a.parts, a.partsStops = device{}, nil
	a.following, a.stops, a.values = entity, nil, attributes{}
	from := a.state
	a.state = State{Entity: entity}
	to := a.state
	if entity != "" {
		a.values = attributes{
			state:     a.o.States.Follow(entity, ""),
			name:      a.o.States.Follow(entity, "friendly_name"),
			features:  a.o.States.Follow(entity, "supported_features"),
			code:      a.o.States.Follow(entity, "code_format"),
			codeToArm: a.o.States.Follow(entity, "code_arm_required"),
			changedBy: a.o.States.Follow(entity, "changed_by"),
		}
		for _, v := range a.values.all() {
			a.stops = append(a.stops, v.Changed.Listen(func(string) { a.refresh() }))
		}
	}
	a.mu.Unlock()
	if from != to {
		a.Changed.Emit(Change{From: from, To: to})
	}
	a.refresh()
	a.Updated.Emit(struct{}{})
	safe.Go("home alarm device", a.discover)
}

func (a *Alarm) refresh() {
	a.mu.Lock()
	if a.following == "" {
		a.mu.Unlock()
		return
	}
	from := a.state
	a.state = a.values.read(a.following)
	to := a.state
	a.mu.Unlock()
	if from != to {
		a.Changed.Emit(Change{From: from, To: to})
	}
}

func (at attributes) all() []*states.Value {
	return []*states.Value{at.state, at.name, at.features, at.code, at.codeToArm, at.changedBy}
}

func (at attributes) read(entity string) State {
	s := State{Entity: entity}
	if v, ok := at.state.Get(); ok {
		s.Status = Status(strings.TrimSpace(v))
	}
	if v, ok := at.name.Get(); ok {
		s.Name = strings.TrimSpace(v)
	}
	if f, ok := at.features.Float(); ok {
		s.features = int(f)
	}
	if v, ok := at.code.Get(); ok {
		switch Code(strings.ToLower(strings.TrimSpace(v))) {
		case NumberCode:
			s.Code = NumberCode
		case TextCode:
			s.Code = TextCode
		}
	}
	if v, ok := at.codeToArm.Get(); ok {
		s.CodeToArm = strings.EqualFold(strings.TrimSpace(v), "true")
	}
	if v, ok := at.changedBy.Get(); ok && !strings.EqualFold(strings.TrimSpace(v), "None") {
		s.ChangedBy = strings.TrimSpace(v)
	}
	return s
}
