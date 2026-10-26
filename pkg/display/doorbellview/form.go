package doorbellview

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/camera/doorbell"
	"github.com/ygelfand/libcountertop/pkg/display/menu"
	sharedsettings "github.com/ygelfand/libcountertop/pkg/display/settings"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const savedFor = time.Second

type form struct {
	v     *View
	slot  int
	fresh bool

	mu    sync.Mutex
	cfg   schema.Doorbell
	shown *shell.Busy
}

func (v *View) doorPage(slot int) *shell.Page {
	f := &form{v: v, slot: slot, cfg: v.o.Settings.Read()}
	return f.page(v.doorName(slot), shell.Action{
		Label: say.T("doorbell.forget"),
		Role:  shell.Danger,
		Do: func() {
			if err := v.o.Bell.Forget(slot); err != nil {
				slog.Error("removing a doorbell failed", "slot", slot, "err", err)
			}
			v.o.Shell.Pop()
		},
	})
}

func (v *View) manualPage() *shell.Page {
	cfg := v.o.Settings.Read()
	slot, _ := freeSlot(cfg)
	cfg.Doors[slot] = schema.DoorbellDoor{User: schema.DoorbellUser}
	f := &form{v: v, slot: slot, fresh: true, cfg: cfg}
	return f.page(say.T("doorbell.manual"))
}

func (f *form) page(title string, extra ...shell.Action) *shell.Page {
	actions := append([]shell.Action{
		{Label: say.T("doorbell.cancel"), Role: shell.Plain, Do: f.v.o.Shell.Pop},
		{Label: say.T("doorbell.save"), Do: f.save},
	}, extra...)
	return &shell.Page{Title: title, Build: f.build, Actions: actions, Busy: f.busy}
}

func (f *form) door() schema.DoorbellDoor {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cfg.Doors[f.slot]
}

func (f *form) edit(change func(*schema.DoorbellDoor)) {
	f.mu.Lock()
	change(&f.cfg.Doors[f.slot])
	f.mu.Unlock()
	f.v.o.Shell.Redraw()
}

func (f *form) build() ([]menu.Row, []func(int)) {
	var (
		rows []menu.Row
		taps []func(int)
	)
	add := func(r menu.Row, t func(int)) { rows, taps = append(rows, r), append(taps, t) }
	if !f.fresh {
		state := f.v.o.Bell.Door(f.slot)
		add(menu.Row{
			Label: say.T("doorbell.status.events"),
			Value: eventsLine(state),
			Hint:  state.EventsWhy,
		}, nil)
		add(menu.Row{
			Label: say.T("doorbell.status.video"),
			Value: f.v.videoLine(f.slot, state),
			Hint:  state.VideoWhy,
		}, nil)
	}
	door := f.door()
	text := func(label string, kind menu.Kind, value string, set func(*schema.DoorbellDoor, string)) {
		add(menu.Row{Label: say.T(label), Kind: kind, Value: value, Save: func(s string) {
			f.edit(func(d *schema.DoorbellDoor) { set(d, s) })
		}}, nil)
	}
	text("doorbell.field.name", menu.Field, door.Name,
		func(d *schema.DoorbellDoor, s string) { d.Name = s })
	text("doorbell.field.uid", menu.Field, door.UID,
		func(d *schema.DoorbellDoor, s string) { d.UID = s })
	text("doorbell.field.host", menu.Field, door.Host,
		func(d *schema.DoorbellDoor, s string) { d.Host = s })
	text("doorbell.field.user", menu.Field, door.User,
		func(d *schema.DoorbellDoor, s string) { d.User = s })
	text("doorbell.field.password", menu.Password, door.Password,
		func(d *schema.DoorbellDoor, s string) { d.Password = s })
	streams := schema.DoorbellStreams()
	add(sharedsettings.SegmentRow(say.T("doorbell.field.stream"), streams, door.Stream),
		sharedsettings.SegmentPick(streams, func(s schema.DoorbellStream) {
			f.edit(func(d *schema.DoorbellDoor) { d.Stream = s })
		}))
	for _, field := range []string{"video", "events", "talk"} {
		add(f.choiceRow(fmt.Sprintf("door%d.%s", f.slot+1, field)))
	}
	if door.Device != "" {
		add(menu.Row{Label: say.T("doorbell.camera.title"), Kind: menu.Chevron, Value: door.Camera},
			func(int) { f.v.o.Shell.Push(f.cameraPage()) })
		for _, s := range sensorKinds {
			title := say.T("doorbell.sensor." + s.field)
			value := *s.at(&door.Sensors)
			if value == "" {
				value = say.T("doorbell.sensor.none")
			}
			add(menu.Row{Label: title, Kind: menu.Chevron, Value: value},
				func(int) { f.v.o.Shell.Push(f.sensorPage(s, title)) })
		}
	}
	return rows, taps
}

func (f *form) busy() (shell.Busy, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.shown == nil {
		return shell.Busy{}, false
	}
	return *f.shown, true
}

func (f *form) show(b *shell.Busy) {
	f.mu.Lock()
	f.shown = b
	f.mu.Unlock()
	f.v.o.Shell.Redraw()
}

