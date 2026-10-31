package call

import (
	"errors"
	"slices"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/actions"
	"github.com/ygelfand/libcountertop/pkg/match"
	discovery "github.com/ygelfand/libcountertop/pkg/network/peer"
	"github.com/ygelfand/libcountertop/pkg/say"
)

func (c *Calls) Action(peers func() []discovery.Peer, video bool) actions.Action {
	triggers := []actions.Trigger{{Sentence: "(call|ring|intercom) [the] {peer}"}}
	if video {
		triggers = append(triggers, actions.Trigger{
			Sentence: "video call [the] {peer}",
			Args:     map[string]string{"video": "true"},
		})
	}
	return actions.Action{
		Name:  "call",
		Title: say.T("actions.call"),
		Args: []esphome.Arg{
			{Name: "peer", Type: esphome.ArgString, Description: "The name of the device to call."},
			{
				Name:        "video",
				Type:        esphome.ArgBool,
				Description: "A video call when both devices have cameras.",
			},
		},
		Triggers: triggers,
		Run: func(call esphome.Call) (string, error) {
			name := call.String("peer")
			p, ok := Find(peers(), name)
			if !ok {
				return say.F("actions.call.unknown", map[string]any{"Peer": name}), nil
			}
			err := c.Start(p, video && call.Bool("video") && p.Video())
			switch {
			case errors.Is(err, ErrBusy):
				return say.T("actions.call.busy"), nil
			case errors.Is(err, ErrForeign):
				return say.F("actions.call.foreign", map[string]any{"Peer": p.Name}), nil
			case err != nil:
				return say.F("actions.call.unreachable", map[string]any{"Peer": p.Name}), nil
			}
			return "", nil
		},
	}
}

func (c *Calls) ControlActions() []actions.Action {
	return []actions.Action{
		{
			Name: "call_answer", Title: say.T("actions.call_answer"),
			Args: []esphome.Arg{{
				Name:        "video",
				Type:        esphome.ArgBool,
				Description: "Answer with video; off answers with audio only.",
			}},
			Triggers: []actions.Trigger{
				{Sentence: "answer [the] (call|phone)"},
				{Sentence: "answer"},
				{Sentence: "answer [the] video call", Args: map[string]string{"video": "true"}},
			},
			Run: func(call esphome.Call) (string, error) {
				if now, ok := c.Now(); !ok || now.State != Ringing {
					return say.T("actions.call_answer.none"), nil
				}
				if call.Bool("video") {
					c.Answer()
				} else {
					c.AnswerAudio()
				}
				return say.T("actions.call_answer.done"), nil
			},
		},
		{
			Name: "call_hangup", Title: say.T("actions.call_hangup"),
			Triggers: []actions.Trigger{
				{Sentence: "hang up"},
				{Sentence: "hang up [the] (call|phone)"},
				{Sentence: "(end|decline|reject) [the] call"},
			},
			Run: func(esphome.Call) (string, error) {
				if _, ok := c.Now(); !ok {
					return say.T("actions.call_hangup.none"), nil
				}
				c.Hangup()
				return say.T("actions.call_hangup.done"), nil
			},
		},
	}
}

func Find(peers []discovery.Peer, name string) (discovery.Peer, bool) {
	candidates := make([]match.Candidate, len(peers))
	for i, p := range peers {
		candidates[i] = match.Candidate{Key: p.ID, Name: p.Name}
	}
	id, ok := match.Best(name, candidates)
	if !ok {
		return discovery.Peer{}, false
	}
	i := slices.IndexFunc(peers, func(p discovery.Peer) bool { return p.ID == id })
	return peers[i], true
}
