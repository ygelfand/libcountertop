package activity

import (
	"encoding/json"
	"log/slog"
	"maps"
	"strconv"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/actions"
	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/voice"
)

const (
	Calendar  = "calendar"
	Remind    = "remind"
	AddToList = "add_to_list"
	Reply     = "reply"

	replyKey = "actions.personal.reply."
)

var common = []string{"unrecognized", "unlinked", "failed"}

var outcomes = map[string][]string{
	Calendar:  {"calendar.none"},
	Remind:    {"no_phone", "no_notify", "remind.set", "remind.not_caught"},
	AddToList: {"add_to_list.no_list", "add_to_list.done", "add_to_list.not_caught"},
}

func (l *Log) VoiceAction() actions.Action {
	trigger := func(key, operation string, args map[string]string) actions.Trigger {
		fixed := map[string]string{"operation": operation}
		maps.Copy(fixed, args)
		return actions.Trigger{Key: "actions.personal.sentence." + key, Args: fixed}
	}
	return actions.Action{
		Name:  "personal",
		Title: say.T("actions.personal"),
		Args: []esphome.Arg{
			{
				Name:        "operation",
				Type:        esphome.ArgString,
				Description: "calendar, remind, add_to_list or reply.",
				Example:     Calendar,
			},
			{Name: "day", Type: esphome.ArgString, Description: "For calendar: today or tomorrow."},
			{Name: "task", Type: esphome.ArgString, Description: "For remind."},
			{Name: "minutes", Type: esphome.ArgString, Description: "For remind."},
			{Name: "hours", Type: esphome.ArgString, Description: "For remind."},
			{Name: "item", Type: esphome.ArgString, Description: "For add_to_list."},
			{Name: "key", Type: esphome.ArgString, Description: "For reply: the outcome to say."},
			{
				Name:        "data",
				Type:        esphome.ArgString,
				Description: "For reply: the outcome's values as JSON.",
			},
		},
		Triggers: []actions.Trigger{
			trigger("calendar", Calendar, map[string]string{"day": "today"}),
			trigger("calendar_tomorrow", Calendar, map[string]string{"day": "tomorrow"}),
			trigger("remind_minutes", Remind, nil),
			trigger("remind_hours", Remind, nil),
			trigger("add_to_list", AddToList, nil),
		},
		Answer:   l.answer,
		Protocol: homeassistant.PersonalProtocol,
	}
}

func (l *Log) answer(c esphome.Call) any {
	operation := c.String("operation")
	if operation == Reply {
		var data map[string]any
		if raw := c.String("data"); raw != "" {
			if err := json.Unmarshal([]byte(raw), &data); err != nil {
				slog.Warn("personal reply data", "key", c.String("key"), "err", err)
				return map[string]any{"reply": ""}
			}
		}
		return map[string]any{"reply": phrase(c.String("key"), whole(data))}
	}
	speaker := l.SpeakerWithin(speakerWait)
	slots := map[string]string{}
	for _, name := range []string{"minutes", "hours"} {
		if n, ok := count(c.String(name)); ok {
			slots[name] = strconv.Itoa(n)
		}
	}
	return map[string]any{
		"speaker": speaker,
		"replies": replies(operation, speaker, c, slots),
		"slots":   slots,
	}
}

func count(said string) (int, bool) {
	n, err := strconv.Atoi(voice.Normalize(said))
	return n, err == nil
}

func replies(
	operation string,
	speaker Speaker,
	c esphome.Call,
	slots map[string]string,
) map[string]string {
	data := map[string]any{
		"Name": speaker.Name,
		"Item": c.String("item"),
		"Task": c.String("task"),
	}
	if hours, ok := count(slots["hours"]); ok {
		data["Hours"] = hours
	} else if minutes, ok := count(slots["minutes"]); ok {
		data["Minutes"] = minutes
	}
	keys := append([]string{}, common...)
	keys = append(keys, outcomes[operation]...)
	if operation == Calendar {
		keys = append(keys, "calendar.empty_"+c.String("day"))
	}
	out := map[string]string{}
	for _, key := range keys {
		if key == "remind.set" && data["Hours"] == nil && data["Minutes"] == nil {
			continue
		}
		if text := phrase(key, data); text != "" {
			out[key] = text
		}
	}
	return out
}

func phrase(key string, data map[string]any) string {
	id := replyKey + key
	if key == "" || say.Missing(id) {
		return ""
	}
	return say.SpeakF(id, data)
}

func whole(v map[string]any) map[string]any {
	for k, x := range v {
		v[k] = number(x)
	}
	return v
}

func number(x any) any {
	switch t := x.(type) {
	case float64:
		if t == float64(int(t)) {
			return int(t)
		}
	case map[string]any:
		return whole(t)
	case []any:
		for i := range t {
			t[i] = number(t[i])
		}
	}
	return x
}
