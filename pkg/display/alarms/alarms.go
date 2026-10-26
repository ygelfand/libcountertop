package alarms

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/ygelfand/libcountertop/pkg/assistant/alarm"
	"github.com/ygelfand/libcountertop/pkg/display/apps"
	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/settings"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

var week = []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday,
	time.Friday, time.Saturday, time.Sunday}

var (
	hours   = span{0, 23}
	minutes = span{0, 59}
	snoozes = span{0, int(alarm.MaxSnooze / time.Minute)}
	ramps   = span{int(alarm.MinRamp / time.Second), int(alarm.MaxRamp / time.Second)}
	lights  = span{0, int(alarm.MaxLight / time.Minute)}
)

type Options struct {
	Shell  *shell.Shell
	Alarms *alarm.Alarms
	Tones  func() []setting.Option
}

type Alarms struct{ o Options }

func New(o Options) *Alarms { return &Alarms{o: o} }

func (a *Alarms) App() apps.App {
	return apps.App{
		ID:   schema.AppAlarms,
		Name: func() string { return say.T("alarm.app") },
		Icon: "mdi:alarm",
		Open: func() { a.o.Shell.Push(a.List()) },
	}
}

func (a *Alarms) List() *shell.Page {
	return &shell.Page{Title: say.T("alarm.app"), Build: func() ([]menu.Row, []func(int)) {
		var rows []menu.Row
		var taps []func(int)
		for _, al := range a.o.Alarms.List() {
			state := onOff(al.On)
			if until, ok := a.o.Alarms.Snoozed(al.ID); ok {
				state = say.F("alarm.snoozed", map[string]any{"T": until.Format("15:04")})
			}
			rows = append(rows, menu.Row{
				Glyph: glyph(al),
				Label: al.Clock(),
				Hint:  strings.TrimSpace(al.Name + "  " + days(al.Days)),
				Kind:  menu.Chevron,
				Value: state,
			})
			taps = append(taps, func(int) { a.o.Shell.Push(a.Edit(al)) })
		}
		rows = append(rows, menu.Row{Glyph: "mdi:plus", Label: say.T("alarm.add")})
		taps = append(taps, func(int) { a.o.Shell.Push(a.Edit(fresh())) })
		return rows, taps
	}}
}

type draft struct{ alarm.Alarm }

func (a *Alarms) Edit(start alarm.Alarm) *shell.Page {
	d := &draft{start}
	saved := start.ID != ""
	page := &shell.Page{Title: say.T("alarm.edit")}
	page.Build = func() ([]menu.Row, []func(int)) {
		volumes := alarm.Volumes()
		labels := make([]string, len(volumes))
		for i, v := range volumes {
			labels[i] = say.T("alarm.volume." + string(v))
		}
		set := func(edit func(*alarm.Alarm)) {
			edit(&d.Alarm)
			a.o.Shell.Redraw()
		}

		rows := []menu.Row{
			{Label: say.T("alarm.name"), Kind: menu.Field, Value: d.Name, Save: func(s string) {
				set(func(al *alarm.Alarm) { al.Name = strings.TrimSpace(s) })
			}},
			{Label: say.T("alarm.on"), Kind: menu.Toggle, On: d.On},
			hours.row(say.T("alarm.hour"), d.Hour, fmt.Sprintf("%02d", d.Hour)),
			minutes.row(say.T("alarm.minute"), d.Minute, fmt.Sprintf(":%02d", d.Minute)),
			{Label: say.T("alarm.repeat"), Kind: menu.Chevron, Value: days(d.Days)},
			{Label: say.T("alarm.sound"), Kind: menu.Chevron, Value: a.toneOf(d.Sound).Label()},
			snoozes.row(say.T("alarm.snooze"), int(d.Snooze/time.Minute), minutesOrOff(d.Snooze)),
			{
				Label: say.T("alarm.volume"), Kind: menu.Segments,
				Options: labels, Level: max(slices.Index(volumes, d.Volume), 0),
			},
		}
		taps := []func(int){
			nil,
			func(int) { set(func(al *alarm.Alarm) { al.On = !al.On }) },
			func(level int) { set(func(al *alarm.Alarm) { al.Hour = hours.value(level) }) },
			func(level int) { set(func(al *alarm.Alarm) { al.Minute = minutes.value(level) }) },
			func(int) { a.o.Shell.Push(a.Days(d)) },
			func(int) { a.o.Shell.Push(a.Sound(d)) },
			func(level int) {
				set(
					func(al *alarm.Alarm) { al.Snooze = time.Duration(snoozes.value(level)) * time.Minute },
				)
			},
			func(i int) {
				set(func(al *alarm.Alarm) {
					al.Volume = volumes[i]
					if al.Volume == alarm.VolumeGentle && al.Ramp < alarm.MinRamp {
						al.Ramp = time.Minute
					}
				})
			},
		}
		if d.Volume == alarm.VolumeGentle {
			rows = append(
				rows,
				ramps.row(say.T("alarm.ramp"), int(d.Ramp/time.Second), seconds(d.Ramp)),
			)
			taps = append(taps, func(level int) {
				set(
					func(al *alarm.Alarm) { al.Ramp = time.Duration(ramps.value(level)) * time.Second },
				)
			})
		}
		rows = append(rows,
			lights.row(say.T("alarm.light"), int(d.Light/time.Minute), minutesOrOff(d.Light)),
		)
		taps = append(taps,
			func(level int) {
				set(
					func(al *alarm.Alarm) { al.Light = time.Duration(lights.value(level)) * time.Minute },
				)
			},
		)
		return rows, taps
	}
	if saved {
		page.Actions = append(page.Actions, shell.Action{
			Label: say.T("alarm.delete"), Glyph: "mdi:delete-outline", Role: shell.Danger,
			Do: func() {
				if err := a.o.Alarms.Delete(d.ID); err != nil {
					slog.Error("deleting an alarm failed", "err", err)
				}
				a.o.Shell.Pop()
			},
		})
	}
	page.Actions = append(page.Actions, shell.Action{
		Label: say.T("alarm.save"), Glyph: "mdi:content-save-outline",
		Do: func() {
			if _, err := a.o.Alarms.Set(d.Alarm); err != nil {
				slog.Error("saving an alarm failed", "err", err)
				return
			}
			a.o.Shell.Pop()
		},
	})
	return page
}

