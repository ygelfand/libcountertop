package cast

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
)

const readChunk = 4096

const outbound = 32

var ErrGone = errors.New("cast: the connection has gone")

type Conn struct {
	conn net.Conn

	held []byte
	buf  []byte

	out    chan Message
	closed chan struct{}
	once   sync.Once

	Trace func(in bool, m Message)
}

func (c *Conn) Peer() string { return c.conn.RemoteAddr().String() }

func NewConn(c net.Conn) *Conn {
	k := &Conn{
		conn:   c,
		out:    make(chan Message, outbound),
		closed: make(chan struct{}),
	}

	go k.writing()
	return k
}

func (c *Conn) writing() {
	for {
		select {
		case m := <-c.out:
			if _, err := c.conn.Write(Frame(m)); err != nil {
				_ = c.Close()
				return
			}
		case <-c.closed:
			return
		}
	}
}

func (c *Conn) Read() (Message, error) {
	for {
		m, took, err := Next(c.held)
		if err == nil {
			c.held = c.held[took:]
			if c.Trace != nil {
				c.Trace(true, m)
			}
			return m, nil
		}
		if !errors.Is(err, ErrShort) {
			return Message{}, err
		}

		if c.buf == nil {
			c.buf = make([]byte, readChunk)
		}

		n, err := c.conn.Read(c.buf)
		if n > 0 {
			c.held = append(c.held, c.buf[:n]...)
			continue
		}
		if err != nil {
			return Message{}, err
		}
	}
}

func (c *Conn) Write(m Message) error {
	select {
	case <-c.closed:
		return ErrGone
	default:
	}

	if c.Trace != nil {
		c.Trace(false, m)
	}
	select {
	case c.out <- m:
		return nil
	case <-c.closed:
		return ErrGone
	default:
		_ = c.Close()
		return fmt.Errorf("cast: %s is not reading, so it has been dropped", m.Destination)
	}
}

func (c *Conn) Close() error {
	c.once.Do(func() {
		close(c.closed)
		c.conn.Close()
	})
	return nil
}

type Service struct {
	Receiver *Receiver

	Fault func(error)

	mu    sync.Mutex
	conns map[string]*Conn
	open  map[*Conn]int
	next  int
}

func NewService(r *Receiver) *Service {
	return &Service{Receiver: r, conns: map[string]*Conn{}, open: map[*Conn]int{}}
}

func (s *Service) Serve(c *Conn) error {
	n, ok := s.admit(c)
	if !ok {
		_ = c.Close()
		return ErrGone
	}
	known := map[string]bool{}

	defer func() {
		_ = c.Close()
		s.release(c)
		for id := range known {
			s.forget(id)
		}
	}()

	for {
		m, err := c.Read()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) ||
				errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}

		if m.Source != "" {
			m.Source = qualify(n, m.Source)
			if !known[m.Source] {
				known[m.Source] = true
				s.remember(m.Source, c)
			}
		}

		out, fault := s.handle(m)
		if fault != nil && s.Fault != nil {
			s.Fault(fault)
		}

		for _, a := range out {
			if a.Destination == m.Source {
				if err := c.Write(onWire(a)); err != nil {
					return err
				}
				continue
			}
			s.Send(a)
		}
	}
}

func (s *Service) handle(m Message) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.Receiver.Receive(m)
}

func (s *Service) Send(m Message) {
	s.mu.Lock()
	var to []*Conn
	if m.Destination == Broadcast {
		seen := map[*Conn]bool{}
		for _, c := range s.conns {
			if !seen[c] {
				seen[c] = true
				to = append(to, c)
			}
		}
	} else if c := s.conns[m.Destination]; c != nil {
		to = append(to, c)
	}
	s.mu.Unlock()

	for _, c := range to {
		if err := c.Write(onWire(m)); err != nil && !errors.Is(err, ErrGone) && s.Fault != nil {
			s.Fault(err)
		}
	}
}

func (s *Service) Announce(out []Message) {
	for _, m := range out {
		s.Send(m)
	}
}

func (s *Service) Report(level float64, muted bool) {
	s.mu.Lock()
	out := s.Receiver.Report(level, muted)
	s.mu.Unlock()
	s.Announce(out)
}

func (s *Service) Publish(control Controls, p *Published) {
	s.mu.Lock()
	out := s.Receiver.Publish(control, p)
	s.mu.Unlock()

	s.Announce(out)
}

func (s *Service) Advertise(app, name string, namespaces []string) {
	s.mu.Lock()
	out := s.Receiver.Advertise(app, name, namespaces)
	s.mu.Unlock()

	s.Announce(out)
}

func (s *Service) End() {
	s.mu.Lock()
	out := s.Receiver.End()
	s.mu.Unlock()

	s.Announce(out)
}

func (s *Service) Done() {
	s.mu.Lock()
	out := s.Receiver.Done()
	s.mu.Unlock()

	s.Announce(out)
}

func (s *Service) Finished() {
	s.mu.Lock()
	out := s.Receiver.Finished()
	s.mu.Unlock()

	s.Announce(out)
}

func (s *Service) remember(id string, c *Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.conns[id] = c
}

func (s *Service) forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.conns, id)
	delete(s.Receiver.senders, id)
}

func (s *Service) Close() {
	s.mu.Lock()
	open := s.open
	s.open = nil
	s.mu.Unlock()

	for c := range open {
		_ = c.Close()
	}
}

func (s *Service) admit(c *Conn) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.open == nil {
		return 0, false
	}
	s.next++
	s.open[c] = s.next
	return s.next, true
}

func (s *Service) release(c *Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.open, c)
}

func qualify(conn int, source string) string { return fmt.Sprintf("%d:%s", conn, source) }

func onWire(m Message) Message {
	if _, source, ok := strings.Cut(m.Destination, ":"); ok && m.Destination != Broadcast {
		m.Destination = source
	}
	return m
}

func (s *Service) Senders() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.conns)
}
