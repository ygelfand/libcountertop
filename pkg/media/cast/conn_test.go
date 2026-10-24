package cast

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

const patience = 2 * time.Second

type sender struct {
	t    *testing.T
	conn net.Conn
	held []byte
}

func (s *sender) send(m Message) {
	s.t.Helper()

	if err := s.conn.SetWriteDeadline(time.Now().Add(patience)); err != nil {
		s.t.Fatal(err)
	}
	if _, err := s.conn.Write(Frame(m)); err != nil {
		s.t.Fatalf("writing: %v", err)
	}
}

func (s *sender) next() (Message, error) {
	buf := make([]byte, readChunk)

	for {
		m, took, err := Next(s.held)
		if err == nil {
			s.held = s.held[took:]
			return m, nil
		}
		if !errors.Is(err, ErrShort) {
			return Message{}, err
		}

		if err := s.conn.SetReadDeadline(time.Now().Add(patience)); err != nil {
			return Message{}, err
		}
		n, err := s.conn.Read(buf)
		if n > 0 {
			s.held = append(s.held, buf[:n]...)
			continue
		}
		if err != nil {
			return Message{}, err
		}
	}
}

func (s *sender) expect() Message {
	s.t.Helper()

	m, err := s.next()
	if err != nil {
		s.t.Fatalf("reading: %v", err)
	}
	return m
}

func dial(t *testing.T, s *Service) *sender {
	t.Helper()

	ours, theirs := net.Pipe()
	t.Cleanup(func() { ours.Close(); theirs.Close() })

	go func() { _ = s.Serve(NewConn(theirs)) }()

	return &sender{t: t, conn: ours}
}

func TestClosingTheServiceEndsEveryConnection(t *testing.T) {
	s := NewService(NewReceiver("Kitchen"))
	spoken, quiet := dial(t, s), dial(t, s)
	spoken.send(Message{
		Source:      "sender-0",
		Destination: "receiver-0",
		Namespace:   NSConnection,
		Payload:     `{"type":"CONNECT"}`,
	})
	waitFor(t, s, 1)

	s.Close()

	for name, c := range map[string]*sender{"a sender that spoke": spoken, "a quiet sender": quiet} {
		if _, err := c.next(); !hungUp(err) {
			t.Errorf("%s was not disconnected: %v", name, err)
		}
	}
	waitFor(t, s, 0)
}

func hungUp(err error) bool { return errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) }

func TestASenderArrivingAfterCloseIsTurnedAway(t *testing.T) {
	s := NewService(NewReceiver("Kitchen"))
	s.Close()

	ours, theirs := net.Pipe()
	t.Cleanup(func() { ours.Close() })
	if err := s.Serve(NewConn(theirs)); !errors.Is(err, ErrGone) {
		t.Errorf("serving after close answered %v, want ErrGone", err)
	}
	if _, err := (&sender{t: t, conn: ours}).next(); !hungUp(err) {
		t.Errorf("a late sender was kept: %v", err)
	}
}

func TestAConversationOverAConnection(t *testing.T) {
	s := NewService(NewReceiver("Kitchen"))
	s.Receiver.Player = &player{}

	phone := dial(t, s)

	phone.send(from(SenderID, NSConnection, Connect()))
	phone.send(from(SenderID, NSHeartbeat, Ping()))

	pong := phone.expect()
	if h, _ := Kind(pong.Payload); h.Type != TypePong {
		t.Fatalf("came back %q", h.Type)
	}
	if pong.Destination != SenderID {
		t.Errorf("the pong went to %q", pong.Destination)
	}

	phone.send(from(SenderID, NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`))

	launched := phone.expect()
	if h, _ := Kind(launched.Payload); h.Type != TypeStatus {
		t.Fatalf("a launch came back as %q", h.Type)
	}
}

func TestAChangeReachesTheOtherSendersOwnConnection(t *testing.T) {
	s := NewService(NewReceiver("Kitchen"))
	s.Receiver.Player = &player{}

	kitchen := dial(t, s)
	hall := dial(t, s)

	kitchen.send(from("sender-kitchen", NSConnection, Connect()))
	hall.send(from("sender-hall", NSConnection, Connect()))

	waitFor(t, s, 2)

	kitchen.send(from("sender-kitchen", NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`))

	answer := kitchen.expect()
	if answer.Destination != "sender-kitchen" {
		t.Errorf("the answer went to %q", answer.Destination)
	}
	if h, _ := Kind(answer.Payload); h.RequestID != 1 {
		t.Errorf("the answer carries request %d", h.RequestID)
	}

	told := hall.expect()
	if told.Destination != Broadcast {
		t.Errorf("the broadcast went to %q", told.Destination)
	}
	if h, _ := Kind(told.Payload); h.RequestID != 0 {
		t.Errorf("the broadcast carries request %d, want 0", h.RequestID)
	}
}

