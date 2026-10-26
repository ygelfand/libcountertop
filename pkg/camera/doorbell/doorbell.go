// Package doorbell watches doorbells through Reolink's own protocol (github.com/ygelfand/reolinkproxy, a fork of github.com/shareed2k/reolinkproxy, MIT), ONVIF or Home Assistant.
package doorbell

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/states"
	"github.com/ygelfand/libcountertop/pkg/hook"
	"github.com/ygelfand/libcountertop/pkg/runtime/service"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const (
	recheck      = 5 * time.Second
	retryFirst   = 5 * time.Second
	retryMost    = 10 * time.Minute
	reolinkFirst = 30 * time.Second
	reolinkMost  = 30 * time.Minute
	steady       = time.Minute
	keep         = 20
)

var errEventsEnded = errors.New("doorbell: events stopped")

type Kind string

const (
	Visitor Kind = "visitor"
	Person  Kind = "people"
	Package Kind = "package"
	Vehicle Kind = "vehicle"
	Animal  Kind = "dog_cat"
)

var kinds = []Kind{Visitor, Person, Package, Vehicle, Animal}

type Event struct {
	Kind     Kind
	Slot     int
	Door     string
	At       time.Time
	Snapshot []byte
}

func (e Event) Says() string {
	return say.F("doorbell.event."+string(e.Kind), map[string]any{"Door": strings.ToLower(e.Door)})
}

type Status int

const (
	Unset Status = iota
	Connecting
	Ready
	BadLogin
	Unreachable
	Unsupported
)

type State struct {
	Status Status
	Count  int
	Doors  [schema.DoorbellSlots]DoorState
}

func (s State) Says() string {
	if s.Status == Ready {
		return say.F("doorbell.status.ready", map[string]any{"Count": s.Count})
	}
	return s.Status.Says()
}

func (s Status) Says() string {
	switch s {
	case Connecting:
		return say.T("doorbell.status.connecting")
	case Ready:
		return say.T("doorbell.status.connected")
	case BadLogin:
		return say.T("doorbell.status.login")
	case Unreachable:
		return say.T("doorbell.status.unreachable")
	case Unsupported:
		return say.T("doorbell.status.unsupported")
	}
	return say.T("doorbell.status.unset")
}

type Finder interface {
	ReolinkDevices(ctx context.Context) ([]homeassistant.ReolinkDevice, error)
}

type Follower interface {
	Follow(entity, attribute string) *states.Value
}

type Options struct {
	Settings storage.Section[schema.Doorbell]
	Finder   Finder
	States   Follower
	DeviceID uint32
	Chimes   func() []setting.Option
	Now      func() time.Time
}

type Doorbell struct {
	Seen    hook.Hook[Event]
	Changed hook.Hook[State]

	o      Options
	knobs  *Controls
	status *esphome.TextSensor

	mu      sync.Mutex
	state   State
	slots   [schema.DoorbellSlots]DoorState
	recent  []Event
	restart context.CancelFunc
	links   [schema.DoorbellSlots]*link
}

var errUnsupported = errors.New("doorbell: this source is not available yet")

func New(o Options) *Doorbell {
	if o.Now == nil {
		o.Now = time.Now
	}
	d := &Doorbell{o: o}
	d.knobs = &Controls{
		Table:  NewTable(o.Chimes),
		Device: o.DeviceID,
		Read:   o.Settings.Read,
		Save: func(s Setting, v string) error {
			before := o.Settings.Read().Doors
			err := setting.Store(o.Settings, s, v)
			if o.Settings.Read().Doors != before {
				d.restartWatch()
			}
			return err
		},
	}
	d.status = &esphome.TextSensor{Base: esphome.Base{
		ObjectID: "doorbell_status", Name: "Doorbell status", Icon: "mdi:doorbell-video",
		Category: esphome.CategoryDiagnostic, DeviceID: o.DeviceID,
	}}
	return d
}

func (d *Doorbell) Name() string { return "doorbell" }

func (d *Doorbell) Entities() []esphome.Entity { return append(d.knobs.Entities(), d.status) }

func (d *Doorbell) Settings() *Controls { return d.knobs }

func (d *Doorbell) Restore(c schema.Doorbell) {
	d.knobs.PublishFrom(c)
	d.status.Set(d.State().Says())
}

func (d *Doorbell) State() State {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state
}

func (d *Doorbell) Forget(slot int) error {
	return d.UpdateDoor(slot, func(door *schema.DoorbellDoor) { *door = schema.DoorbellDoor{} })
}

func (d *Doorbell) UpdateDoor(slot int, change func(*schema.DoorbellDoor)) error {
	if slot < 0 || slot >= schema.DoorbellSlots {
		return errUnsupported
	}
	err := d.o.Settings.Update(func(c *schema.Doorbell) { change(&c.Doors[slot]) })
	d.knobs.PublishFrom(d.o.Settings.Read())
	d.restartWatch()
	return err
}

