package timer

import (
	"cmp"
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"
	"github.com/ygelfand/go-esphome-device/api"

	"github.com/ygelfand/libcountertop/pkg/hook"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
)

const (
	refresh = 250 * time.Millisecond

	ringFor   = 15 * time.Minute
	ringEvery = 2 * time.Second
)

type Countdown interface {
	Show(left, total time.Duration)
	Clear()
}

type Options struct {
	Alarm   func() (release func())
	Chime   func()
	RingFor func() time.Duration

	Countdown Countdown
}

type Timers struct {
	Changed hook.Hook[Card]

	opts  Options
	names *esphome.TextSensor

	woke chan struct{}

	mu    sync.Mutex
	held  map[string]*timer
	local map[string]*time.Timer
	// done holds finished timers until the ring stops, so the Split style can show them.
	done map[string]*timer
	stop context.CancelFunc
	said string
}

type timer struct {
	first  time.Time
	name   string
	total  time.Duration
	left   time.Duration
	at     time.Time
	active bool
}

func (t *timer) remaining(now time.Time) time.Duration {
	if !t.active {
		return t.left
	}
	return max(t.left-now.Sub(t.at), 0)
}

func New(opts Options) *Timers {
	return &Timers{
		opts: opts,
		names: &esphome.TextSensor{
			Base: esphome.Base{
				ObjectID: "timers",
				Name:     "Timers",
				Icon:     "mdi:timer-outline",
				Category: esphome.CategoryDiagnostic,
			},
		},
		woke: make(chan struct{}, 1),
		held: map[string]*timer{},
		done: map[string]*timer{},
	}
}

func (t *Timers) Name() string { return "timers" }

func (t *Timers) Entities() []esphome.Entity { return []esphome.Entity{t.names} }

func (t *Timers) Run(ctx context.Context) error {
	for {
		if !t.counting() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.woke:
			}
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(refresh):
		}
		t.show()
	}
}

func (t *Timers) counting() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.soonest(time.Now()) != nil
}

func (t *Timers) Event(e esphome.TimerEvent) {
	slog.Debug(
		"timer",
		"event",
		e.Type,
		"name",
		e.Name,
		"left",
		e.SecondsLeft,
		"total",
		e.TotalSeconds,
		"active",
		e.IsActive,
	)

	switch e.Type {
	case api.VoiceAssistantTimerEvent_VOICE_ASSISTANT_TIMER_STARTED,
		api.VoiceAssistantTimerEvent_VOICE_ASSISTANT_TIMER_UPDATED:
		t.set(e)
	case api.VoiceAssistantTimerEvent_VOICE_ASSISTANT_TIMER_CANCELLED:
		t.forget(e.TimerID)
	case api.VoiceAssistantTimerEvent_VOICE_ASSISTANT_TIMER_FINISHED:
		t.finished(e)
	}
	t.show()
	t.publish()
}

func (t *Timers) Count(id, name string, d time.Duration) {
	seconds := uint32(max(d, time.Second) / time.Second)
	t.mu.Lock()
	if t.local == nil {
		t.local = map[string]*time.Timer{}
	}
	if old := t.local[id]; old != nil {
		old.Stop()
	}
	t.local[id] = time.AfterFunc(time.Duration(seconds)*time.Second, func() {
		t.mu.Lock()
		delete(t.local, id)
		t.mu.Unlock()
		t.Event(esphome.TimerEvent{
			Type:    api.VoiceAssistantTimerEvent_VOICE_ASSISTANT_TIMER_FINISHED,
			TimerID: id,
		})
	})
	t.mu.Unlock()
	t.Event(esphome.TimerEvent{
		Type:         api.VoiceAssistantTimerEvent_VOICE_ASSISTANT_TIMER_STARTED,
		TimerID:      id,
		Name:         name,
		TotalSeconds: seconds,
		SecondsLeft:  seconds,
		IsActive:     true,
	})
}

func (t *Timers) publish() {
	t.mu.Lock()
	now := time.Now()
	running := make([]*timer, 0, len(t.held))
	for _, c := range t.held {
		if c.active {
			running = append(running, c)
		}
	}
	t.mu.Unlock()

	slices.SortFunc(
		running,
		func(a, b *timer) int { return cmp.Compare(a.remaining(now), b.remaining(now)) },
	)

	names := make([]string, 0, len(running))
	for _, c := range running {
		names = append(names, cmp.Or(c.name, "Timer"))
	}
	t.names.Set(strings.Join(names, ", "))
}