func waitFor(t *testing.T, s *Service, n int) {
	t.Helper()

	deadline := time.Now().Add(patience)
	for time.Now().Before(deadline) {
		if s.Senders() >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("only %d of %d connections were learned", s.Senders(), n)
}

func TestASenderThatWentAwayIsForgotten(t *testing.T) {
	s := NewService(NewReceiver("Kitchen"))

	kitchen := dial(t, s)
	hall := dial(t, s)

	kitchen.send(from("sender-kitchen", NSConnection, Connect()))
	hall.send(from("sender-hall", NSConnection, Connect()))
	waitFor(t, s, 2)

	hall.conn.Close()

	deadline := time.Now().Add(patience)
	for time.Now().Before(deadline) && s.Senders() > 1 {
		time.Sleep(time.Millisecond)
	}
	if got := s.Senders(); got != 1 {
		t.Fatalf("%d connections after one closed", got)
	}

	kitchen.send(from("sender-kitchen", NSReceiver, `{"type":"GET_STATUS","requestId":1}`))

	got := kitchen.expect()
	if got.Destination != "sender-kitchen" {
		t.Errorf("came back to %q", got.Destination)
	}
}

func TestSeveralMessagesInOneWrite(t *testing.T) {
	s := NewService(NewReceiver("Kitchen"))
	phone := dial(t, s)

	var buf []byte
	buf = append(buf, Frame(from(SenderID, NSConnection, Connect()))...)
	buf = append(buf, Frame(from(SenderID, NSHeartbeat, Ping()))...)
	buf = append(buf, Frame(from(SenderID, NSHeartbeat, Ping()))...)

	if err := phone.conn.SetWriteDeadline(time.Now().Add(patience)); err != nil {
		t.Fatal(err)
	}
	if _, err := phone.conn.Write(buf); err != nil {
		t.Fatalf("writing: %v", err)
	}

	for i := range 2 {
		got := phone.expect()
		if h, _ := Kind(got.Payload); h.Type != TypePong {
			t.Errorf("answer %d is %q", i, h.Type)
		}
	}
}

func TestAMessageSplitAcrossWrites(t *testing.T) {
	s := NewService(NewReceiver("Kitchen"))
	phone := dial(t, s)

	framed := Frame(from(SenderID, NSHeartbeat, Ping()))

	go func() {
		_ = phone.conn.SetWriteDeadline(time.Now().Add(patience))
		_, _ = phone.conn.Write(framed[:3])
		time.Sleep(10 * time.Millisecond)
		_ = phone.conn.SetWriteDeadline(time.Now().Add(patience))
		_, _ = phone.conn.Write(framed[3:])
	}()

	if h, _ := Kind(phone.expect().Payload); h.Type != TypePong {
		t.Errorf("came back %q", h.Type)
	}
}

func TestABadPayloadIsReportedAndTheConnectionKept(t *testing.T) {
	s := NewService(NewReceiver("Kitchen"))

	var mu sync.Mutex
	var faults int
	s.Fault = func(error) { mu.Lock(); faults++; mu.Unlock() }

	phone := dial(t, s)

	phone.send(from(SenderID, NSReceiver, `not json`))
	phone.send(from(SenderID, NSHeartbeat, Ping()))

	if h, _ := Kind(phone.expect().Payload); h.Type != TypePong {
		t.Error("the connection did not survive a bad payload")
	}

	mu.Lock()
	defer mu.Unlock()
	if faults != 1 {
		t.Errorf("%d faults reported, want one", faults)
	}
}

func TestAnUnframeableStreamEndsTheConnection(t *testing.T) {
	s := NewService(NewReceiver("Kitchen"))

	ours, theirs := net.Pipe()
	t.Cleanup(func() { ours.Close(); theirs.Close() })

	done := make(chan error, 1)
	go func() { done <- s.Serve(NewConn(theirs)) }()

	if err := ours.SetWriteDeadline(time.Now().Add(patience)); err != nil {
		t.Fatal(err)
	}
	if _, err := ours.Write([]byte{0x00, 0x00, 0x00, 0x04, 0xff, 0xff, 0xff, 0xff}); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Error("an unframeable stream ended cleanly")
		}
	case <-time.After(patience):
		t.Error("the connection was not closed")
	}
}

func TestAClosedConnectionIsNotAFault(t *testing.T) {
	s := NewService(NewReceiver("Kitchen"))

	ours, theirs := net.Pipe()
	t.Cleanup(func() { theirs.Close() })

	done := make(chan error, 1)
	go func() { done <- s.Serve(NewConn(theirs)) }()

	ours.Close()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
			t.Errorf("a closed connection gave %v", err)
		}
	case <-time.After(patience):
		t.Error("serving did not stop")
	}
}

