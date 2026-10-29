package gatt

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"
)

type attr struct {
	handle uint16
	typ    []byte
	value  []byte
}

type peripheral struct {
	t *testing.T

	mu       sync.Mutex
	handlers map[uint16]func([]byte)
	done     chan struct{}
	closed   sync.Once
	in       chan []byte

	mtu     int
	attrs   []attr
	written map[uint16][]byte
	queued  []byte
	cccd    chan struct{}
}

var custom = UUID{
	0x12,
	0x34,
	0x56,
	0x78,
	0x12,
	0x34,
	0x56,
	0x78,
	0x12,
	0x34,
	0x56,
	0x78,
	0x9a,
	0xbc,
	0,
	0,
}

func reversed(u UUID) []byte {
	b := make([]byte, 16)
	for i := range 16 {
		b[i] = u[15-i]
	}
	return b
}

func u16(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }

func newPeripheral(t *testing.T) *peripheral {
	charUUID := custom
	charUUID[15] = 1
	long := bytes.Repeat([]byte("0123456789"), 13)

	p := &peripheral{
		t:        t,
		handlers: map[uint16]func([]byte){},
		done:     make(chan struct{}),
		in:       make(chan []byte, 64),
		mtu:      64,
		written:  map[uint16][]byte{},
		cccd:     make(chan struct{}, 1),
		attrs: []attr{
			{1, u16(0x2800), u16(0x1800)},
			{2, u16(0x2803), append([]byte{0x02}, append(u16(3), u16(0x2a00)...)...)},
			{3, u16(0x2a00), []byte("Fake")},
			{4, u16(0x2800), reversed(custom)},
			{5, u16(0x2803), append([]byte{0x12}, append(u16(6), reversed(charUUID)...)...)},
			{6, reversed(charUUID), long},
			{7, u16(0x2902), []byte{0, 0}},
			{8, u16(0x2803), append([]byte{0x08}, append(u16(9), u16(0x2a01)...)...)},
			{9, u16(0x2a01), nil},
		},
	}
	go p.run()
	t.Cleanup(func() { _ = p.Disconnect() })
	return p
}

func (p *peripheral) Send(ctx context.Context, cid uint16, payload []byte) error {
	if cid != CID {
		p.t.Errorf("sent on channel %#04x", cid)
	}
	select {
	case <-p.done:
		return errors.New("gone")
	case p.in <- append([]byte(nil), payload...):
		return nil
	}
}

func (p *peripheral) On(cid uint16, f func([]byte)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handlers[cid] = f
}

func (p *peripheral) Done() <-chan struct{} { return p.done }

func (p *peripheral) Disconnect() error {
	p.closed.Do(func() { close(p.done) })
	return nil
}

func (p *peripheral) run() {
	for {
		select {
		case <-p.done:
			return
		case pdu := <-p.in:
			p.att(pdu)
		}
	}
}

func (p *peripheral) send(pdu []byte) {
	p.mu.Lock()
	f := p.handlers[CID]
	p.mu.Unlock()
	f(pdu)
}

func (p *peripheral) fail(op byte, handle uint16, code byte) {
	p.send(append([]byte{attErrorRsp, op}, append(u16(handle), code)...))
}

func (p *peripheral) find(handle uint16) *attr {
	for i := range p.attrs {
		if p.attrs[i].handle == handle {
			return &p.attrs[i]
		}
	}
	return nil
}

func (p *peripheral) att(pdu []byte) {
	op := pdu[0]
	switch op {
	case attMTUReq:
		p.send(append([]byte{attMTURsp}, u16(uint16(p.mtu))...))
	case attReadByGroupReq, attReadByTypeReq:
		start, end := binary.LittleEndian.Uint16(pdu[1:]), binary.LittleEndian.Uint16(pdu[3:])
		typ := pdu[5:]
		var out []byte
		size := 0
		for i, a := range p.attrs {
			if a.handle < start || a.handle > end || !bytes.Equal(a.typ, typ) {
				continue
			}
			entry := u16(a.handle)
			if op == attReadByGroupReq {
				groupEnd := uint16(0xffff)
				for _, b := range p.attrs[i+1:] {
					if bytes.Equal(b.typ, typ) {
						groupEnd = b.handle - 1
						break
					}
				}
				entry = append(entry, u16(groupEnd)...)
			}
			entry = append(entry, a.value...)
			if size == 0 {
				size = len(entry)
			}
			if len(entry) != size || 2+len(out)+len(entry) > p.mtu {
				break
			}
			out = append(out, entry...)
		}
		if len(out) == 0 {
			p.fail(op, start, errAttributeNotFound)
			return
		}
		p.send(append([]byte{op + 1, byte(size)}, out...))
	case attFindInfoReq:
		start, end := binary.LittleEndian.Uint16(pdu[1:]), binary.LittleEndian.Uint16(pdu[3:])
		var out []byte
		for _, a := range p.attrs {
			if a.handle >= start && a.handle <= end && len(a.typ) == 2 {
				out = append(out, append(u16(a.handle), a.typ...)...)
			}
		}
		if len(out) == 0 {
			p.fail(op, start, errAttributeNotFound)
			return
		}
		p.send(append([]byte{attFindInfoRsp, 0x01}, out...))
	case attReadReq, attReadBlobReq:
		a := p.find(binary.LittleEndian.Uint16(pdu[1:]))
		offset := 0
		if op == attReadBlobReq {
			offset = int(binary.LittleEndian.Uint16(pdu[3:]))
		}
		value := a.value[offset:]
		p.send(append([]byte{op + 1}, value[:min(len(value), p.mtu-1)]...))
	case attWriteReq:
		handle := binary.LittleEndian.Uint16(pdu[1:])
		p.written[handle] = append([]byte(nil), pdu[3:]...)
		p.send([]byte{attWriteRsp})
		if handle == 7 {
			p.cccd <- struct{}{}
		}
	case attPrepareWriteReq:
		p.queued = append(p.queued, pdu[5:]...)
		p.send(append([]byte{attPrepareWriteRsp}, pdu[1:]...))
	case attExecuteWriteReq:
		if pdu[1] == 0x01 {
			p.written[9] = p.queued
		}
		p.queued = nil
		p.send([]byte{attExecuteWriteRsp})
	case attConfirmation:
	default:
		p.t.Errorf("unexpected request %#02x", op)
	}
}

