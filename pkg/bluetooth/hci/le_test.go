//go:build linux

package hci

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func takeCommand(t *testing.T, chip *os.File) (uint16, []byte) {
	t.Helper()

	if err := chip.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = chip.SetReadDeadline(time.Time{}) }()

	head := make([]byte, 4)
	if _, err := io.ReadFull(chip, head); err != nil {
		t.Fatalf("waiting for a command: %v", err)
	}
	if head[0] != typeCommand {
		t.Fatalf("got packet type %#02x, want a command", head[0])
	}
	params := make([]byte, head[3])
	if _, err := io.ReadFull(chip, params); err != nil {
		t.Fatalf("reading the parameters: %v", err)
	}
	return binary.LittleEndian.Uint16(head[1:]), params
}

func masked(t *testing.T, chip *os.File) {
	t.Helper()
	op, params := takeCommand(t, chip)
	if op != hciLESetEventMask || !bytes.Equal(params, leEventMask[:]) {
		t.Fatalf("got %#04x % x, want the LE event mask % x", op, params, leEventMask)
	}
	write(t, chip, commandComplete(hciLESetEventMask, 0))
}

func commandStatus(opcode uint16, status byte) []byte {
	out := []byte{typeEvent, eventCommandStatus, 0x04, status, 0x01}
	return binary.LittleEndian.AppendUint16(out, opcode)
}

func commandComplete(opcode uint16, ret ...byte) []byte {
	out := []byte{typeEvent, eventCommandComplete, byte(3 + len(ret)), 0x01}
	out = binary.LittleEndian.AppendUint16(out, opcode)
	return append(out, ret...)
}

// Subevent, status, handle, role, peer address type, peer address, interval, latency, timeout, accuracy.
func leConnected(status byte, h Handle, addressType byte, address [6]byte) []byte {
	out := []byte{typeEvent, eventLEMeta, 19, leConnectionComplete, status}
	out = binary.LittleEndian.AppendUint16(out, uint16(h))
	out = append(out, 0x00, addressType)
	for i := range 6 {
		out = append(out, address[5-i])
	}
	return append(out, 0x18, 0x00, 0x00, 0x00, 0x90, 0x01, 0x00)
}

func disconnected(h Handle, reason byte) []byte {
	out := []byte{typeEvent, 0x05, 0x04, 0x00}
	out = binary.LittleEndian.AppendUint16(out, uint16(h))
	return append(out, reason)
}

func write(t *testing.T, chip *os.File, b []byte) {
	t.Helper()
	if _, err := chip.Write(b); err != nil {
		t.Fatal(err)
	}
}

func connecting(t *testing.T, s *Session, chip *os.File, h Handle) *Link {
	t.Helper()

	address := [6]byte{0xC0, 0x11, 0x22, 0x33, 0x44, 0x55}
	type result struct {
		l   *Link
		err error
	}
	back := make(chan result, 1)
	go func() {
		l, err := s.Connect(context.Background(), address, 0x01)
		back <- result{l, err}
	}()

	op, params := takeCommand(t, chip)
	if op != hciLECreateConnection || len(params) != 25 {
		t.Fatalf("got %#04x with %d bytes, want LE Create Connection with 25", op, len(params))
	}
	if params[5] != 0x01 || params[6] != 0x55 || params[11] != 0xC0 {
		t.Fatalf("peer type and address went out as % x", params[5:12])
	}
	write(t, chip, commandStatus(hciLECreateConnection, 0))
	write(t, chip, leConnected(0, h, 0x01, address))

	got := <-back
	if got.err != nil {
		t.Fatalf("connect: %v", got.err)
	}
	if got.l.Handle != h || got.l.Address != address || got.l.AddressType != 0x01 {
		t.Fatalf("link is %#04x %x type %d", got.l.Handle, got.l.Address, got.l.AddressType)
	}
	return got.l
}

