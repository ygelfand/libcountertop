package desktop

import (
	"context"
	"encoding/json"
	"math"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/input/textentry"
	"github.com/ygelfand/libcountertop/pkg/input/touch"
)

func (s *session) receive(raw []byte) error {
	if len(raw) > textentry.TextMost+256 {
		return textentry.ErrInvalid
	}
	var event struct {
		Phase string `json:"phase"`
		Text  string `json:"text"`
		Key   string `json:"key"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return textentry.ErrInvalid
	}
	if event.Phase != "text" && event.Phase != "key" {
		s.input.receive(raw)
		return nil
	}
	if s.server.keyboard == nil {
		return textentry.ErrUnavailable
	}
	action := "type"
	if event.Phase == "key" {
		action = "key"
	}
	ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
	defer cancel()
	_, err := s.server.keyboard.Call(
		ctx,
		textentry.Request{Action: action, Text: event.Text, Key: event.Key},
	)
	if err == nil {
		s.input.mu.Lock()
		s.input.last = time.Now()
		s.input.mu.Unlock()
	}
	return err
}

type pointer struct {
	mu            sync.Mutex
	deliver       func(touch.Contact)
	width, height int
	contact       *touch.Contact
	last          time.Time
}

func (p *pointer) receive(raw []byte) {
	if len(raw) > 256 {
		return
	}
	var event struct {
		Phase string   `json:"phase"`
		X     *float64 `json:"x"`
		Y     *float64 `json:"y"`
	}
	if json.Unmarshal(raw, &event) != nil || event.X == nil || event.Y == nil {
		return
	}
	x, y := *event.X, *event.Y
	if math.IsNaN(x) || math.IsNaN(y) || x < 0 || x > 1 || y < 0 || y > 1 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.deliver == nil {
		return
	}
	var phase touch.Phase
	switch event.Phase {
	case "down":
		if p.contact != nil {
			return
		}
		phase = touch.Down
	case "move", "up":
		if p.contact == nil {
			return
		}
		phase = touch.Move
		if event.Phase == "up" {
			phase = touch.Up
		}
	default:
		return
	}
	c := touch.Contact{ID: -1 << 30, Phase: phase, At: time.Now(),
		X: min(int(x*float64(p.width)), p.width-1),
		Y: min(int(y*float64(p.height)), p.height-1)}
	p.contact = &c
	p.last = c.At
	p.deliver(c)
	if phase == touch.Up {
		p.contact = nil
	}
}

func (p *pointer) idle(now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return now.Sub(p.last) >= idleTimeout
}

func (p *pointer) release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.contact != nil {
		c := *p.contact
		c.Phase, c.At = touch.Up, time.Now()
		p.deliver(c)
		p.contact = nil
	}
	p.deliver = nil
}
