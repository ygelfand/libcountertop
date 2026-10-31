package doorbellview

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/ygelfand/libcountertop/pkg/camera/doorbell"
	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type sensorKind struct {
	field string
	at    func(*schema.DoorbellSensors) *string
}

var sensorKinds = []sensorKind{
	{"visitor", func(s *schema.DoorbellSensors) *string { return &s.Visitor }},
	{"person", func(s *schema.DoorbellSensors) *string { return &s.Person }},
	{"package", func(s *schema.DoorbellSensors) *string { return &s.Package }},
	{"vehicle", func(s *schema.DoorbellSensors) *string { return &s.Vehicle }},
	{"animal", func(s *schema.DoorbellSensors) *string { return &s.Animal }},
	{"motion", func(s *schema.DoorbellSensors) *string { return &s.Motion }},
}

func (v *View) addPage() *shell.Page {
	f := &found{}
	v.lookup(f)
	return &shell.Page{Title: say.T("doorbell.add"), Build: func() ([]menu.Row, []func(int)) {
		var (
			rows []menu.Row
			taps []func(int)
		)
		list, fetched := f.get()
		switch {
		case !fetched:
			rows = append(rows, menu.Row{Label: say.T("doorbell.looking"), Dim: true})
			taps = append(taps, nil)
		case len(list) > 0:
			rows = append(rows, menu.Row{
				Label: say.T("doorbell.source.ha_reolink"),
				Kind:  menu.Chevron,
				Value: say.F("doorbell.cameras", map[string]any{"N": len(list)}),
			})
			taps = append(taps, func(int) { v.o.Shell.Push(v.reolinkPage(f)) })
		}
		rows = append(rows,
			menu.Row{Label: say.T("doorbell.source.nvr"), Kind: menu.Chevron},
			menu.Row{Label: say.T("doorbell.manual"), Kind: menu.Chevron},
		)
		taps = append(taps,
			func(int) { v.o.Shell.Push(v.nvrPage()) },
			func(int) {
				v.o.Shell.Pop()
				v.o.Shell.Push(v.manualPage())
			},
		)
		return rows, taps
	}}
}

func (v *View) reolinkPage(f *found) *shell.Page {
	return &shell.Page{
		Title: say.T("doorbell.source.ha_reolink"),
		Build: func() ([]menu.Row, []func(int)) {
			cfg := v.o.Settings.Read()
			list, _ := f.get()
			var (
				rows []menu.Row
				taps []func(int)
			)
			for _, d := range list {
				added := configured(
					cfg,
					func(door schema.DoorbellDoor) bool { return door.Device == d.Device },
				)
				row := menu.Row{Label: d.Name, Hint: d.Model, Value: say.T("doorbell.autosetup")}
				if added {
					row.Value, row.Dim = say.T("doorbell.added"), true
					rows, taps = append(rows, row), append(taps, nil)
					continue
				}
				rows = append(rows, row)
				taps = append(taps, func(int) { v.setupReolink(d) })
			}
			return rows, taps
		},
	}
}

func (v *View) setupReolink(d homeassistant.ReolinkDevice) {
	slot, ok := freeSlot(v.o.Settings.Read())
	if !ok {
		return
	}
	v.updateDoor(slot, func(door *schema.DoorbellDoor) {
		*door = schema.DoorbellDoor{
			Name:   d.Name,
			Device: d.Device,
			UID:    d.UID,
			User:   schema.DoorbellUser,
			Video:  schema.VideoAssistant,
			Events: schema.FeedAssistant,
		}
		if len(d.Cameras) > 0 {
			door.Camera = d.Cameras[0].Entity
		}
		door.Sensors = schema.DoorbellSensors{
			Visitor: d.Sensors["visitor"],
			Person:  d.Sensors["person"],
			Package: d.Sensors["package"],
			Vehicle: d.Sensors["vehicle"],
			Animal:  d.Sensors["pet"],
		}
	})
}

func (v *View) updateDoor(slot int, change func(*schema.DoorbellDoor)) {
	if err := v.o.Bell.UpdateDoor(slot, change); err != nil {
		slog.Error("saving a doorbell failed", "slot", slot, "err", err)
	}
	v.o.Shell.Redraw()
}

type nvrForm struct {
	mu       sync.Mutex
	host     string
	user     string
	password string
	busy     bool
	err      error
	channels []doorbell.Channel
}

