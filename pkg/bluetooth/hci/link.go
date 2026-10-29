package hci

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"sync"

	"github.com/ygelfand/libcountertop/pkg/bluetooth/pair"
)

const (
	hciLEReadBufferSize = 0x2002

	eventEncryptionChange  = 0x08
	eventKeyRefreshDone    = 0x30
	leConnectionComplete   = 0x01
	leEnhancedConnComplete = 0x0a
)

type wire struct {
	mu      sync.Mutex
	classic *credit
	le      *credit
	isLE    map[Handle]bool
	held    map[Handle]int
	sending map[Handle]*sync.Mutex
}

func (s *Session) pools() (classic, le *credit, err error) {
	s.acl.mu.Lock()
	classic, le = s.acl.classic, s.acl.le
	s.acl.mu.Unlock()
	if classic != nil {
		return classic, le, nil
	}

	size, total, err := buffers(s)
	if err != nil {
		return nil, nil, fmt.Errorf("ble: asking what the controller holds: %w", err)
	}
	if classic, err = newCredit(size, total); err != nil {
		return nil, nil, err
	}
	le = classic
	if said, err := s.Command(hciLEReadBufferSize); err == nil && len(said) >= 3 {
		if n, count := int(binary.LittleEndian.Uint16(said)), int(said[2]); n > 0 && count > 0 {
			le, _ = newCredit(n, count)
		}
	}
	slog.Debug("bluetooth buffers", "packet", classic.size, "buffers", classic.total,
		"le packet", le.size, "le buffers", le.total, "shared", le == classic)

	s.acl.mu.Lock()
	defer s.acl.mu.Unlock()
	if s.acl.classic == nil {
		s.acl.classic, s.acl.le = classic, le
	}
	return s.acl.classic, s.acl.le, nil
}

func (s *Session) Send(ctx context.Context, h Handle, pdu []byte) error {
	classic, le, err := s.pools()
	if err != nil {
		return err
	}
	pool := classic
	if s.onLE(h) {
		pool = le
	}
	frames, err := aclFrames(h, pdu, pool.size)
	if err != nil {
		return err
	}

	order := s.sender(h)
	order.Lock()
	defer order.Unlock()

	for _, f := range frames {
		if err := pool.take(ctx); err != nil {
			return err
		}
		s.hold(h, 1)
		if _, err := s.Write(f); err != nil {
			s.hold(h, -1)
			_ = pool.give(1)
			return err
		}
	}
	return nil
}

func (s *Session) onLE(h Handle) bool {
	s.acl.mu.Lock()
	defer s.acl.mu.Unlock()
	return s.acl.isLE[h]
}

func (s *Session) sender(h Handle) *sync.Mutex {
	s.acl.mu.Lock()
	defer s.acl.mu.Unlock()
	if s.acl.sending == nil {
		s.acl.sending = map[Handle]*sync.Mutex{}
	}
	m := s.acl.sending[h]
	if m == nil {
		m = &sync.Mutex{}
		s.acl.sending[h] = m
	}
	return m
}

func (s *Session) hold(h Handle, n int) {
	s.acl.mu.Lock()
	defer s.acl.mu.Unlock()
	if s.acl.held == nil {
		s.acl.held = map[Handle]int{}
	}
	s.acl.held[h] += n
}

func (s *Session) completed(e event) {
	done, err := parseCompletedPackets(e)
	if err != nil {
		slog.Warn("bluetooth completed packets", "err", err)
		return
	}
	s.acl.mu.Lock()
	defer s.acl.mu.Unlock()
	for _, d := range done {
		pool := s.acl.classic
		if s.acl.isLE[d.Handle] {
			pool = s.acl.le
		}
		if pool == nil {
			continue
		}
		s.acl.held[d.Handle] -= d.Count
		if err := pool.give(d.Count); err != nil {
			slog.Warn("bluetooth completed packets", "handle", d.Handle, "err", err)
		}
	}
}

func (s *Session) joined(h Handle) {
	s.acl.mu.Lock()
	defer s.acl.mu.Unlock()
	if s.acl.isLE == nil {
		s.acl.isLE = map[Handle]bool{}
	}
	s.acl.isLE[h] = true
}

func (s *Session) left(h Handle) {
	s.acl.mu.Lock()
	defer s.acl.mu.Unlock()
	pool := s.acl.classic
	if s.acl.isLE[h] {
		pool = s.acl.le
	}
	if n := s.acl.held[h]; n > 0 && pool != nil {
		_ = pool.give(n)
	}
	delete(s.acl.held, h)
	delete(s.acl.isLE, h)
	delete(s.acl.sending, h)
}

func (s *Session) track(e event) func() {
	switch e.Code {
	case pair.EventDisconnectionComplete:
		if h, ok := pair.Handle(e.Params); ok {
			return func() { s.left(Handle(h)) }
		}
	case eventLEMeta:
		if len(e.Params) >= 4 && (e.Params[0] == leConnectionComplete ||
			e.Params[0] == leEnhancedConnComplete) && e.Params[1] == 0 {
			s.joined(Handle(binary.LittleEndian.Uint16(e.Params[2:]) & handleMask))
		}
	}
	return nil
}

func eventHandle(e event) (Handle, bool) {
	switch e.Code {
	case pair.EventDisconnectionComplete, eventEncryptionChange, eventKeyRefreshDone:
		if len(e.Params) >= 3 {
			return Handle(binary.LittleEndian.Uint16(e.Params[1:]) & handleMask), true
		}
	}
	return 0, false
}

func (s *Session) classicWants(g packet) bool {
	switch g.kind {
	case typeACL:
		return !s.onLE(g.acl.Handle)
	case typeEvent:
		if g.event.Code == eventLEMeta {
			return false
		}
		if h, ok := eventHandle(g.event); ok && s.onLE(h) {
			return false
		}
		return true
	}
	return false
}
