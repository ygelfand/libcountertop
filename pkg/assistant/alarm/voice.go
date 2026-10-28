package alarm

import (
	"fmt"
	"strings"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/actions"
	"github.com/ygelfand/libcountertop/pkg/say"
)

func (a *Alarms) VoiceAction() actions.Action {
	trigger := func(sentence, operation string) actions.Trigger {
		return actions.Trigger{Sentence: sentence, Args: map[string]string{"operation": operation}}
	}
	return actions.Action{
		Name: "alarm", Title: say.T("actions.alarm"),
		Args: []esphome.Arg{
			{Name: "operation", Type: esphome.ArgString},
			{Name: "time", Type: esphome.ArgString},
			{Name: "days", Type: esphome.ArgString},
			{Name: "name", Type: esphome.ArgString},
			{Name: "target", Type: esphome.ArgString},
		},
		Triggers: []actions.Trigger{
			trigger("(set|create|add) [a|an] alarm (called|named) {name} (for|at) {time}", "set"),
			trigger("(set|create|add) [a|an] alarm (for|at) {time}", "set"),
			trigger("list [my] alarms", "list"),
			trigger("what are my alarms", "list"),
			trigger("when is [my|the] next alarm", "next"),
			trigger("(enable|turn on) [my|the] {target} alarm", "enable"),
			trigger("(disable|turn off) [my|the] {target} alarm", "disable"),
			trigger("(delete|remove|cancel) [my|the] {target} alarm", "delete"),
			trigger("(stop|dismiss) [my|the] alarm", "stop"),
			trigger("snooze [my|the] alarm", "snooze"),
			trigger("snooze", "snooze"),
		},
		Run: a.voice,
	}
}

func (a *Alarms) voice(c esphome.Call) (string, error) {
	switch operation := c.String("operation"); operation {
	case "set":
		hour, minute, days, err := spokenSchedule(c.String("time"), c.String("days"))
		if err != nil {
			return say.T("actions.alarm.time"), nil
		}
		name := strings.TrimSpace(c.String("name"))
		for _, al := range a.List() {
			if (name != "" && strings.EqualFold(al.Name, name)) ||
				(name == "" && al.Hour == hour && al.Minute == minute && al.Days.String() == days.String()) {
				return say.F("actions.alarm.exists", map[string]any{"Alarm": alarmLabel(al)}), nil
			}
		}
		al, err := a.Set(Alarm{
			Name: name, Hour: hour, Minute: minute, Days: days, On: true,
			Snooze: 5 * time.Minute, Volume: VolumeStatic,
		})
		if err != nil {
			return "", err
		}
		return say.F("actions.alarm.set", map[string]any{"Alarm": alarmLabel(al)}), nil
	case "list":
		list := a.List()
		if len(list) == 0 {
			return say.T("actions.alarm.empty"), nil
		}
		labels := make([]string, 0, len(list))
		for _, al := range list {
			label := alarmLabel(al)
			if !al.On {
				label = say.F("actions.alarm.off", map[string]any{"Alarm": label})
			}
			labels = append(labels, label)
		}
		return strings.Join(labels, ". "), nil
	case "next":
		al, at, ok := a.Upcoming()
		if !ok {
			return say.T("actions.alarm.none"), nil
		}
		return say.F(
			"actions.alarm.next",
			map[string]any{
				"Alarm": alarmLabel(al),
				"When":  at.Format("Monday, January 2 at 15:04"),
			},
		), nil
	case "enable", "disable", "delete":
		al, reply := a.voiceTarget(c.String("target"))
		if reply != "" {
			return reply, nil
		}
		var err error
		if operation == "delete" {
			err = a.Delete(al.ID)
		} else {
			err = a.Enable(al.ID, operation == "enable")
		}
		if err != nil {
			return "", err
		}
		return say.F("actions.alarm."+operation, map[string]any{"Alarm": alarmLabel(al)}), nil
	case "stop":
		if !a.Stop() {
			return say.T("actions.alarm.not_ringing"), nil
		}
		return say.T("actions.alarm.stopped"), nil
	case "snooze":
		if !a.Snooze() {
			return say.T("actions.alarm.not_snoozed"), nil
		}
		return say.T("actions.alarm.snoozed"), nil
	default:
		return "", fmt.Errorf("unknown alarm operation %q", operation)
	}
}

func (a *Alarms) voiceTarget(target string) (Alarm, string) {
	target = strings.TrimSpace(target)
	if strings.EqualFold(target, "next") {
		if al, _, ok := a.Upcoming(); ok {
			return al, ""
		}
		return Alarm{}, say.T("actions.alarm.none")
	}
	if target == "" {
		return Alarm{}, say.T("actions.alarm.target")
	}
	hour, minute, clockErr := spokenClock(target)
	var matches []Alarm
	for _, al := range a.List() {
		if al.ID == target || strings.EqualFold(al.Name, target) ||
			(clockErr == nil && al.Hour == hour && al.Minute == minute) {
			matches = append(matches, al)
		}
	}
	switch len(matches) {
	case 0:
		return Alarm{}, say.T("actions.alarm.missing")
	case 1:
		return matches[0], ""
	default:
		return Alarm{}, say.T("actions.alarm.ambiguous")
	}
}

func alarmLabel(al Alarm) string {
	label := al.Clock()
	if al.Name != "" {
		label = al.Name + " at " + label
	}
	if al.Once() {
		return label + ", " + say.T("actions.alarm.once")
	}
	return label + ", " + al.Days.String()
}