func (v *View) nvrPage() *shell.Page {
	n := &nvrForm{user: schema.DoorbellUser}
	field := func(label string, kind menu.Kind, at *string) menu.Row {
		n.mu.Lock()
		value := *at
		n.mu.Unlock()
		return menu.Row{Label: say.T(label), Kind: kind, Value: value, Save: func(s string) {
			n.mu.Lock()
			*at = s
			n.mu.Unlock()
			v.o.Shell.Redraw()
		}}
	}
	connect := func() {
		n.mu.Lock()
		if n.busy || n.host == "" {
			n.mu.Unlock()
			return
		}
		n.busy, n.err, n.channels = true, nil, nil
		host, user, password := n.host, n.user, n.password
		n.mu.Unlock()
		v.o.Shell.Redraw()
		safe.Go("doorbell nvr", func() {
			ctx, cancel := context.WithTimeout(context.Background(), snapWait)
			defer cancel()
			channels, err := doorbell.Channels(ctx, host, user, password)
			n.mu.Lock()
			n.busy, n.err, n.channels = false, err, channels
			n.mu.Unlock()
			v.o.Shell.Redraw()
		})
	}
	return &shell.Page{
		Title: say.T("doorbell.source.nvr"),
		Build: func() ([]menu.Row, []func(int)) {
			rows := []menu.Row{
				field("doorbell.field.host", menu.Field, &n.host),
				field("doorbell.field.user", menu.Field, &n.user),
				field("doorbell.field.password", menu.Password, &n.password),
			}
			taps := []func(int){nil, nil, nil}
			n.mu.Lock()
			busy, err, channels := n.busy, n.err, n.channels
			host, user, password := n.host, n.user, n.password
			n.mu.Unlock()
			switch {
			case busy:
				rows, taps = append(
					rows,
					menu.Row{Label: say.T("doorbell.connecting"), Dim: true},
				), append(
					taps,
					nil,
				)
			case errors.Is(err, doorbell.ErrNVRLogin):
				rows, taps = append(
					rows,
					menu.Row{Label: say.T("doorbell.status.login"), Dim: true},
				), append(
					taps,
					nil,
				)
			case err != nil:
				rows = append(
					rows,
					menu.Row{
						Label: say.T("doorbell.status.unreachable"),
						Hint:  err.Error(),
						Dim:   true,
					},
				)
				taps = append(taps, nil)
			}
			cfg := v.o.Settings.Read()
			for _, ch := range channels {
				added := configured(cfg, func(d schema.DoorbellDoor) bool {
					return d.Host == host && d.Channel == ch.Number
				})
				row := menu.Row{
					Label: ch.Name,
					Hint:  say.F("doorbell.channel", map[string]any{"N": ch.Number + 1}),
					Value: say.T("doorbell.autosetup"),
				}
				switch {
				case added:
					row.Value, row.Dim = say.T("doorbell.added"), true
					rows, taps = append(rows, row), append(taps, nil)
					continue
				case !ch.Online:
					row.Value = say.T("doorbell.offline")
				}
				rows = append(rows, row)
				taps = append(taps, func(int) { v.setupChannel(host, user, password, ch) })
			}
			return rows, taps
		},
		Actions: []shell.Action{{Label: say.T("doorbell.connect"), Do: connect}},
	}
}

func (v *View) setupChannel(host, user, password string, ch doorbell.Channel) {
	slot, ok := freeSlot(v.o.Settings.Read())
	if !ok {
		return
	}
	v.updateDoor(slot, func(door *schema.DoorbellDoor) {
		*door = schema.DoorbellDoor{
			Name:     ch.Name,
			Host:     host,
			User:     user,
			Password: password,
			Channel:  ch.Number,
			Video:    schema.VideoONVIF,
			Events:   schema.FeedONVIF,
			Talk:     schema.TalkONVIF,
		}
	})
}

func sourceName(source string) string {
	if source == "" {
		return ""
	}
	return say.T("doorbell.source." + source)
}

func eventsLine(s doorbell.DoorState) string {
	if s.EventsVia == "" {
		return s.Events.Says()
	}
	line := say.F(
		"doorbell.events.via",
		map[string]any{"State": s.Events.Says(), "Source": sourceName(s.EventsVia)},
	)
	if s.EventsWhy != "" {
		line = say.F("doorbell.fellback", map[string]any{"Line": line})
	}
	return line
}

func (v *View) videoLine(slot int, s doorbell.DoorState) string {
	source := sourceName(s.VideoVia)
	switch {
	case s.VideoVia == "":
		return sourceName(v.o.Bell.PlannedVideo(slot))
	case s.Video == doorbell.Unset && s.VideoWhy != "":
		return say.F("doorbell.fellback", map[string]any{"Line": source})
	case s.Video == doorbell.Unset:
		return source
	case s.Video == doorbell.Ready:
		return say.F("doorbell.video.working", map[string]any{"Source": source})
	case s.Video == doorbell.Connecting:
		return say.F("doorbell.video.connecting", map[string]any{"Source": source})
	}
	return say.F("doorbell.video.failed", map[string]any{"Source": source, "Why": s.Video.Says()})
}
