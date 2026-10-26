package alerts

import (
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/audio/tone"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const (
	Level       = 0.6
	repeatEvery = 2 * time.Second
)

type (
	Setting = setting.Setting[schema.Alerts]
	Table   = setting.Table[schema.Alerts]
)

const Group setting.Group = "Alerts"

type Sounds interface {
	Sound(c, fallback schema.Chime) tone.Sound
	Choices() []setting.Option
}

type Alerts struct {
	*setting.Controls[schema.Alerts]
	sounds Sounds
	play   func(tone.Sound, float64)

	mu    sync.Mutex
	tones repeater
	rings repeater
}

type repeater struct {
	generation int
	timer      *time.Timer
}

func (r *repeater) stop() int {
	r.generation++
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	return r.generation
}

func New(
	at storage.Section[schema.Alerts],
	device uint32,
	sounds Sounds,
	play func(tone.Sound, float64),
) *Alerts {
	var more func() []setting.Option
	if sounds != nil {
		more = sounds.Choices
	}
	return &Alerts{
		Controls: &setting.Controls[schema.Alerts]{
			Table:  NewTable(more),
			Device: device,
			Read:   at.Read,
			Save: func(s Setting, v string) error {
				return setting.Store(at, s, v)
			},
		},
		sounds: sounds,
		play:   play,
	}
}

func (*Alerts) Name() string { return "alerts" }

func (a *Alerts) Current() schema.Alerts { return a.Read() }

func (a *Alerts) Start(t schema.Tone) {
	current := a.Read()
	snd := a.sound(current.Chime(t), schema.DefaultAlerts().Chime(t))
	a.mu.Lock()
	generation := a.tones.stop()
	a.mu.Unlock()
	if snd.Empty() {
		return
	}
	a.play(snd, Level)
	if t == schema.ToneAlert {
		a.again(&a.tones, generation, snd, until(time.Now().Add(current.RingFor.After())))
	}
}

func (a *Alerts) Chime(c schema.Chime, repeat time.Duration) {
	snd := a.sound(c, schema.ChimeNone)
	a.mu.Lock()
	generation := a.tones.stop()
	a.mu.Unlock()
	if snd.Empty() {
		return
	}
	a.play(snd, Level)
	if repeat > 0 {
		a.again(&a.tones, generation, snd, until(time.Now().Add(repeat)))
	}
}

func (a *Alerts) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tones.stop()
}

func (a *Alerts) Ring(c schema.Chime) {
	snd := a.sound(c, schema.ChimeNone)
	a.mu.Lock()
	generation := a.rings.stop()
	a.mu.Unlock()
	if snd.Empty() {
		return
	}
	a.play(snd, Level)
	a.again(&a.rings, generation, snd, func() bool { return true })
}

func (a *Alerts) Quiet() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rings.stop()
}

func (a *Alerts) Preview(c schema.Chime) {
	if snd := a.sound(c, schema.ChimeNone); !snd.Empty() {
		a.play(snd, Level)
	}
}

func (a *Alerts) RingTimer() {
	if snd := a.sound(a.Read().Timer, schema.DefaultAlerts().Timer); !snd.Empty() {
		a.play(snd, Level)
	}
}

func (a *Alerts) RingAlarm(c schema.Chime, share float64) {
	if snd := a.sound(c, schema.DefaultAlerts().Timer); !snd.Empty() {
		a.play(snd, Level*min(max(share, 0), 1))
	}
}

func (a *Alerts) RingFor() time.Duration { return a.Read().RingFor.After() }

func (a *Alerts) Tones() []setting.Option {
	if a.sounds == nil {
		return nil
	}
	return a.sounds.Choices()
}

func (a *Alerts) sound(c, fallback schema.Chime) tone.Sound {
	if a.sounds == nil {
		if _, custom := c.Custom(); custom {
			c = fallback
		}
		return tone.Sound{Notes: tone.Wake(c)}
	}
	return a.sounds.Sound(c, fallback)
}

func until(end time.Time) func() bool {
	return func() bool { return time.Now().Add(repeatEvery).Before(end) }
}

func (a *Alerts) again(r *repeater, generation int, snd tone.Sound, more func() bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if r.generation != generation || !more() {
		return
	}
	r.timer = time.AfterFunc(repeatEvery, func() {
		a.mu.Lock()
		live := r.generation == generation
		a.mu.Unlock()
		if !live {
			return
		}
		a.play(snd, Level)
		a.again(r, generation, snd, more)
	})
}

func NewTable(more func() []setting.Option) *Table {
	chime := func(name, icon string, field func(*schema.Alerts) *schema.Chime) Setting {
		return setting.Pick(
			Setting{Name: name, Group: Group, Icon: icon, More: more},
			schema.AlertChimes(),
			field,
		)
	}
	return setting.NewTable("alerts", []setting.Group{Group}, []Setting{
		chime(
			"info",
			"mdi:information-outline",
			func(a *schema.Alerts) *schema.Chime { return &a.Info },
		),
		chime(
			"success",
			"mdi:check-circle-outline",
			func(a *schema.Alerts) *schema.Chime { return &a.Success },
		),
		chime(
			"warning",
			"mdi:alert-outline",
			func(a *schema.Alerts) *schema.Chime { return &a.Warning },
		),
		chime(
			"alert",
			"mdi:alert-octagon-outline",
			func(a *schema.Alerts) *schema.Chime { return &a.Alert },
		),
		chime(
			"timer",
			"mdi:timer-music-outline",
			func(a *schema.Alerts) *schema.Chime { return &a.Timer },
		),
		setting.Pick(Setting{Name: "ring_for", Group: Group, Icon: "mdi:timer-cancel-outline"},
			schema.RingDelays(), func(a *schema.Alerts) *schema.Delay { return &a.RingFor }),
		setting.Pick(Setting{Name: "wake", Group: Group, Icon: "mdi:weather-sunset-up"},
			schema.WakeLooks(), func(a *schema.Alerts) *schema.WakeLook { return &a.Wake }),
	}, setting.Messages{Text: say.T, Missing: say.Missing})
}

// TimerTable contains only the controls needed by a device without messages.
func TimerTable(more func() []setting.Option) *Table {
	full := NewTable(more)
	var rows []Setting
	for _, row := range full.Rows() {
		if row.Name == "timer" || row.Name == "ring_for" {
			rows = append(rows, row)
		}
	}
	return setting.NewTable(
		"alerts",
		[]setting.Group{Group},
		rows,
		setting.Messages{Text: say.T, Missing: say.Missing},
	)
}
