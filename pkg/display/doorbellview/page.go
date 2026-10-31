package doorbellview

import (
	"context"
	"log/slog"
	"slices"
	"sync"

	"github.com/ygelfand/libcountertop/pkg/camera/doorbell"
	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const segmentsMost = 4

var sliders = []string{"ring_for", "live_for"}

type found struct {
	mu      sync.Mutex
	list    []homeassistant.ReolinkDevice
	binary  []homeassistant.Named
	cameras []homeassistant.Named
	fetched bool
}

func (v *View) Page() *shell.Page {
	return &shell.Page{Title: say.T("doorbell.app"), Build: func() ([]menu.Row, []func(int)) {
		cfg := v.o.Settings.Read()
		var (
			rows []menu.Row
			taps []func(int)
		)
		for i, door := range cfg.Doors {
			if !door.Set() {
				continue
			}
			state := v.o.Bell.Door(i)
			rows = append(rows, menu.Row{
				Label: v.doorName(i),
				Kind:  menu.Chevron,
				Value: eventsLine(state),
				Hint:  v.videoLine(i, state),
			})
			taps = append(taps, func(int) { v.o.Shell.Push(v.doorPage(i)) })
		}
		if _, ok := freeSlot(cfg); ok {
			rows = append(rows, menu.Row{Label: say.T("doorbell.add"), Kind: menu.Chevron})
			taps = append(taps, func(int) { v.o.Shell.Push(v.addPage()) })
		}
		for _, name := range []string{"visitor", "chime", "ring_for", "live_for", "style", "notify", "tab"} {
			r, t := v.settingRow(name)
			rows, taps = append(rows, r), append(taps, t)
		}
		return rows, taps
	}}
}

func (v *View) lookup(f *found) {
	safe.Go("doorbell lookup", func() {
		ctx, cancel := context.WithTimeout(context.Background(), cameraWait)
		defer cancel()
		list, err := v.o.Bell.ReolinkDevices(ctx)
		if err != nil {
			slog.Debug("doorbell lookup", "err", err)
		}
		binary, err := v.o.Bell.BinarySensors(ctx)
		if err != nil {
			slog.Debug("doorbell sensor lookup", "err", err)
		}
		cameras, err := v.o.Bell.Cameras(ctx)
		if err != nil {
			slog.Debug("doorbell camera lookup", "err", err)
		}
		f.mu.Lock()
		f.list, f.binary, f.cameras, f.fetched = list, binary, cameras, true
		f.mu.Unlock()
		v.o.Shell.Redraw()
	})
}

func (f *found) get() ([]homeassistant.ReolinkDevice, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.list), f.fetched
}

func (f *found) sensors() []homeassistant.Named {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.binary)
}

func (f *found) allCameras() []homeassistant.Named {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.cameras)
}

func freeSlot(cfg schema.Doorbell) (int, bool) {
	slot := slices.IndexFunc(cfg.Doors[:], func(d schema.DoorbellDoor) bool { return !d.Set() })
	return slot, slot >= 0
}

func configured(cfg schema.Doorbell, match func(schema.DoorbellDoor) bool) bool {
	return slices.ContainsFunc(
		cfg.Doors[:],
		func(d schema.DoorbellDoor) bool { return d.Set() && match(d) },
	)
}

func (v *View) settingRow(name string) (menu.Row, func(int)) {
	c := v.o.Bell.Settings()
	at := c.Read()
	s, ok := find(c, name)
	if !ok {
		return menu.Row{Label: name, Dim: true}, nil
	}
	row := menu.Row{Label: s.Title()}
	save := func(value string) { v.change(name, value) }
	switch s.Kind {
	case setting.Text:
		row.Kind, row.Value, row.Save = menu.Field, s.Read(&at), save
		return row, nil
	case setting.Secret, setting.Credential:
		row.Kind, row.Value, row.Save = menu.Password, s.Read(&at), save
		return row, nil
	case setting.Toggle:
		on, _ := setting.Boolean(s.Read(&at))
		row.Kind, row.On = menu.Toggle, on
		return row, func(int) { save(setting.OnOff(!on)) }
	}
	values, labels := s.Values(), s.Labels()
	current := s.Read(&at)
	if slices.Contains(sliders, name) {
		steps := menu.Steps(len(values))
		row.Kind, row.Snap, row.Value = menu.Slider, steps.Snap, s.LabelOf(current)
		row.Level = steps.Level(max(slices.Index(values, current), 0))
		return row, func(level int) { save(values[steps.Index(level)]) }
	}
	if len(values) <= segmentsMost {
		row.Kind, row.Options = menu.Segments, labels
		row.Level = max(slices.Index(values, current), 0)
		return row, func(i int) {
			if i >= 0 && i < len(values) {
				save(values[i])
			}
		}
	}
	row.Kind, row.Value = menu.Chevron, s.LabelOf(current)
	return row, func(int) { v.o.Shell.Push(v.choicePage(name)) }
}

func (v *View) choicePage(name string) *shell.Page {
	c := v.o.Bell.Settings()
	s, _ := find(c, name)
	return &shell.Page{Title: s.Title(), Build: func() ([]menu.Row, []func(int)) {
		at := c.Read()
		current := s.Read(&at)
		values, labels := s.Values(), s.Labels()
		rows := make([]menu.Row, len(values))
		taps := make([]func(int), len(values))
		for i, value := range values {
			rows[i] = menu.Row{Label: labels[i], Chosen: value == current}
			taps[i] = func(int) {
				v.change(name, value)
				if name == "chime" && v.o.Chime != nil {
					v.o.Chime(schema.Chime(value))
				}
			}
		}
		return rows, taps
	}}
}

func find(c *doorbell.Controls, name string) (doorbell.Setting, bool) {
	for _, s := range c.Table.Rows() {
		if s.Name == name {
			return s, true
		}
	}
	return doorbell.Setting{}, false
}

func (v *View) change(name, value string) {
	if err := v.o.Bell.Settings().Change(name, value); err != nil {
		slog.Error("saving a setting failed", "setting", "doorbell."+name, "err", err)
	}
}