func TestFinishingReachesEveryConnection(t *testing.T) {
	s := NewService(NewReceiver("Kitchen"))
	s.Receiver.Player = &player{}

	kitchen := dial(t, s)
	hall := dial(t, s)

	kitchen.send(from("sender-kitchen", NSConnection, Connect()))
	hall.send(from("sender-hall", NSConnection, Connect()))
	waitFor(t, s, 2)

	kitchen.send(from("sender-kitchen", NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`))

	launched := kitchen.expect()
	kitchen.expect()
	hall.expect()

	transport := status(t, launched.Payload).Applications[0].TransportID

	kitchen.send(toApp("sender-kitchen", transport, NSMedia,
		`{"type":"LOAD","requestId":2,"media":{"contentId":"http://example/a.mp3"}}`))
	kitchen.expect()
	kitchen.expect()
	hall.expect()

	s.Finished()

	for _, who := range []*sender{kitchen, hall} {
		got := mediaStatus(t, who.expect().Payload)
		if len(got) != 1 {
			t.Fatalf("%d statuses", len(got))
		}
		if got[0].IdleReason != IdleFinished {
			t.Errorf("idle reason %q", got[0].IdleReason)
		}
	}
}

func TestASenderThatStopsReadingIsDroppedRatherThanBlocking(t *testing.T) {
	ours, theirs := net.Pipe()
	t.Cleanup(func() { ours.Close(); theirs.Close() })

	c := NewConn(theirs)

	done := make(chan error, 1)
	go func() {
		for range outbound * 4 {
			err := c.Write(Message{
				Destination: "sender-0",
				Namespace:   NSHeartbeat,
				Payload:     Pong(),
			})
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("a sender that never read took every message without complaint")
		}
	case <-time.After(patience):
		t.Fatal("writing blocked on a sender that had stopped reading")
	}
}

func TestFinishingDoesNotWaitOnTheSocket(t *testing.T) {
	s := NewService(NewReceiver("Kitchen"))
	s.Receiver.Player = &player{}

	kitchen := dial(t, s)
	kitchen.send(from("sender-kitchen", NSConnection, Connect()))
	waitFor(t, s, 1)

	kitchen.send(from("sender-kitchen", NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`))
	transport := status(t, kitchen.expect().Payload).Applications[0].TransportID
	kitchen.expect()

	kitchen.send(toApp("sender-kitchen", transport, NSMedia,
		`{"type":"LOAD","requestId":2,"media":{"contentId":"http://example/a.mp3"}}`))
	kitchen.expect()
	kitchen.expect()

	done := make(chan struct{})
	go func() { s.Finished(); close(done) }()

	select {
	case <-done:
	case <-time.After(patience):
		t.Fatal("finishing waited for the socket, which on the device is the audio path waiting")
	}

	got := mediaStatus(t, kitchen.expect().Payload)
	if len(got) != 1 || got[0].IdleReason != IdleFinished {
		t.Errorf("the status came back %+v", got)
	}
}

func TestSendingToNobody(t *testing.T) {
	s := NewService(NewReceiver("Kitchen"))

	var faults int
	s.Fault = func(error) { faults++ }

	s.Send(Message{Destination: "sender-nobody", Namespace: NSReceiver, Payload: Pong()})

	if faults != 0 {
		t.Errorf("%d faults for a sender that is not there", faults)
	}
}

func TestTwoSendersWithTheSameNameKeepTheirOwnReplies(t *testing.T) {
	s := NewService(NewReceiver("Kitchen"))

	music := dial(t, s)
	home := dial(t, s)

	music.send(from("sender-0", NSConnection, Connect()))
	home.send(from("sender-0", NSConnection, Connect()))
	waitFor(t, s, 2)

	s.mu.Lock()
	var ids []string
	for id := range s.conns {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	if len(ids) != 2 {
		t.Fatalf("two senders named sender-0 are known as %v", ids)
	}
	for i, id := range ids {
		s.Send(Message{Source: "receiver-0", Destination: id, Namespace: NSReceiver,
			Payload: fmt.Sprintf(`{"type":"RECEIVER_STATUS","requestId":%d}`, i+1)})
	}
	seen := map[int]bool{}
	for _, c := range []*sender{music, home} {
		got := c.expect()
		h, _ := Kind(got.Payload)
		if got.Destination != "sender-0" || seen[h.RequestID] {
			t.Fatalf("a sender got request %d addressed to %q; already seen %v",
				h.RequestID, got.Destination, seen)
		}
		seen[h.RequestID] = true
	}

	home.conn.Close()
	deadline := time.Now().Add(patience)
	for time.Now().Before(deadline) && s.Senders() > 1 {
		time.Sleep(time.Millisecond)
	}
	music.send(from("sender-0", NSReceiver, `{"type":"GET_STATUS","requestId":8}`))
	if got := music.expect(); got.Destination != "sender-0" {
		t.Fatalf("after the other sender-0 left, the first one heard back as %q", got.Destination)
	}
}