func (d *Doorbell) restartWatch() {
	d.mu.Lock()
	restart := d.restart
	d.mu.Unlock()
	if restart != nil {
		restart()
	}
}

func (d *Doorbell) Door(slot int) DoorState {
	if _, ok := doorAt(d.o.Settings.Read(), slot); !ok {
		return DoorState{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.slots[slot]
	if s.Events == Unset {
		s.Events = Connecting
	}
	return s
}

func (d *Doorbell) Recent() []Event {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.recent)
}

func (d *Doorbell) ReolinkDevices(ctx context.Context) ([]homeassistant.ReolinkDevice, error) {
	if d.o.Finder == nil {
		return nil, nil
	}
	return d.o.Finder.ReolinkDevices(ctx)
}

func (d *Doorbell) Device(
	ctx context.Context,
	door schema.DoorbellDoor,
) (homeassistant.ReolinkDevice, bool) {
	if door.Device == "" {
		return homeassistant.ReolinkDevice{}, false
	}
	found, err := d.ReolinkDevices(ctx)
	if err != nil {
		return homeassistant.ReolinkDevice{}, false
	}
	i := slices.IndexFunc(
		found,
		func(f homeassistant.ReolinkDevice) bool { return f.Device == door.Device },
	)
	if i < 0 {
		return homeassistant.ReolinkDevice{}, false
	}
	return found[i], true
}

func (d *Doorbell) Camera(ctx context.Context, slot int) (string, bool) {
	door, ok := doorAt(d.o.Settings.Read(), slot)
	if !ok {
		return "", false
	}
	if door.Camera != "" {
		return door.Camera, true
	}
	dev, ok := d.Device(ctx, door)
	if !ok || len(dev.Cameras) == 0 {
		return "", false
	}
	return dev.Cameras[0].Entity, true
}

func sensorFor(door schema.DoorbellDoor, k Kind) string {
	switch k {
	case Visitor:
		return door.Sensors.Visitor
	case Person:
		return door.Sensors.Person
	case Package:
		return door.Sensors.Package
	case Vehicle:
		return door.Sensors.Vehicle
	case Animal:
		return door.Sensors.Animal
	}
	return ""
}

func (d *Doorbell) Doors() []Door {
	var out []Door
	for i, door := range d.o.Settings.Read().Doors {
		if door.Set() {
			out = append(out, Door{Slot: i, Name: door.Name})
		}
	}
	return out
}

type Door struct {
	Slot int
	Name string
}

func (d *Doorbell) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		d.watchAll(ctx, d.o.Settings.Read().Doors)
	}
	return nil
}

func (d *Doorbell) watchAll(ctx context.Context, doors [schema.DoorbellSlots]schema.DoorbellDoor) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	d.mu.Lock()
	d.slots = [schema.DoorbellSlots]DoorState{}
	d.restart = cancel
	d.mu.Unlock()

	var wg sync.WaitGroup
	for i, door := range doors {
		if !door.Set() {
			continue
		}
		d.mark(i, Connecting)
		wg.Go(func() { d.watchSlot(ctx, i, door) })
	}
	d.summarize()
	for ctx.Err() == nil && d.o.Settings.Read().Doors == doors {
		sleep(ctx, recheck)
	}
	cancel()
	wg.Wait()
}

func (d *Doorbell) watchSlot(ctx context.Context, slot int, door schema.DoorbellDoor) {
	err := service.Keep(ctx, "doorbell events: "+door.Name, func(ctx context.Context) error {
		err := d.feed(ctx, slot, door)
		switch {
		case errors.Is(err, ErrBadLogin):
			return service.Permanent(err)
		case err == nil && ctx.Err() == nil:
			return errEventsEnded
		}
		return err
	}, service.Restart(retryFirst, retryMost))
	if err != nil {
		slog.Warn("doorbell events stopped", "door", door.Name, "err", err)
		<-ctx.Done()
	}
}