func (a *Alarms) Days(d *draft) *shell.Page {
	return &shell.Page{Title: say.T("alarm.repeat"), Build: func() ([]menu.Row, []func(int)) {
		rows := make([]menu.Row, 0, len(week))
		taps := make([]func(int), 0, len(week))
		for _, day := range week {
			rows = append(rows, menu.Row{
				Label: say.T("alarm.day." + strings.ToLower(day.String()[:3])),
				Kind:  menu.Toggle,
				On:    slices.Contains(d.Days, day),
			})
			taps = append(taps, func(int) {
				if slices.Contains(d.Days, day) {
					d.Days = slices.DeleteFunc(
						slices.Clone(d.Days),
						func(x time.Weekday) bool { return x == day },
					)
				} else {
					d.Days = append(slices.Clone(d.Days), day)
				}
				slices.Sort(d.Days)
				a.o.Shell.Redraw()
			})
		}
		return rows, taps
	}}
}

func (a *Alarms) Sound(d *draft) *shell.Page {
	return settings.Choose(
		say.T("alarm.sound"),
		a.tones(),
		func() tone { return a.toneOf(d.Sound) },
		func(t tone) { d.Sound = t.chime },
		nil,
	)
}

type span struct{ least, most int }

func (s span) steps() menu.Steps { return menu.Steps(s.most - s.least + 1) }

func (s span) row(label string, v int, shown string) menu.Row {
	steps := s.steps()
	return menu.Row{
		Label: label,
		Kind:  menu.Slider,
		Level: steps.Level(min(max(v, s.least), s.most) - s.least),
		Snap:  steps.Snap,
		Value: shown,
	}
}

func (s span) value(level int) int { return s.least + s.steps().Index(level) }

type tone struct {
	chime schema.Chime
	label string
}

func (t tone) Label() string { return t.label }

func (a *Alarms) tones() []tone {
	builtin := schema.AlertChimes()
	out := make([]tone, 0, len(builtin))
	for _, c := range builtin {
		if !c.Silent() {
			out = append(out, tone{chime: c, label: c.Label()})
		}
	}
	if a.o.Tones != nil {
		for _, o := range a.o.Tones() {
			out = append(out, tone{chime: schema.Chime(o.Value), label: o.Label})
		}
	}
	return out
}

func (a *Alarms) toneOf(c schema.Chime) tone {
	for _, t := range a.tones() {
		if t.chime == c {
			return t
		}
	}
	return tone{chime: c, label: c.Label()}
}

func fresh() alarm.Alarm {
	return alarm.Alarm{
		Hour:   7,
		On:     true,
		Sound:  schema.ChimeTimer,
		Snooze: 9 * time.Minute,
		Volume: alarm.VolumeGentle,
		Ramp:   time.Minute,
	}
}

func glyph(al alarm.Alarm) string {
	if al.On {
		return "mdi:alarm"
	}
	return "mdi:alarm-off"
}

func onOff(on bool) string {
	if on {
		return say.T("alarm.state.on")
	}
	return say.T("alarm.state.off")
}

func days(d alarm.Weekdays) string {
	switch {
	case len(d) == 0:
		return say.T("alarm.once")
	case len(d) == 7:
		return say.T("alarm.daily")
	case slices.Equal(d, alarm.Weekdays{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}):
		return say.T("alarm.weekdays")
	case slices.Equal(d, alarm.Weekdays{time.Sunday, time.Saturday}):
		return say.T("alarm.weekends")
	}
	names := make([]string, 0, len(d))
	for _, day := range week {
		if slices.Contains(d, day) {
			names = append(names, say.T("alarm.day."+strings.ToLower(day.String()[:3])))
		}
	}
	return strings.Join(names, ", ")
}

func minutesOrOff(d time.Duration) string {
	if d <= 0 {
		return say.T("alarm.off")
	}
	return say.F("alarm.minutes", map[string]any{"N": int(d / time.Minute)})
}

func seconds(d time.Duration) string {
	return say.F("alarm.seconds", map[string]any{"N": int(d / time.Second)})
}
