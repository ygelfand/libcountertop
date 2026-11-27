package activity

import (
	"strconv"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/runtime/component"
)

type Log struct {
	opts  Options
	mu    sync.Mutex
	now   *Turn
	word  *esphome.TextSensor
	heard *esphome.TextSensor
	reply *esphome.TextSensor
}

type Options struct {
	Emit   func(map[string]string)
	Duck   func(map[string]string)
	DuckDB func() float64
}

func New(opts Options) *Log {
	return &Log{
		opts: opts,
		word: &esphome.TextSensor{
			Base: esphome.Base{
				ObjectID: "last_wake_word",
				Name:     "Last wake word",
				Icon:     "mdi:account-voice",
			},
		},
		heard: &esphome.TextSensor{
			Base: esphome.Base{ObjectID: "last_heard", Name: "Last heard", Icon: "mdi:ear-hearing"},
		},
		reply: &esphome.TextSensor{
			Base: esphome.Base{
				ObjectID: "last_reply",
				Name:     "Last reply",
				Icon:     "mdi:message-text",
			},
		},
	}
}

func (l *Log) Name() string { return "activity" }

func (l *Log) Entities() []esphome.Entity {
	return []esphome.Entity{l.word, l.heard, l.reply}
}

func (l *Log) Speaker() Speaker {
	l.mu.Lock()
	t := l.now
	l.mu.Unlock()
	return t.who()
}

func (l *Log) SpeakerWithin(wait time.Duration) Speaker {
	l.mu.Lock()
	t := l.now
	l.mu.Unlock()
	if s := t.who(); s.Match != Pending {
		return s
	}
	select {
	case <-t.settled:
	case <-time.After(wait):
	}
	return t.who()
}

func (l *Log) Woke(phrase string) {
	if phrase == "" {
		return
	}
	l.word.Set(component.Fit(phrase))
}

func (l *Log) Ducked(on bool) {
	if l.opts.Duck == nil {
		return
	}
	fields := map[string]string{"version": Version, "ducked": strconv.FormatBool(on)}
	if l.opts.DuckDB != nil {
		fields["duck_db"] = strconv.FormatFloat(l.opts.DuckDB(), 'f', -1, 64)
	}
	l.opts.Duck(fields)
}

func (l *Log) Heard(text string) {
	if text == "" {
		return
	}
	l.heard.Set(component.Fit(text))
}

func (l *Log) Replied(text string) {
	if text == "" {
		return
	}
	l.reply.Set(component.Fit(text))
}