func (f *form) refuse(why string) {
	f.show(&shell.Busy{
		Text:    say.T("doorbell.notsaved"),
		Detail:  why,
		Outcome: shell.Failed,
		Close:   say.T("doorbell.ok"),
		Dismiss: func() { f.show(nil) },
	})
}

func (f *form) save() {
	f.mu.Lock()
	busy := f.shown != nil
	door := f.cfg.Doors[f.slot]
	f.mu.Unlock()
	switch {
	case busy:
		return
	case f.fresh && door.UID == "" && door.Host == "":
		f.refuse(say.T("doorbell.needs"))
		return
	case !door.Signed() && door.Device == "":
		f.refuse(say.T("doorbell.why.nopassword"))
		return
	}
	f.show(&shell.Busy{Text: say.T("doorbell.checking")})
	safe.Go("doorbell check", func() {
		if door.Signed() {
			pic, err := doorbell.Check(context.Background(), door)
			if err != nil {
				slog.Warn("doorbell check failed", "door", door.Name, "err", err)
				f.refuse(failure(err))
				return
			}
			f.v.shots.keep(doorKey(f.slot), pic, f.v.screenMost()/thumbShare)
		}
		f.v.updateDoor(f.slot, func(d *schema.DoorbellDoor) { *d = door })
		f.show(&shell.Busy{Text: say.T("doorbell.saved"), Outcome: shell.Succeeded})
		time.AfterFunc(savedFor, f.v.o.Shell.Pop)
	})
}

func (f *form) choiceRow(name string) (menu.Row, func(int)) {
	c := f.v.o.Bell.Settings()
	s, ok := find(c, name)
	if !ok {
		return menu.Row{Label: name, Dim: true}, nil
	}
	f.mu.Lock()
	current := s.Read(&f.cfg)
	f.mu.Unlock()
	values, labels := s.Values(), s.Labels()
	pick := func(value string) {
		f.mu.Lock()
		err := s.Write(&f.cfg, value)
		f.mu.Unlock()
		if err != nil {
			slog.Error("doorbell setting", "setting", name, "err", err)
		}
		f.v.o.Shell.Redraw()
	}
	row := menu.Row{Label: s.Title()}
	if len(values) <= segmentsMost {
		row.Kind, row.Options = menu.Segments, labels
		row.Level = max(slices.Index(values, current), 0)
		return row, func(i int) {
			if i >= 0 && i < len(values) {
				pick(values[i])
			}
		}
	}
	row.Kind, row.Value = menu.Chevron, s.LabelOf(current)
	return row, func(int) {
		f.v.o.Shell.Push(&shell.Page{Title: s.Title(), Build: func() ([]menu.Row, []func(int)) {
			f.mu.Lock()
			now := s.Read(&f.cfg)
			f.mu.Unlock()
			rows := make([]menu.Row, len(values))
			taps := make([]func(int), len(values))
			for i, value := range values {
				rows[i] = menu.Row{Label: labels[i], Chosen: value == now}
				taps[i] = func(int) { pick(value) }
			}
			return rows, taps
		}})
	}
}

func (f *form) cameraPage() *shell.Page {
	found := &found{}
	f.v.lookup(found)
	return &shell.Page{
		Title: say.T("doorbell.camera.title"),
		Build: func() ([]menu.Row, []func(int)) {
			door := f.door()
			var (
				rows []menu.Row
				taps []func(int)
			)
			list, fetched := found.get()
			if !fetched {
				rows = append(rows, menu.Row{Label: say.T("doorbell.looking"), Dim: true})
				taps = append(taps, nil)
			}
			for _, d := range list {
				if d.Device != door.Device {
					continue
				}
				for i, cam := range d.Cameras {
					chosen := door.Camera == cam.Entity || (door.Camera == "" && i == 0)
					rows = append(rows, menu.Row{Label: cam.Name, Hint: cam.Entity, Chosen: chosen})
					taps = append(taps, func(int) {
						f.edit(func(d *schema.DoorbellDoor) { d.Camera = cam.Entity })
					})
				}
			}
			return rows, taps
		},
	}
}

func (f *form) sensorPage(kind sensorKind, title string) *shell.Page {
	found := &found{}
	f.v.lookup(found)
	pick := func(entity string) {
		f.edit(func(d *schema.DoorbellDoor) { *kind.at(&d.Sensors) = entity })
	}
	return &shell.Page{Title: title, Build: func() ([]menu.Row, []func(int)) {
		door := f.door()
		current := *kind.at(&door.Sensors)
		rows := []menu.Row{{Label: say.T("doorbell.sensor.none"), Chosen: current == ""}}
		taps := []func(int){func(int) { pick("") }}
		list, fetched := found.get()
		if !fetched {
			rows = append(rows, menu.Row{Label: say.T("doorbell.looking"), Dim: true})
			taps = append(taps, nil)
		}
		for _, d := range list {
			if d.Device != door.Device {
				continue
			}
			for _, b := range d.Binary {
				rows = append(
					rows,
					menu.Row{Label: b.Name, Hint: b.Entity, Chosen: current == b.Entity},
				)
				taps = append(taps, func(int) { pick(b.Entity) })
			}
		}
		return rows, taps
	}}
}
