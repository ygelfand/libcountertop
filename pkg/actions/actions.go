package actions

import (
	"encoding/json"
	"log/slog"
	"slices"
	"sync"

	esphome "github.com/ygelfand/go-esphome-device"
)

const (
	Event   = "esphome.countertop_actions"
	Version = 1
)

type Trigger struct {
	Sentence string            `json:"sentence"`
	Args     map[string]string `json:"args,omitempty"`
}

type Action struct {
	Name     string
	Title    string
	Args     []esphome.Arg
	Speech   string
	Triggers []Trigger
	Run      func(esphome.Call) (string, error)
}

type Registry struct {
	mu   sync.Mutex
	list []Action
}

func (r *Registry) Add(a Action) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.list = slices.DeleteFunc(r.list, func(have Action) bool { return have.Name == a.Name })
	r.list = append(r.list, a)
}

func (r *Registry) all() []Action {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.list)
}

func (r *Registry) Actions() []*esphome.Action {
	list := r.all()
	out := make([]*esphome.Action, 0, len(list))
	for _, a := range list {
		out = append(out, &esphome.Action{
			Name:        a.Name,
			Description: a.Title,
			Args:        a.Args,
			Answers:     true,
			Run: func(c esphome.Call) (any, error) {
				reply, err := a.Run(c)
				if err != nil {
					slog.Warn("action failed", "action", a.Name, "err", err)
					return nil, err
				}
				return map[string]any{"reply": reply}, nil
			},
		})
	}
	return out
}

type declared struct {
	Version int              `json:"version"`
	Actions []declaredAction `json:"actions"`
}

type declaredAction struct {
	Name     string        `json:"name"`
	Title    string        `json:"title"`
	Switch   string        `json:"switch,omitempty"`
	Args     []declaredArg `json:"args"`
	Triggers []Trigger     `json:"triggers,omitempty"`
}

type declaredArg struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

const speechType = "speech"

var argTypes = map[esphome.ArgType]string{
	esphome.ArgString: "string",
	esphome.ArgInt:    "int",
	esphome.ArgFloat:  "float",
	esphome.ArgBool:   "bool",
}

func (r *Registry) Names() []string {
	var out []string
	for _, a := range r.all() {
		out = append(out, a.Name)
	}
	return out
}

func (r *Registry) Declaration(
	voiced func(name string) bool,
	switched func(name string) string,
) map[string]string {
	d := declared{Version: Version, Actions: []declaredAction{}}
	for _, a := range r.all() {
		triggers := a.Triggers
		if !voiced(a.Name) {
			triggers = nil
		}
		var toggle string
		if switched != nil {
			toggle = switched(a.Name)
		}
		args := make([]declaredArg, len(a.Args))
		for i, arg := range a.Args {
			args[i] = declaredArg{Name: arg.Name, Type: argTypes[arg.Type]}
			if arg.Name == a.Speech {
				args[i].Type = speechType
			}
		}
		d.Actions = append(d.Actions, declaredAction{
			Name:     a.Name,
			Title:    a.Title,
			Switch:   toggle,
			Args:     args,
			Triggers: triggers,
		})
	}
	data, err := json.Marshal(d)
	if err != nil {
		slog.Error("declaring actions failed", "err", err)
		return nil
	}
	return map[string]string{"actions": string(data)}
}
