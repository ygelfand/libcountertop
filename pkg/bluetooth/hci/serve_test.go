//go:build linux

package hci

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/bluetooth/pair"
)

type deciding struct {
	mu sync.Mutex

	events []pair.Event
	pdus   [][]byte
	gone   []uint16

	reply [][]byte

	commands []pair.Command
}

func (d *deciding) Event(e pair.Event) ([]pair.Command, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.events = append(d.events, e)
	return d.commands, nil
}

func (d *deciding) Data(handle uint16, pdu []byte) ([][]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.pdus = append(d.pdus, append([]byte(nil), pdu...))
	return d.reply, nil
}

func (d *deciding) Gone(handle uint16) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.gone = append(d.gone, handle)
}

func (d *deciding) saw() (int, int, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.events), len(d.pdus), len(d.gone)
}

func aclOf(h Handle, boundary byte, body []byte) []byte {
	out := []byte{typeACL}
	out = binary.LittleEndian.AppendUint16(out, uint16(h)|uint16(boundary)<<12)
	out = binary.LittleEndian.AppendUint16(out, uint16(len(body)))
	return append(out, body...)
}

// Status, ACL length, SCO length, ACL count, SCO count.
func bufferSize(size, total int) []byte {
	out := []byte{typeEvent, eventCommandComplete, 0x0b, 0x01}
	out = binary.LittleEndian.AppendUint16(out, hciReadBufferSize)
	out = append(out, 0x00)
	out = binary.LittleEndian.AppendUint16(out, uint16(size))
	out = append(out, 0x00)
	out = binary.LittleEndian.AppendUint16(out, uint16(total))
	return binary.LittleEndian.AppendUint16(out, 0)
}

// Status, LE ACL length, LE ACL count.
func leBufferSize(size, total int) []byte {
	out := []byte{typeEvent, eventCommandComplete, 0x07, 0x01}
	out = binary.LittleEndian.AppendUint16(out, hciLEReadBufferSize)
	out = append(out, 0x00)
	out = binary.LittleEndian.AppendUint16(out, uint16(size))
	return append(out, byte(total))
}

func serving(t *testing.T, d *deciding, size, total int) (chip *os.File, stop func()) {
	t.Helper()

	p, chip := paired(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		_ = Serve(ctx, Reader(ctx, p), d)
	}()

	asked(t, chip, hciReadBufferSize)
	if _, err := chip.Write(bufferSize(size, total)); err != nil {
		t.Fatal(err)
	}
	asked(t, chip, hciLEReadBufferSize)
	if _, err := chip.Write(leBufferSize(0, 0)); err != nil {
		t.Fatal(err)
	}

	return chip, func() { cancel(); <-done }
}

func asked(t *testing.T, chip *os.File, opcode uint16) {
	t.Helper()

	want, err := command(opcode)
	if err != nil {
		t.Fatal(err)
	}

	got := make([]byte, len(want))
	if err := chip.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(chip, got); err != nil {
		t.Fatalf("waiting for %#04x: %v", opcode, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("the loop sent % x, want % x", got, want)
	}

	if err := chip.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
}

func TestALinkCarriesAWholePDUFromItsFragments(t *testing.T) {
	d := &deciding{}
	chip, stop := serving(t, d, 64, 4)
	defer stop()

	// An L2CAP header saying 36 bytes of payload on channel 0x40.
	body := append([]byte{0x24, 0x00, 0x40, 0x00}, make([]byte, 36)...)
	for i := range body[4:] {
		body[4+i] = byte(i)
	}

	_, _ = chip.Write(aclOf(1, boundaryFlushable, body[:20]))
	_, _ = chip.Write(aclOf(1, boundaryContinue, body[20:]))

	settles(t, func() bool { _, pdus, _ := d.saw(); return pdus == 1 })

	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.pdus[0]) != len(body) {
		t.Fatalf("a PDU of %d bytes came back as %d", len(body), len(d.pdus[0]))
	}
	for i := range body {
		if d.pdus[0][i] != body[i] {
			t.Fatalf("byte %d is %#02x, want %#02x", i, d.pdus[0][i], body[i])
		}
	}
}

func TestAnEventReachesWhoeverDecides(t *testing.T) {
	d := &deciding{}
	chip, stop := serving(t, d, 64, 4)
	defer stop()

	_, _ = chip.Write([]byte{typeEvent, pair.EventConnectionRequest, 0x03, 0xaa, 0xbb, 0xcc})

	settles(t, func() bool { events, _, _ := d.saw(); return events >= 1 })

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.events[0].Code != pair.EventConnectionRequest {
		t.Errorf("event %#02x reached the decider", d.events[0].Code)
	}
}

func TestALinkEndingIsPassedOnAndForgotten(t *testing.T) {
	d := &deciding{}
	chip, stop := serving(t, d, 64, 4)
	defer stop()

	half := append([]byte{0x24, 0x00, 0x40, 0x00}, make([]byte, 16)...)
	_, _ = chip.Write(aclOf(7, boundaryFlushable, half))

	// Disconnection complete: status, handle, reason.
	_, _ = chip.Write(
		[]byte{typeEvent, pair.EventDisconnectionComplete, 0x04, 0x00, 0x07, 0x00, 0x13},
	)

	settles(t, func() bool { _, _, gone := d.saw(); return gone == 1 })

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.gone[0] != 7 {
		t.Errorf("handle %d went, want 7", d.gone[0])
	}
}

func TestSendingWaitsForTheControllerToCatchUp(t *testing.T) {
	// 200 bytes over 32 byte packets is seven fragments, against two buffers.
	d := &deciding{reply: [][]byte{make([]byte, 200)}}

	chip, stop := serving(t, d, 32, 2)
	defer stop()

	var sent counter
	go func() { _, _ = io.Copy(&sent, chip) }()

	_, _ = chip.Write(aclOf(1, boundaryFlushable, []byte{0x00, 0x00, 0x40, 0x00}))
	settles(t, func() bool { _, pdus, _ := d.saw(); return pdus == 1 })

	settles(t, func() bool { return sent.n() >= 2 })

	before := sent.n()
	time.Sleep(50 * time.Millisecond)

	if now := sent.n(); now != before {
		t.Errorf("%d packets went out with no buffers free, was %d", now, before)
	}

	for range 5 {
		_, _ = chip.Write(
			[]byte{typeEvent, eventCompletedPackets, 0x05, 0x01, 0x01, 0x00, 0x01, 0x00},
		)
		time.Sleep(5 * time.Millisecond)
	}

	settles(t, func() bool { return sent.n() >= 7 })
}

type counter struct {
	mu   sync.Mutex
	seen int
	held []byte
}

func (c *counter) Write(b []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.held = append(c.held, b...)

	for len(c.held) > 0 {
		var n int

		switch c.held[0] {
		case typeACL:
			if len(c.held) < aclHeader {
				return len(b), nil
			}
			n = aclHeader + int(binary.LittleEndian.Uint16(c.held[3:]))

		case typeCommand:
			// Type, opcode, length.
			const header = 4
			if len(c.held) < header {
				return len(b), nil
			}
			n = header + int(c.held[3])

		default:
			c.held = c.held[1:]
			continue
		}

		if len(c.held) < n {
			return len(b), nil
		}
		if c.held[0] == typeACL {
			c.seen++
		}
		c.held = c.held[n:]
	}
	return len(b), nil
}

func (c *counter) n() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seen
}

func settles(t *testing.T, want func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if want() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out")
}
