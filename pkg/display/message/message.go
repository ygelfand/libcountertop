package message

import (
	"context"
	"fmt"
	"image"
	"log/slog"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/settings/schema"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/hook"
)

const (
	DefaultSeconds = 15
	MaxSeconds     = 3600
)

type Messages struct {
	Changed hook.Hook[bool]

	mu         sync.Mutex
	up         bool
	timer      *time.Timer
	current    Message
	generation uint64
	took       *int
	catch      image.Rectangle
	swipe      swipe
}

func (m *Messages) Took(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.took != nil && *m.took == id
}

func (m *Messages) Current() (Message, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current, m.up
}

func New() *Messages                              { return &Messages{} }
func (m *Messages) Run(ctx context.Context) error { <-ctx.Done(); m.Close(); return nil }
func (m *Messages) Close()                        { m.Hide() }

func (m *Messages) Catch(area image.Rectangle) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.catch = area
}

func (m *Messages) Dismiss(id int, at image.Point) {
	m.mu.Lock()
	showing := m.up && len(m.current.Keys) == 0 && (m.catch.Empty() || at.In(m.catch))
	m.took = nil
	if showing {
		m.took = &id
	}
	m.mu.Unlock()

	if !showing {
		return
	}
	slog.Info("message dismissed")
	m.end(End{Why: EndTapped})
}

func (m *Messages) Discard() {
	m.mu.Lock()
	up := m.up
	m.mu.Unlock()
	if up {
		m.end(End{Why: EndClosed})
	}
}

func (m *Messages) Press(key int) {
	m.mu.Lock()
	ok := m.up && key >= 0 && key < len(m.current.Keys)
	m.mu.Unlock()
	if ok {
		m.end(End{Key: key, Why: EndKey})
	}
}

func (m *Messages) Name() string { return "messages" }

func (m *Messages) Actions() []*esphome.Action {
	return []*esphome.Action{
		{
			Name: "show_message",
			Args: []esphome.Arg{
				{Name: "body", Type: esphome.ArgString},
				{Name: "title", Type: esphome.ArgString},
				{Name: "tone", Type: esphome.ArgString},
				{Name: "seconds", Type: esphome.ArgInt},
			},
			Run: func(c esphome.Call) (any, error) {
				body := c.String("body")
				if body == "" {
					return nil, fmt.Errorf("show_message needs a body")
				}

				m.Show(Message{
					Title: c.String("title"),
					Body:  body,
					Tone:  tone(c.String("tone")),
				}, Hold(c.Int("seconds")))
				return nil, nil
			},
		},
		{
			Name: "clear_message",
			Run:  func(esphome.Call) (any, error) { m.Hide(); return nil, nil },
		},
	}
}

func (m *Messages) Show(msg Message, hold time.Duration) {
	m.mu.Lock()
	old, was := m.current, m.up
	m.current, m.up = msg, true
	m.catch = image.Rectangle{}
	m.generation++
	generation := m.generation
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	if hold > 0 {
		m.timer = time.AfterFunc(hold, func() { m.expire(generation) })
	}
	m.mu.Unlock()
	if was && old.Ended != nil {
		old.Ended(End{Why: EndReplaced})
	}
	slog.Info("message", "title", msg.Title, "tone", msg.Tone, "for", hold)
	m.Changed.Emit(true)
}

type Sounder interface {
	Start(schema.Tone)
	Chime(c schema.Chime, repeat time.Duration)
	Stop()
}

func (m *Messages) SoundWith(s Sounder) {
	m.Changed.Listen(func(up bool) {
		current, ok := m.Current()
		switch {
		case !up || !ok:
			s.Stop()
		case current.Chime != "":
			s.Chime(current.Chime, current.Repeat)
		default:
			s.Start(current.Tone)
		}
	})
}

func (m *Messages) Info(title, body string, hold time.Duration) {
	m.Show(Message{Title: title, Body: body, Tone: schema.ToneInfo}, hold)
}

func (m *Messages) Hide() { m.end(End{Why: EndCleared}) }

func (m *Messages) end(why End) {
	m.mu.Lock()
	if !m.up {
		m.mu.Unlock()
		return
	}
	m.endLocked(why)
}

func (m *Messages) endLocked(why End) {
	ended := m.current.Ended
	m.up = false
	m.generation++
	m.swipe = swipe{}
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.mu.Unlock()
	if ended != nil {
		ended(why)
	}
	m.Changed.Emit(false)
}

func tone(name string) schema.Tone {
	for _, t := range schema.Tones() {
		if string(t) == name {
			return t
		}
	}
	return schema.ToneInfo
}

func Hold(v int) time.Duration {
	switch {
	case v <= 0:
		v = DefaultSeconds
	case v > MaxSeconds:
		v = MaxSeconds
	}
	return time.Duration(v) * time.Second
}

func (m *Messages) expire(generation uint64) {
	m.mu.Lock()
	if !m.up || m.generation != generation {
		m.mu.Unlock()
		return
	}
	m.timer = nil
	m.endLocked(End{Why: EndTimeout})
}
