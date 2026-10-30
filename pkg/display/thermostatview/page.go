package thermostatview

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/thermostat"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type climates struct {
	mu      sync.Mutex
	list    []homeassistant.Entity
	err     error
	fetched bool
}

func (v *View) Page() *shell.Page {
	return &shell.Page{Title: say.T("thermostat.app"), Build: func() ([]menu.Row, []func(int)) {
		c := v.o.Thermostats.Settings()
		at := c.Read()
		chosen := len(at.Entities)
		rows := []menu.Row{{
			Label: say.T("thermostat.setting.entities"),
			Kind:  menu.Chevron,
			Value: say.F(
				"thermostat.chosen",
				map[string]any{"N": chosen, "Of": schema.ThermostatsMost},
			),
		}}
		taps := []func(int){func(int) { v.o.Shell.Push(v.climatesPage()) }}
		for _, s := range c.Table.Rows() {
			current := s.Read(&at)
			row := menu.Row{Label: s.Title()}
			switch s.Kind {
			case setting.Toggle:
				on, _ := setting.Boolean(current)
				row.Kind, row.On = menu.Toggle, on
				taps = append(taps, func(int) { v.change(s.Name, setting.OnOff(!on)) })
			default:
				row.Kind, row.Value = menu.Chevron, s.LabelOf(current)
				taps = append(taps, func(int) { v.o.Shell.Push(v.lookPage(s)) })
			}
			rows = append(rows, row)
		}
		return rows, taps
	}}
}

func (v *View) lookPage(s thermostat.Setting) *shell.Page {
	return &shell.Page{Title: s.Title(), Tiles: func() ([]menu.Cell, []func(int)) {
		at := v.o.Thermostats.Settings().Read()
		values, labels := s.Values(), s.Labels()
		cells := make([]menu.Cell, len(values))
		taps := make([]func(int), len(values))
		for i, value := range values {
			l := schema.ThermostatLook(value)
			cells[i] = menu.Cell{Label: labels[i], Chosen: l == at.Look, Preview: preview{v, l}}
			taps[i] = func(int) { v.change(s.Name, value) }
		}
		return cells, taps
	}}
}

func (v *View) climatesPage() *shell.Page {
	p := &climates{}
	safe.Go("thermostat climates", func() {
		ctx, cancel := context.WithTimeout(context.Background(), sendWait)
		defer cancel()
		list, err := v.o.Thermostats.Climates(ctx)
		p.mu.Lock()
		p.list, p.err, p.fetched = list, err, true
		p.mu.Unlock()
		v.o.Shell.Redraw()
	})
	return &shell.Page{
		Title: say.T("thermostat.setting.entities"),
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
				return []menu.Row{{Label: say.T("thermostat.none")}}, []func(int){nil}
			}
			chosen := v.o.Thermostats.Chosen()
			full := len(chosen) >= schema.ThermostatsMost
			rows := make([]menu.Row, len(list))
			taps := make([]func(int), len(list))
			for i, e := range list {
				on := slices.Contains(chosen, e.ID)
				rows[i] = menu.Row{Label: e.Name, Hint: e.ID, Kind: menu.Toggle, On: on}
				if full && !on {
					rows[i].Hint = say.F(
						"thermostat.full",
						map[string]any{"N": schema.ThermostatsMost},
					)
					taps[i] = nil
					continue
				}
				taps[i] = func(int) {
					if err := v.o.Thermostats.Choose(e.ID, !on); err != nil {
						slog.Error("saving the thermostats failed", "err", err)
					}
					v.o.Shell.Redraw()
				}
			}
			return rows, taps
		},
	}
}

func (v *View) change(name, value string) {
	if err := v.o.Thermostats.Settings().Change(name, value); err != nil {
		slog.Error("saving a setting failed", "setting", "thermostat."+name, "err", err)
	}
	v.o.Shell.Redraw()
}

type preview struct {
	v    *View
	look schema.ThermostatLook
}

func (p preview) View(w *gogui.Window, width, height float32) gogui.View {
	list := p.v.shown()
	st := thermostat.Samples(1, p.v.o.Thermostats.Unit())[0]
	if len(list) > 0 && list[0].Known() {
		st = list[0]
	}
	f := &face{
		v: p.v, w: w, vw: width, vh: height, st: st, unit: p.v.o.Thermostats.Unit(),
		still: true, now: time.Now(),
	}
	lk := lookOf(p.look)
	body := f.render(lk, false)
	return gogui.Column(gogui.ContainerCfg{
		Width:   width,
		Height:  height,
		Sizing:  gogui.FixedFixed,
		Color:   lk.back,
		Radius:  gogui.RadiusMedium,
		Padding: gogui.NoPadding,
		Clip:    true,
		Content: []gogui.View{body},
	})
}