func TestTheClientWalksReadsWritesAndHearsAPeripheral(t *testing.T) {
	p := newPeripheral(t)
	c := New(p)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mtu, err := c.ExchangeMTU(ctx)
	if err != nil || mtu != 64 {
		t.Fatalf("mtu = %d, %v; want 64", mtu, err)
	}

	services, err := c.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 2 {
		t.Fatalf("services = %+v", services)
	}
	if services[0].UUID != Short(0x1800) || services[0].End != 3 ||
		services[1].UUID != custom || services[1].End != 0xffff {
		t.Errorf("services = %+v", services)
	}
	chars := services[1].Characteristics
	if len(chars) != 2 || chars[0].Handle != 6 || chars[0].Properties != 0x12 ||
		chars[1].Handle != 9 {
		t.Fatalf("characteristics = %+v", chars)
	}
	if d := chars[0].Descriptors; len(d) != 1 || d[0].Handle != 7 || d[0].UUID != Short(0x2902) {
		t.Errorf("descriptors = %+v", d)
	}
	if high, low := Short(
		0x2902,
	).Halves(); high != 0x0000290200001000 ||
		low != 0x800000805f9b34fb {
		t.Errorf("halves = %016x %016x", high, low)
	}

	value, err := c.Read(ctx, 6)
	if err != nil || !bytes.Equal(value, p.find(6).value) {
		t.Fatalf("read = %q, %v", value, err)
	}

	long := bytes.Repeat([]byte{0xab}, 150)
	if err := c.Write(ctx, 9, long, true); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.written[9], long) {
		t.Errorf("long write = %x", p.written[9])
	}

	got := make(chan []byte, 2)
	c.OnNotify(func(handle uint16, data []byte) {
		if handle == 6 {
			got <- data
		}
	})
	if err := c.Write(ctx, 7, []byte{0x01, 0x00}, true); err != nil {
		t.Fatal(err)
	}
	<-p.cccd
	p.send(append([]byte{attNotification}, append(u16(6), "tick"...)...))
	p.send(append([]byte{attIndication}, append(u16(6), "tock"...)...))
	for _, want := range []string{"tick", "tock"} {
		select {
		case data := <-got:
			if string(data) != want {
				t.Errorf("heard %q, want %q", data, want)
			}
		case <-ctx.Done():
			t.Fatalf("never heard %q", want)
		}
	}

	if err := c.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(ctx, 3); !errors.Is(err, ErrClosed) {
		t.Errorf("read on a closed link: %v, want ErrClosed", err)
	}
}

func TestAWriteWithoutResponseMustFitOnePDU(t *testing.T) {
	c := New(newPeripheral(t))
	err := c.Write(context.Background(), 9, make([]byte, attDefaultMTU-2), false)
	if err == nil {
		t.Fatal("an oversized write without response went out")
	}
}

func TestTheClientRefusesRequestsToItsEmptyServer(t *testing.T) {
	p := &peripheral{
		t:        t,
		handlers: map[uint16]func([]byte){},
		done:     make(chan struct{}),
		in:       make(chan []byte, 1),
	}
	c := New(p)

	go c.receive([]byte{attReadReq, 0x05, 0x00})
	select {
	case pdu := <-p.in:
		want := []byte{attErrorRsp, attReadReq, 0x05, 0x00, errInvalidHandle}
		if !bytes.Equal(pdu, want) {
			t.Fatalf("answered % x, want % x", pdu, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the request went unanswered")
	}
}