func (d *Doorbell) feed(ctx context.Context, slot int, door schema.DoorbellDoor) error {
	switch door.Events {
	case schema.FeedReolink:
		return d.reolinkEvents(ctx, slot, door)
	case schema.FeedONVIF:
		return d.onvifEvents(ctx, slot, door)
	case schema.FeedAssistant:
		return d.assistantEvents(ctx, slot, door, "")
	}
	if !door.Signed() {
		return d.assistantEvents(ctx, slot, door, "")
	}
	wait := reolinkFirst
	for {
		began := time.Now()
		err := d.reolinkEvents(ctx, slot, door)
		if ctx.Err() != nil || !errors.Is(err, ErrUnreachable) {
			return err
		}
		if time.Since(began) >= steady {
			wait = reolinkFirst
		}
		why := tried(sourceReolink, err)
		slog.Warn("doorbell events fall back to Home Assistant",
			"door", door.Name, "why", why, "reolink_again_in", wait)
		hold, cancel := context.WithTimeout(ctx, wait)
		err = d.assistantEvents(hold, slot, door, why)
		cancel()
		wait = min(wait*2, reolinkMost)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func tried(source string, err error) string {
	return say.F("doorbell.tried", map[string]any{
		"Source": say.T("doorbell.source." + source),
		"Why":    strings.ReplaceAll(err.Error(), "\n", ": "),
	})
}

func (d *Doorbell) assistantEvents(
	ctx context.Context,
	slot int,
	door schema.DoorbellDoor,
	why string,
) error {
	if d.o.States == nil {
		d.mark(slot, Unsupported)
		return errUnsupported
	}
	var stops []func()
	for _, k := range kinds {
		entity := sensorFor(door, k)
		if entity == "" {
			continue
		}
		v := d.o.States.Follow(entity, "")
		var (
			mu   sync.Mutex
			seen bool
			was  string
		)
		stops = append(stops, v.Changed.Listen(func(state string) {
			mu.Lock()
			rose := seen && was != "on" && state == "on"
			seen, was = true, state
			mu.Unlock()
			if rose {
				go d.record(ctx, slot, door, k)
			}
		}))
	}
	if len(stops) == 0 {
		d.mark(slot, Unsupported)
		return errUnsupported
	}
	defer func() {
		for _, stop := range stops {
			stop()
		}
	}()
	d.via(slot, sourceAssistant, why)
	d.mark(slot, Ready)
	<-ctx.Done()
	return nil
}

func (d *Doorbell) Rehearse(slot int, k Kind, picture []byte) error {
	door, ok := doorAt(d.o.Settings.Read(), slot)
	if !ok {
		return errUnsupported
	}
	if !slices.Contains(kinds, k) {
		return fmt.Errorf("doorbell: no event kind %q", k)
	}
	if picture != nil {
		d.emit(Event{Kind: k, Slot: slot, Door: door.Name, At: d.o.Now(), Snapshot: picture})
		return nil
	}
	go d.record(context.Background(), slot, door, k)
	return nil
}

func Kinds() []Kind { return slices.Clone(kinds) }

func (d *Doorbell) record(ctx context.Context, slot int, door schema.DoorbellDoor, k Kind) {
	ev := Event{Kind: k, Slot: slot, Door: door.Name, At: d.o.Now()}
	if pic, err := d.Snapshot(ctx, slot); err == nil {
		ev.Snapshot = pic
	}
	d.emit(ev)
}

func (d *Doorbell) emit(ev Event) {
	d.mu.Lock()
	d.recent = append([]Event{ev}, d.recent...)
	if len(d.recent) > keep {
		d.recent = d.recent[:keep]
	}
	d.mu.Unlock()
	d.Seen.Emit(ev)
}

const (
	sourceReolink   = string(schema.FeedReolink)
	sourceOnvif     = string(schema.FeedONVIF)
	sourceAssistant = string(schema.FeedAssistant)
	sourceRTSP      = string(schema.VideoRTSP)
)

type DoorState struct {
	Events    Status
	EventsVia string
	EventsWhy string
	Video     Status
	VideoVia  string
	VideoWhy  string
}

func (d *Doorbell) mark(slot int, s Status) {
	d.mu.Lock()
	d.slots[slot].Events = s
	d.mu.Unlock()
	d.summarize()
}

func (d *Doorbell) via(slot int, source, why string) {
	d.mu.Lock()
	d.slots[slot].EventsVia, d.slots[slot].EventsWhy = source, why
	d.mu.Unlock()
	d.summarize()
}

func (d *Doorbell) seeVideo(slot int, source string, s Status, why string) {
	d.mu.Lock()
	d.slots[slot].Video, d.slots[slot].VideoVia, d.slots[slot].VideoWhy = s, source, why
	d.mu.Unlock()
	d.summarize()
}

func (d *Doorbell) summarize() {
	cfg := d.o.Settings.Read()
	d.mu.Lock()
	ready, any, worst := 0, false, Status(0)
	for i, door := range cfg.Doors {
		if !door.Set() {
			continue
		}
		any = true
		switch s := d.slots[i].Events; s {
		case Ready:
			ready++
		default:
			if worst == 0 || s == BadLogin {
				worst = s
			}
		}
	}
	doors := d.slots
	d.mu.Unlock()
	switch {
	case !any:
		d.set(State{Status: Unset, Doors: doors})
	case ready > 0:
		d.set(State{Status: Ready, Count: ready, Doors: doors})
	default:
		d.set(State{Status: worst, Doors: doors})
	}
}

func (d *Doorbell) set(s State) {
	d.mu.Lock()
	same := d.state == s
	d.state = s
	d.mu.Unlock()
	if same {
		return
	}
	d.status.Set(s.Says())
	d.Changed.Emit(s)
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
