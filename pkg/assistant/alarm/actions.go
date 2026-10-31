package alarm

import (
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func (a *Alarms) Actions() []*esphome.Action {
	return []*esphome.Action{
		{
			Name:        "alarm_list",
			Description: "List the device's alarms.",
			Answers:     true,
			Run: func(esphome.Call) (any, error) {
				return map[string]any{"alarms": a.List()}, nil
			},
		},
		{
			Name:        "alarm_set",
			Description: "Create an alarm, or replace the one with this id.",
			Answers:     true,
			Args: []esphome.Arg{
				{
					Name:        "id",
					Type:        esphome.ArgString,
					Description: "The alarm to replace; empty creates a new one.",
				},
				{Name: "name", Type: esphome.ArgString, Example: "Wake up"},
				{
					Name:        "time",
					Type:        esphome.ArgString,
					Description: "24-hour time.",
					Example:     "06:30",
				},
				{
					Name:        "days",
					Type:        esphome.ArgString,
					Description: "Weekdays it repeats on; empty rings once.",
					Example:     "mon,tue,wed,thu,fri",
				},
				{Name: "enabled", Type: esphome.ArgBool},
				{
					Name:        "sound",
					Type:        esphome.ArgString,
					Description: "A chime or custom tone; empty uses the default.",
				},
				{
					Name:        "snooze_minutes",
					Type:        esphome.ArgInt,
					Description: "Snooze length; 0 turns snooze off.",
					Example:     "9",
				},
				{
					Name:        "volume",
					Type:        esphome.ArgString,
					Description: "static, gentle (rises over ramp_seconds) or silent.",
					Example:     "gentle",
				},
				{
					Name:        "ramp_seconds",
					Type:        esphome.ArgInt,
					Description: "How long a gentle alarm takes to reach full volume.",
					Example:     "60",
				},
				{
					Name:        "light_minutes",
					Type:        esphome.ArgInt,
					Description: "Wake light before it rings; 0 for none.",
					Example:     "10",
				},
			},
			Run: func(c esphome.Call) (any, error) {
				al, err := fromCall(c)
				if err != nil {
					return nil, err
				}
				saved, err := a.Set(al)
				if err != nil {
					return nil, err
				}
				return map[string]any{"alarm": saved}, nil
			},
		},
		{
			Name:        "alarm_enable",
			Description: "Turn an alarm on or off without deleting it.",
			Args: []esphome.Arg{
				{Name: "id", Type: esphome.ArgString},
				{Name: "enabled", Type: esphome.ArgBool},
			},
			Run: func(c esphome.Call) (any, error) {
				return nil, a.Enable(c.String("id"), c.Bool("enabled"))
			},
		},
		{
			Name:        "alarm_delete",
			Description: "Delete an alarm.",
			Args:        []esphome.Arg{{Name: "id", Type: esphome.ArgString}},
			Run: func(c esphome.Call) (any, error) {
				return nil, a.Delete(c.String("id"))
			},
		},
		{
			Name:        "alarm_stop",
			Description: "Stop the alarm that is ringing or waking, keeping its schedule.",
			Run:         func(esphome.Call) (any, error) { a.Stop(); return nil, nil },
		},
		{
			Name:        "alarm_snooze",
			Description: "Snooze the alarm that is ringing.",
			Run:         func(esphome.Call) (any, error) { a.Snooze(); return nil, nil },
		},
	}
}

func fromCall(c esphome.Call) (Alarm, error) {
	hour, minute, err := ParseClock(c.String("time"))
	if err != nil {
		return Alarm{}, err
	}
	days, err := ParseWeekdays(c.String("days"))
	if err != nil {
		return Alarm{}, err
	}
	volume := Volume(c.String("volume"))
	if volume == "" {
		volume = VolumeStatic
	}
	return Alarm{
		ID:     c.String("id"),
		Name:   c.String("name"),
		Hour:   hour,
		Minute: minute,
		Days:   days,
		On:     c.Bool("enabled"),
		Sound:  schema.Chime(c.String("sound")),
		Snooze: time.Duration(c.Int("snooze_minutes")) * time.Minute,
		Volume: volume,
		Ramp:   time.Duration(c.Int("ramp_seconds")) * time.Second,
		Light:  time.Duration(c.Int("light_minutes")) * time.Minute,
	}, nil
}
