package webapi

import (
	"encoding/json"
	"fmt"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
)

const (
	skippedMissing = "missing"
	skippedLocal   = "local"
)

type exported struct {
	Domain  string `json:"domain"`
	Section string `json:"section"`
	Group   string `json:"group"`
	Name    string `json:"name"`
	Title   string `json:"title"`
	Kind    string `json:"kind"`
	Value   string `json:"value"`
}

type skipped struct {
	Domain string `json:"domain"`
	Name   string `json:"name"`
	Why    string `json:"why"`
}

type imported struct {
	Changed int       `json:"changed"`
	Skipped []skipped `json:"skipped"`
	Errors  string    `json:"errors,omitempty"`
}

func (a *API) Actions() []*esphome.Action {
	return []*esphome.Action{
		{
			Name:        "settings_export",
			Description: "The device's settings that can move to another device.",
			Answers:     true,
			Run: func(esphome.Call) (any, error) {
				return map[string]any{"settings": a.portable()}, nil
			},
		},
		{
			Name:        "settings_import",
			Description: "Apply settings exported from a device, reporting any it skipped.",
			Answers:     true,
			Args: []esphome.Arg{{
				Name:        "settings",
				Type:        esphome.ArgString,
				Description: "The JSON settings_export answered.",
			}},
			Run: func(c esphome.Call) (any, error) {
				var want setting.Snapshot
				if err := json.Unmarshal([]byte(c.String("settings")), &want); err != nil {
					return nil, fmt.Errorf("settings: %w", err)
				}
				return a.apply(want), nil
			},
		},
	}
}

func (a *API) portable() []exported {
	values := a.o.Registry.Export()
	out := []exported{}
	for _, b := range a.o.Registry.All() {
		d, ok := b.(setting.Described)
		if !ok {
			continue
		}
		_, key := sectionOf(b.Domain())
		for _, f := range d.Fields() {
			value, ok := values[b.Domain()][f.Name]
			if !ok {
				continue
			}
			out = append(out, exported{
				Domain:  b.Domain(),
				Section: say.T(key),
				Group:   f.Group,
				Name:    f.Name,
				Title:   f.Title,
				Kind:    f.Kind,
				Value:   value,
			})
		}
	}
	return out
}

func (a *API) apply(want setting.Snapshot) imported {
	values := a.o.Registry.Export()
	known := map[string]map[string]bool{}
	for _, b := range a.o.Registry.All() {
		d, ok := b.(setting.Described)
		if !ok {
			continue
		}
		known[b.Domain()] = map[string]bool{}
		for _, f := range d.Fields() {
			known[b.Domain()][f.Name] = true
		}
	}
	keep := setting.Snapshot{}
	out := imported{Skipped: []skipped{}}
	for domain, names := range want {
		for name, value := range names {
			switch _, portable := values[domain][name]; {
			case portable:
				if keep[domain] == nil {
					keep[domain] = map[string]string{}
				}
				keep[domain][name] = value
			case known[domain][name]:
				out.Skipped = append(out.Skipped, skipped{domain, name, skippedLocal})
			default:
				out.Skipped = append(out.Skipped, skipped{domain, name, skippedMissing})
			}
		}
	}
	changed, err := a.o.Registry.Import(keep)
	out.Changed = changed
	if err != nil {
		out.Errors = err.Error()
	}
	return out
}
