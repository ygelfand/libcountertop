package message

import (
	"image"
	"log/slog"
	"time"
)

const (
	swipeShare = 4
	swipeFor   = 180 * time.Millisecond
)

type swipe struct {
	id      int
	held    bool
	from    image.Point
	dx      int
	settle  time.Time
	start   int
	landing int
}

func (m *Messages) Grab(id int, at image.Point) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.up || !m.swipe.settle.IsZero() || (!m.catch.Empty() && !at.In(m.catch)) {
		return
	}
	m.swipe = swipe{id: id, held: true, from: at}
}

func (m *Messages) Drag(id int, at image.Point) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.swipe.held && m.swipe.id == id {
		m.swipe.dx = at.X - m.swipe.from.X
	}
}

func (m *Messages) Release(id int, at image.Point) {
	m.mu.Lock()
	if !m.swipe.held || m.swipe.id != id {
		m.mu.Unlock()
		return
	}
	dx := at.X - m.swipe.from.X
	reach := max(m.catch.Dx()/swipeShare, 1)
	gone := dx >= reach || -dx >= reach
	landing := 0
	if gone {
		landing = m.catch.Dx() * 2
		if dx < 0 {
			landing = -landing
		}
	}
	m.swipe = swipe{settle: time.Now(), start: dx, landing: landing}
	generation := m.generation
	m.mu.Unlock()
	time.AfterFunc(swipeFor, func() { m.settled(generation, gone) })
}

func (m *Messages) Offset() (int, bool) {
	m.mu.Lock()
	s := m.swipe
	m.mu.Unlock()
	if s.settle.IsZero() {
		return s.dx, s.held
	}
	p := min(time.Since(s.settle).Seconds()/swipeFor.Seconds(), 1)
	eased := 1 - (1-p)*(1-p)
	return s.start + int(float64(s.landing-s.start)*eased), p < 1
}

func (m *Messages) settled(generation uint64, gone bool) {
	m.mu.Lock()
	if m.generation != generation || !m.up {
		m.mu.Unlock()
		return
	}
	if !gone {
		m.swipe = swipe{}
		m.mu.Unlock()
		m.Changed.Emit(true)
		return
	}
	slog.Info("message swiped away")
	m.endLocked(End{Why: EndSwiped})
}