func (t *Timers) Forget() {
	t.mu.Lock()
	n := len(t.held)
	t.held = map[string]*timer{}
	t.done = map[string]*timer{}
	t.mu.Unlock()

	if n > 0 {
		slog.Info("timers forgotten", "count", n)
	}
	t.show()
	t.publish()
}

func (t *Timers) Ringing() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stop != nil
}

func (t *Timers) Stop() bool {
	t.mu.Lock()
	stop := t.stop
	t.mu.Unlock()

	if stop == nil {
		return false
	}
	stop()
	return true
}

func (t *Timers) set(e esphome.TimerEvent) {
	t.mu.Lock()
	first := time.Now()
	if old, ok := t.held[e.TimerID]; ok {
		first = old.first
	}
	t.held[e.TimerID] = &timer{
		first:  first,
		name:   e.Name,
		total:  time.Duration(e.TotalSeconds) * time.Second,
		left:   time.Duration(e.SecondsLeft) * time.Second,
		at:     time.Now(),
		active: e.IsActive,
	}
	t.mu.Unlock()

	select {
	case t.woke <- struct{}{}:
	default:
	}
}

func (t *Timers) forget(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.held, id)
}

func (t *Timers) finished(e esphome.TimerEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	done := &timer{first: time.Now(), name: e.Name, total: time.Duration(e.TotalSeconds) * time.Second}
	if old, ok := t.held[e.TimerID]; ok {
		done.first, done.name, done.total = old.first, old.name, old.total
	}
	delete(t.held, e.TimerID)
	t.done[e.TimerID] = done
	t.start(t.opts)
}

func (t *Timers) RingSilently() {
	t.mu.Lock()
	t.start(Options{})
	t.mu.Unlock()
	t.show()
}

func (t *Timers) start(sound Options) {
	if t.stop != nil {
		return
	}

	var ctx context.Context
	ctx, t.stop = context.WithCancel(context.Background())
	safe.Go("timer alarm", func() { t.ring(ctx, sound) })
}

func (t *Timers) ring(ctx context.Context, sound Options) {
	defer func() {
		t.mu.Lock()
		t.stop = nil
		t.done = map[string]*timer{}
		t.mu.Unlock()
		t.show()
	}()

	if sound.Alarm != nil {
		defer sound.Alarm()()
	}

	length := ringFor
	if t.opts.RingFor != nil && t.opts.RingFor() > 0 {
		length = t.opts.RingFor()
	}
	over := time.After(length)
	for {
		if sound.Chime != nil {
			sound.Chime()
		}

		select {
		case <-ctx.Done():
			return
		case <-over:
			slog.Info("timer rang out", "for", length)
			return
		case <-time.After(ringEvery):
		}
	}
}

func (t *Timers) Now() Card {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.now()
}

func (t *Timers) now() Card {
	now := time.Now()
	card := Card{Ringing: t.stop != nil}
	if soon := t.soonest(now); soon != nil {
		card.Name, card.Left, card.Of = soon.name, soon.remaining(now), soon.total
		card.Showing = true
	}

	card.Showing = card.Showing || card.Ringing
	card.All = t.all(now)
	return card
}

// all lists held and finished timers oldest first.
func (t *Timers) all(now time.Time) []Entry {
	type item struct {
		id   string
		c    *timer
		done bool
	}
	items := make([]item, 0, len(t.held)+len(t.done))
	for id, c := range t.held {
		items = append(items, item{id, c, false})
	}
	for id, c := range t.done {
		items = append(items, item{id, c, true})
	}
	slices.SortFunc(items, func(a, b item) int {
		if c := a.c.first.Compare(b.c.first); c != 0 {
			return c
		}
		return cmp.Compare(a.id, b.id)
	})
	out := make([]Entry, 0, len(items))
	for _, it := range items {
		e := Entry{ID: it.id, Name: cmp.Or(it.c.name, "Timer"), Of: it.c.total, Done: it.done}
		if !it.done {
			e.Left, e.Paused = it.c.remaining(now), !it.c.active
		}
		out = append(out, e)
	}
	return out
}

func (t *Timers) show() {
	t.mu.Lock()
	card := t.now()
	key := card.key()
	changed := key != t.said
	t.said = key
	t.mu.Unlock()
	if changed {
		t.Changed.Emit(card)
	}

	if t.opts.Countdown == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	if soon := t.soonest(now); soon != nil {
		t.opts.Countdown.Show(soon.remaining(now), soon.total)
		return
	}
	t.opts.Countdown.Clear()
}

func (t *Timers) soonest(now time.Time) *timer {
	var soon *timer
	for _, c := range t.held {
		if !c.active {
			continue
		}
		if soon == nil || c.remaining(now) < soon.remaining(now) {
			soon = c
		}
	}
	return soon
}
