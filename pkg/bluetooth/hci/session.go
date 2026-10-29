package hci

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const deep = 256

type line interface {
	next(time.Duration) (packet, error)

	Send([]byte) (int, error)
	Wake() error
	Asleep() bool
}

type Session struct {
	p line

	wmu sync.Mutex

	mu sync.Mutex

	answers map[uint16]chan event

	takers []*taker

	dropped uint64

	acl    wire
	le     leSide
	scan   scanState
	quirks Quirks

	err  error
	done chan struct{}
}

type taker struct {
	wants func(packet) bool
	ch    chan packet
}

func Reader(ctx context.Context, p *Port) *Session {
	s := newSession(p)
	go s.serve(ctx)
	return s
}

func newSession(p line) *Session {
	return &Session{
		p:       p,
		answers: map[uint16]chan event{},
		done:    make(chan struct{}),
	}
}

func (s *Session) serve(ctx context.Context) {
	defer close(s.done)

	for ctx.Err() == nil {
		got, err := s.p.next(awake)
		switch {
		case errors.Is(err, errQuiet):
			continue
		case err != nil:
			s.fail(fmt.Errorf("ble: reading the line: %w", err))
			return
		}

		s.route(got)
	}
	s.fail(ctx.Err())
}

func (s *Session) route(got packet) {
	if got.kind == typeEvent {
		if got.event.Code == eventCompletedPackets {
			s.completed(got.event)
			return
		}
		if op, ok := addressed(got.event); ok {
			s.mu.Lock()
			waiter := s.answers[op]
			delete(s.answers, op)
			s.mu.Unlock()

			if waiter != nil {
				waiter <- got.event
				return
			}
		}
		if after := s.track(got.event); after != nil {
			defer after()
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, t := range s.takers {
		if !t.wants(got) {
			continue
		}
		select {
		case t.ch <- got:
		default:
			if s.dropped++; s.dropped%100 == 1 {
				slog.Warn("bluetooth packets dropped, a reader is not keeping up",
					"times", s.dropped, "waiting", deep)
			}
		}
	}
}

func addressed(e event) (uint16, bool) {
	switch {
	case e.Code == eventCommandComplete && len(e.Params) >= 3:
		return binary.LittleEndian.Uint16(e.Params[1:]), true
	case e.Code == eventCommandStatus && len(e.Params) >= 4:
		return binary.LittleEndian.Uint16(e.Params[2:]), true
	}
	return 0, false
}

func (s *Session) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.err == nil {
		s.err = err
	}
	for op, waiter := range s.answers {
		close(waiter)
		delete(s.answers, op)
	}
	for _, t := range s.takers {
		close(t.ch)
	}
	s.takers = nil
}

func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *Session) Write(b []byte) (int, error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()

	if err := s.rouse(); err != nil {
		return 0, err
	}
	return s.p.Send(b)
}

const noticed = 2 * time.Millisecond

func (s *Session) rouse() error {
	if !s.p.Asleep() {
		return nil
	}

	deadline := time.Now().Add(answers)
	var asked time.Time

	for s.p.Asleep() {
		if !time.Now().Before(deadline) {
			return fmt.Errorf("ble: the chip did not wake in %v", answers)
		}
		if time.Since(asked) >= wakeAgain {
			if err := s.p.Wake(); err != nil {
				return fmt.Errorf("ble: waking the chip: %w", err)
			}
			asked = time.Now()
		}
		time.Sleep(noticed)
	}
	return nil
}

func (s *Session) Ask(cmd []byte, within time.Duration) (event, error) {
	if len(cmd) < 3 {
		return event{}, fmt.Errorf("ble: %d bytes is not a command", len(cmd))
	}
	op := binary.LittleEndian.Uint16(cmd[1:])

	back := make(chan event, 1)

	s.mu.Lock()
	if s.err != nil {
		err := s.err
		s.mu.Unlock()
		return event{}, err
	}
	if _, already := s.answers[op]; already {
		s.mu.Unlock()
		return event{}, fmt.Errorf("ble: %#04x is already in flight", op)
	}
	s.answers[op] = back
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.answers, op)
		s.mu.Unlock()
	}()

	if _, err := s.Write(cmd); err != nil {
		return event{}, fmt.Errorf("ble: writing %#04x: %w", op, err)
	}

	select {
	case e, ok := <-back:
		if !ok {
			return event{}, fmt.Errorf("ble: the line stopped while %#04x was in flight", op)
		}
		return e, nil
	case <-time.After(within):
		return event{}, fmt.Errorf("%w: %#04x was not answered in %v", errQuiet, op, within)
	}
}

func (s *Session) Command(opcode uint16, params ...byte) ([]byte, error) {
	cmd, err := command(opcode, params...)
	if err != nil {
		return nil, err
	}

	e, err := s.Ask(cmd, answers)
	if err != nil {
		return nil, err
	}
	return complete(e, opcode)
}

func (s *Session) Take(wants func(packet) bool) (<-chan packet, func()) {
	t := &taker{wants: wants, ch: make(chan packet, deep)}

	s.mu.Lock()
	s.takers = append(s.takers, t)
	s.mu.Unlock()

	var once sync.Once
	return t.ch, func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()

			for i, have := range s.takers {
				if have == t {
					s.takers = append(s.takers[:i], s.takers[i+1:]...)
					close(t.ch)
					return
				}
			}
		})
	}
}

func (s *Session) Events() (<-chan packet, func()) {
	return s.Take(func(g packet) bool {
		return g.kind != typeEvent || g.event.Code != eventLEMeta
	})
}

func (s *Session) Sniff(ctx context.Context, saw func(string)) {
	got, done := s.Take(func(packet) bool { return true })
	defer done()

	for {
		select {
		case g, ok := <-got:
			if !ok {
				return
			}
			if g.kind == typeACL {
				saw(fmt.Sprintf("data handle %#04x  % x", g.acl.Handle, g.acl.Data))
				continue
			}
			saw(fmt.Sprintf("event %#02x  % x", g.event.Code, g.event.Params))
		case <-ctx.Done():
			return
		}
	}
}

func (s *Session) Reports() (<-chan packet, func()) {
	return s.Take(func(g packet) bool {
		return g.kind == typeEvent && g.event.Code == eventLEMeta
	})
}

func (s *Session) Wait() { <-s.done }
