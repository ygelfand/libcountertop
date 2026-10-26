package homealarmview

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/homealarm"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type panels struct {
	mu      sync.Mutex
	list    []homeassistant.Entity
	err     error
	fetched bool
}

func (v *View) Page() *shell.Page {
	return &shell.Page{Title: say.T("homealarm.app"), Build: func() ([]menu.Row, []func(int)) {
		c := v.o.Alarm.Settings()
		at := c.Read()
		var (
			rows []menu.Row
			taps []func(int)
		)
		for _, s := range c.Table.Rows() {
			r, t := v.settingRow(s, at)
			rows, taps = append(rows, r), append(taps, t)
			if s.Name == "look" {
				if zones := v.o.Alarm.Zones(); len(zones) > 0 {
					shown := len(zones) - len(at.Hide)
					rows = append(rows, menu.Row{
						Label: say.T("homealarm.setting.zones"),
						Kind:  menu.Chevron,
						Value: say.F(
							"homealarm.zones.shown",
							map[string]any{"N": shown, "Of": len(zones)},
						),
					})
					taps = append(taps, func(int) { v.o.Shell.Push(v.zonesPage()) })
				}
			}
		}
		return rows, taps
	}}
}

func (v *View) settingRow(s homealarm.Setting, at schema.HomeAlarm) (menu.Row, func(int)) {
	row := menu.Row{Label: s.Title()}
	current := s.Read(&at)
	switch {
	case s.Name == "entity":
		row.Kind, row.Value = menu.Chevron, v.entityName(current)
		return row, func(int) { v.o.Shell.Push(v.panelsPage()) }
	case s.Kind == setting.Toggle:
		on, _ := setting.Boolean(current)
		row.Kind, row.On = menu.Toggle, on
		return row, func(int) { v.change(s.Name, setting.OnOff(!on)) }
	}
	if s.Name == "settled.for" {
		values := s.Values()
		steps := menu.Steps(len(values))
		row.Kind, row.Snap, row.Value = menu.Slider, steps.Snap, s.LabelOf(current)
		row.Level = steps.Level(max(slices.Index(values, current), 0))
		return row, func(level int) { v.change(s.Name, values[steps.Index(level)]) }
	}
	row.Kind, row.Value = menu.Chevron, s.LabelOf(current)
	if s.Name == "look" {
		return row, func(int) { v.o.Shell.Push(v.lookPage(s)) }
	}
	return row, func(int) { v.o.Shell.Push(v.choicePage(s)) }
}

func (v *View) lookPage(s homealarm.Setting) *shell.Page {
	return &shell.Page{Title: s.Title(), Tiles: func() ([]menu.Cell, []func(int)) {
		at := v.o.Alarm.Settings().Read()
		values, labels := s.Values(), s.Labels()
		cells := make([]menu.Cell, len(values))
		taps := make([]func(int), len(values))
		for i, value := range values {
			look := schema.HomeAlarmLook(value)
			cells[i] = menu.Cell{
				Label:   labels[i],
				Chosen:  look == at.Look,
				Preview: v.preview(look),
			}
			taps[i] = func(int) { v.change(s.Name, value) }
		}
		return cells, taps
	}}
}

func (v *View) choicePage(s homealarm.Setting) *shell.Page {
	sound := strings.HasSuffix(s.Name, ".sound")
	return &shell.Page{Title: s.Title(), Build: func() ([]menu.Row, []func(int)) {
		at := v.o.Alarm.Settings().Read()
		current := s.Read(&at)
		values, labels := s.Values(), s.Labels()
		rows := make([]menu.Row, len(values))
		taps := make([]func(int), len(values))
		for i, value := range values {
			rows[i] = menu.Row{Label: labels[i], Chosen: value == current}
			taps[i] = func(int) {
				v.change(s.Name, value)
				if sound {
					v.o.Chime(schema.Chime(value))
				}
			}
		}
		return rows, taps
	}}
}

func (v *View) panelsPage() *shell.Page {
	p := &panels{}
	safe.Go("home alarm panels", func() {
		ctx, cancel := context.WithTimeout(context.Background(), sendWait)
		defer cancel()
		list, err := v.o.Alarm.Panels(ctx)
		p.mu.Lock()
		p.list, p.err, p.fetched = list, err, true
		p.mu.Unlock()
		v.o.Shell.Redraw()
	})
	return &shell.Page{
		Title: say.T("homealarm.setting.entity"),
		Build: func() ([]menu.Row, []func(int)) {
			p.mu.Lock()
			list, err, fetched := p.list, p.err, p.fetched
			p.mu.Unlock()
			switch {
			case !fetched:
				return []menu.Row{{Label: say.T("home.loading")}}, []func(int){nil}
			case err != nil:
				return []menu.Row{
						{Label: say.T("home.failed"), Hint: err.Error()},
					}, []func(int){
						nil,
					}
			case len(list) == 0:
				return []menu.Row{{Label: say.T("homealarm.none")}}, []func(int){nil}
			}
			chosen := v.o.Settings.Read().Entity
			rows := make([]menu.Row, len(list))
			taps := make([]func(int), len(list))
			for i, e := range list {
				rows[i] = menu.Row{Label: e.Name, Hint: e.ID, Chosen: e.ID == chosen}
				taps[i] = func(int) {
					if err := v.o.Alarm.Choose(e.ID); err != nil {
						slog.Error("saving the home alarm panel failed", "err", err)
					}
					v.o.Shell.Pop()
				}
			}
			return rows, taps
		},
	}
}

func (v *View) zonesPage() *shell.Page {
	return &shell.Page{
		Title: say.T("homealarm.setting.zones"),
		Build: func() ([]menu.Row, []func(int)) {
			hidden := v.o.Settings.Read().Hide
			zones := v.o.Alarm.Zones()
			rows := make([]menu.Row, len(zones))
			taps := make([]func(int), len(zones))
			for i, z := range zones {
				shown := !slices.Contains(hidden, z.Entity)
				rows[i] = menu.Row{Label: z.Name, Hint: z.Entity, Kind: menu.Toggle, On: shown}
				taps[i] = func(int) {
					if err := v.o.Alarm.Hide(z.Entity, shown); err != nil {
						slog.Error("saving the home alarm zones failed", "err", err)
					}
					v.o.Shell.Redraw()
				}
			}
			return rows, taps
		},
	}
}

func (v *View) entityName(id string) string {
	if id == "" {
		return say.T("homealarm.pick")
	}
	if s := v.o.Alarm.State(); s.Entity == id && s.Name != "" {
		return s.Name
	}
	return id
}

func (v *View) change(name, value string) {
	if err := v.o.Alarm.Settings().Change(name, value); err != nil {
		slog.Error("saving a setting failed", "setting", "homealarm."+name, "err", err)
	}
	v.o.Shell.Redraw()
}