func TestALinkCarriesItsChannelsAndEndsOnDisconnection(t *testing.T) {
	p, chip := paired(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := Reader(ctx, p)

	l := connecting(t, s, chip, 0x0040)

	write(t, chip, aclOf(0x0040, 0x02, []byte{0x03, 0x00, 0x04, 0x00, 0x1b, 0x05, 0x00}))
	time.Sleep(50 * time.Millisecond)
	heard := make(chan []byte, 2)
	l.On(0x0004, func(b []byte) { heard <- append([]byte(nil), b...) })
	write(t, chip, aclOf(0x0040, 0x02, []byte{0x02, 0x00, 0x04, 0x00, 0x1b, 0x06}))

	for _, want := range [][]byte{{0x1b, 0x05, 0x00}, {0x1b, 0x06}} {
		select {
		case b := <-heard:
			if !bytes.Equal(b, want) {
				t.Fatalf("heard % x, want % x", b, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("the channel handler never ran")
		}
	}

	sent := make(chan error, 1)
	go func() { sent <- l.Send(context.Background(), 0x0004, []byte{0x0a, 0x03, 0x00}) }()

	asked(t, chip, hciReadBufferSize)
	write(t, chip, bufferSize(64, 4))
	asked(t, chip, hciLEReadBufferSize)
	write(t, chip, leBufferSize(27, 2))

	want := aclOf(0x0040, 0x00, []byte{0x03, 0x00, 0x04, 0x00, 0x0a, 0x03, 0x00})
	got := make([]byte, len(want))
	if _, err := io.ReadFull(chip, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("sent % x, want % x", got, want)
	}
	if err := <-sent; err != nil {
		t.Fatalf("send: %v", err)
	}

	write(t, chip, disconnected(0x0040, 0x13))
	select {
	case <-l.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the link outlived its disconnection")
	}
	if l.Reason() != 0x13 {
		t.Fatalf("reason %#02x, want 0x13", l.Reason())
	}
	if err := l.Send(context.Background(), 0x0004, []byte{0x01}); err != ErrClosed {
		t.Fatalf("send after close: %v, want ErrClosed", err)
	}
	if s.onLE(0x0040) {
		t.Fatal("the handle is still routed as LE")
	}
	if n := s.acl.le.total; n != 2 {
		t.Fatalf("le pool holds %d, want 2", n)
	}
}

func TestALinkAsksTheControllerForNewTiming(t *testing.T) {
	p, chip := paired(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := Reader(ctx, p)
	l := connecting(t, s, chip, 0x0042)

	back := make(chan error, 1)
	go func() { back <- l.Update(6, 12, 0, 500) }()
	op, params := takeCommand(t, chip)
	want := []byte{0x42, 0x00, 6, 0, 12, 0, 0, 0, 0xf4, 0x01, 0, 0, 0, 0}
	if op != hciLEConnectionUpdate || !bytes.Equal(params, want) {
		t.Fatalf("got %#04x % x, want LE Connection Update % x", op, params, want)
	}
	write(t, chip, commandStatus(hciLEConnectionUpdate, 0x12))
	if err := <-back; err != Status(0x12) {
		t.Fatalf("got %v, want the controller's refusal 0x12", err)
	}
}

func TestAConnectionThatFailsReportsItsStatus(t *testing.T) {
	p, chip := paired(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := Reader(ctx, p)

	back := make(chan error, 1)
	go func() {
		_, err := s.Connect(context.Background(), [6]byte{1, 2, 3, 4, 5, 6}, 0)
		back <- err
	}()
	if op, _ := takeCommand(t, chip); op != hciLECreateConnection {
		t.Fatalf("got %#04x", op)
	}
	write(t, chip, commandStatus(hciLECreateConnection, 0))
	write(t, chip, leConnected(byte(StatusFailedToEstablish), 0, 0, [6]byte{}))

	if err := <-back; err != StatusFailedToEstablish {
		t.Fatalf("got %v, want %v", err, StatusFailedToEstablish)
	}
}

func TestACancelledConnectionAsksTheControllerToStop(t *testing.T) {
	p, chip := paired(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := Reader(ctx, p)

	dial, stop := context.WithCancel(context.Background())
	back := make(chan error, 1)
	go func() {
		_, err := s.Connect(dial, [6]byte{1, 2, 3, 4, 5, 6}, 0)
		back <- err
	}()
	if op, _ := takeCommand(t, chip); op != hciLECreateConnection {
		t.Fatalf("got %#04x", op)
	}
	write(t, chip, commandStatus(hciLECreateConnection, 0))
	stop()

	asked(t, chip, hciLECreateCancel)
	write(t, chip, commandComplete(hciLECreateCancel, 0x00))
	write(t, chip, leConnected(byte(StatusUnknownConnection), 0, 0, [6]byte{}))

	if err := <-back; !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want a cancelled connect", err)
	}
}

func TestAControllerThatSetsParamsGetsTheLEEventMask(t *testing.T) {
	p, chip := paired(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := Reader(ctx, p)
	s.quirks = ControllerSetsParams

	go func() { _, _ = s.Connect(context.Background(), [6]byte{1, 2, 3, 4, 5, 6}, 0) }()
	masked(t, chip)
	if op, _ := takeCommand(t, chip); op != hciLECreateConnection {
		t.Fatalf("got %#04x, want LE Create Connection after the mask", op)
	}
	write(t, chip, commandStatus(hciLECreateConnection, 0))
	write(t, chip, leConnected(byte(StatusFailedToEstablish), 0, 0, [6]byte{}))
}

func TestLEDataStaysOutOfTheClassicLoop(t *testing.T) {
	p, chip := paired(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := Reader(ctx, p)

	d := &deciding{}
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = Serve(ctx, s, d)
	}()
	defer func() { cancel(); <-served }()

	asked(t, chip, hciReadBufferSize)
	write(t, chip, bufferSize(64, 4))
	asked(t, chip, hciLEReadBufferSize)
	write(t, chip, leBufferSize(0, 0))

	l := connecting(t, s, chip, 0x0041)
	heard := make(chan []byte, 1)
	l.On(0x0004, func(b []byte) { heard <- b })

	write(t, chip, aclOf(0x0041, 0x02, []byte{0x01, 0x00, 0x04, 0x00, 0x1b}))
	write(t, chip, aclOf(0x0050, 0x02, []byte{0x01, 0x00, 0x40, 0x00, 0xaa}))

	select {
	case <-heard:
	case <-time.After(2 * time.Second):
		t.Fatal("the link never heard its data")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		events, pdus, _ := d.saw()
		if pdus == 1 {
			if events != 0 {
				t.Fatalf("classic saw %d events, want none", events)
			}
			break
		}
		if pdus > 1 || time.Now().After(deadline) {
			t.Fatalf("classic saw %d PDUs, want 1", pdus)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPausingStopsAScanAndResumingStartsIt(t *testing.T) {
	p, chip := paired(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := Reader(ctx, p)

	scanCtx, stopScan := context.WithCancel(context.Background())
	defer stopScan()
	go func() { _ = Scan(scanCtx, s, Scanning{Percent: 50}, func(Advertisement) {}) }()

	if op, _ := takeCommand(t, chip); op != hciLEScanParams {
		t.Fatalf("got %#04x, want scan parameters", op)
	}
	write(t, chip, commandComplete(hciLEScanParams, 0))
	if op, params := takeCommand(t, chip); op != hciLEScanEnable || params[0] != 1 {
		t.Fatalf("got %#04x % x, want scan on", op, params)
	}
	write(t, chip, commandComplete(hciLEScanEnable, 0))

	resumed := make(chan func(), 1)
	go func() { resumed <- s.Pause() }()
	if op, params := takeCommand(t, chip); op != hciLEScanEnable || params[0] != 0 {
		t.Fatalf("got %#04x % x, want scan off", op, params)
	}
	write(t, chip, commandComplete(hciLEScanEnable, 0))
	resume := <-resumed

	done := make(chan struct{})
	go func() { resume(); resume(); close(done) }()
	if op, _ := takeCommand(t, chip); op != hciLEScanParams {
		t.Fatalf("got %#04x, want scan parameters", op)
	}
	write(t, chip, commandComplete(hciLEScanParams, 0))
	if op, params := takeCommand(t, chip); op != hciLEScanEnable || params[0] != 1 {
		t.Fatalf("got %#04x % x, want scan on", op, params)
	}
	write(t, chip, commandComplete(hciLEScanEnable, 0))
	<-done
}
