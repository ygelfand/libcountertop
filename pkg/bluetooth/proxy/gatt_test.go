package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ygelfand/go-esphome-device/api"
	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/libcountertop/pkg/bluetooth/gatt"
	"github.com/ygelfand/libcountertop/pkg/bluetooth/hci"
	"github.com/ygelfand/libcountertop/pkg/bluetooth/smp"
)

type homeAssistant struct {
	got chan proto.Message
}

func (h *homeAssistant) Send(m proto.Message) error {
	h.got <- m
	return nil
}

func (h *homeAssistant) next(t *testing.T) proto.Message {
	t.Helper()
	select {
	case m := <-h.got:
		return m
	case <-time.After(2 * time.Second):
		t.Fatal("Home Assistant heard nothing")
		return nil
	}
}

func (h *homeAssistant) expect(t *testing.T, want proto.Message) {
	t.Helper()
	for {
		m := h.next(t)
		if _, ok := m.(*api.BluetoothConnectionsFreeResponse); ok {
			if _, wanted := want.(*api.BluetoothConnectionsFreeResponse); !wanted {
				continue
			}
		}
		if !proto.Equal(m, want) {
			t.Fatalf("Home Assistant heard %v, want %v", m, want)
		}
		return
	}
}

type device struct {
	mu       sync.Mutex
	handlers map[uint16]func([]byte)
	done     chan struct{}
	closed   sync.Once
	reason   byte
	value    []byte
	writes   [][]byte
	pairing  chan struct{}
	timing   [4]uint16
}

func newDevice() *device {
	return &device{
		handlers: map[uint16]func([]byte){},
		done:     make(chan struct{}),
		value:    []byte("Fake"),
		pairing:  make(chan struct{}, 1),
	}
}

func (d *device) Send(ctx context.Context, cid uint16, pdu []byte) error {
	select {
	case <-d.done:
		return hci.ErrClosed
	default:
	}
	if cid == smp.CID {
		select {
		case d.pairing <- struct{}{}:
		default:
		}
		return nil
	}
	var reply []byte
	switch pdu[0] {
	case 0x02:
		reply = []byte{0x03, 64, 0}
	case 0x0a:
		reply = append([]byte{0x0b}, d.value...)
	case 0x12:
		reply = []byte{0x13}
	case 0x52:
		d.mu.Lock()
		d.writes = append(d.writes, append([]byte(nil), pdu[3:]...))
		d.mu.Unlock()
		return nil
	}
	go d.say(reply)
	return nil
}

func (d *device) say(pdu []byte) {
	d.mu.Lock()
	f := d.handlers[gatt.CID]
	d.mu.Unlock()
	if f != nil {
		f(pdu)
	}
}

func (d *device) On(cid uint16, f func([]byte)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[cid] = f
}

func (d *device) Done() <-chan struct{} { return d.done }

func (d *device) Disconnect() error {
	d.drop(0x16)
	return nil
}

func (d *device) drop(reason byte) {
	d.closed.Do(func() {
		d.reason = reason
		close(d.done)
	})
}

func (d *device) Encrypt(ctx context.Context, ltk [16]byte, ediv uint16, random [8]byte) error {
	return hci.StatusKeyMissing
}

func (d *device) Reason() byte {
	<-d.done
	return d.reason
}

func (d *device) Update(minInterval, maxInterval, latency, timeout uint16) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.timing = [4]uint16{minInterval, maxInterval, latency, timeout}
	if minInterval > maxInterval {
		return hci.Status(0x12)
	}
	return nil
}

type radio struct {
	mu       sync.Mutex
	attempts int
	refuse   int
	paused   int
	resumed  int
	device   *device
}

func (r *radio) connections(t *testing.T) *connections {
	return &connections{
		connect: func(ctx context.Context, address [6]byte, addressType byte) (link, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.attempts++
			if r.paused == r.resumed {
				t.Error("connecting while the scan runs")
			}
			if r.attempts <= r.refuse {
				return nil, hci.StatusFailedToEstablish
			}
			return r.device, nil
		},
		pause: func() func() {
			r.mu.Lock()
			r.paused++
			r.mu.Unlock()
			return func() {
				r.mu.Lock()
				r.resumed++
				r.mu.Unlock()
			}
		},
		local: func() ([6]byte, error) { return [6]byte{1, 2, 3, 4, 5, 6}, nil },
		bonds: smp.NewBonds(t.TempDir() + "/bonds.json"),
		slots: map[uint64]*slot{},
	}
}

const peer = 0x685edd16f8e2

func connected(t *testing.T, g *connections, ha *homeAssistant) {
	t.Helper()
	handled, err := g.handle(ha, &api.BluetoothDeviceRequest{
		Address:     peer,
		AddressType: 1,
		RequestType: api.BluetoothDeviceRequestType_BLUETOOTH_DEVICE_REQUEST_TYPE_CONNECT_V3_WITH_CACHE,
	})
	if !handled || err != nil {
		t.Fatalf("connect handled %v, %v", handled, err)
	}
	ha.expect(t, &api.BluetoothDeviceConnectionResponse{Address: peer, Connected: true, Mtu: 64})
}

func TestAProxiedLinkReadsWritesInOrderAndReportsItsEnd(t *testing.T) {
	r := &radio{device: newDevice(), refuse: 2}
	g := r.connections(t)
	ha := &homeAssistant{got: make(chan proto.Message, 64)}

	connected(t, g, ha)
	if r.attempts != 3 || r.paused != 1 || r.resumed != 1 {
		t.Fatalf(
			"attempts %d, paused %d, resumed %d; want 3, 1, 1",
			r.attempts,
			r.paused,
			r.resumed,
		)
	}

	_, _ = g.handle(ha, &api.BluetoothGATTReadRequest{Address: peer, Handle: 3})
	ha.expect(t, &api.BluetoothGATTReadResponse{Address: peer, Handle: 3, Data: []byte("Fake")})

	for i := range 50 {
		_, _ = g.handle(
			ha,
			&api.BluetoothGATTWriteRequest{Address: peer, Handle: 9, Data: []byte{byte(i)}},
		)
	}
	for range 50 {
		ha.expect(t, &api.BluetoothGATTWriteResponse{Address: peer, Handle: 9})
	}
	r.device.mu.Lock()
	for i, w := range r.device.writes {
		if len(w) != 1 || w[0] != byte(i) {
			t.Fatalf("write %d carried % x", i, w)
		}
	}
	r.device.mu.Unlock()

	_, _ = g.handle(ha, &api.BluetoothGATTNotifyRequest{Address: peer, Handle: 6, Enable: true})
	ha.expect(t, &api.BluetoothGATTNotifyResponse{Address: peer, Handle: 6})
	r.device.say(append([]byte{0x1b}, binary.LittleEndian.AppendUint16(nil, 7)...))
	for i := range 3 {
		r.device.say(append([]byte{0x1b, 6, 0}, byte(i)))
	}
	for i := range 3 {
		ha.expect(
			t,
			&api.BluetoothGATTNotifyDataResponse{Address: peer, Handle: 6, Data: []byte{byte(i)}},
		)
	}

	r.device.drop(0x13)
	ha.expect(t, &api.BluetoothDeviceConnectionResponse{Address: peer, Error: 0x13})
	deadline := time.Now().Add(2 * time.Second)
	for g.freeResponse().GetFree() != connectionSlots {
		if time.Now().After(deadline) {
			t.Fatal("the slot was never freed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, _ = g.handle(ha, &api.BluetoothGATTReadRequest{Address: peer, Handle: 3})
	ha.expect(t, &api.BluetoothGATTErrorResponse{Address: peer, Handle: 3, Error: errNoConnection})
}

func TestHomeAssistantCanRetimeALink(t *testing.T) {
	r := &radio{device: newDevice()}
	g := r.connections(t)
	ha := &homeAssistant{got: make(chan proto.Message, 64)}

	ask := func(min, max uint32) {
		_, _ = g.handle(ha, &api.BluetoothSetConnectionParamsRequest{
			Address: peer, MinInterval: min, MaxInterval: max, Latency: 0, Timeout: 70000,
		})
	}
	ask(6, 12)
	ha.expect(t, &api.BluetoothSetConnectionParamsResponse{Address: peer, Error: errNotConnected})

	connected(t, g, ha)
	ask(6, 12)
	ha.expect(t, &api.BluetoothSetConnectionParamsResponse{Address: peer})
	r.device.mu.Lock()
	got := r.device.timing
	r.device.mu.Unlock()
	if got != [4]uint16{6, 12, 0, 0xffff} {
		t.Fatalf("link was asked for %v, want 6 12 0 and the timeout clamped to 0xffff", got)
	}

	ask(12, 6)
	ha.expect(t, &api.BluetoothSetConnectionParamsResponse{Address: peer, Error: 0x12})
}

func TestAProxyHoldsOnlyItsSlots(t *testing.T) {
	r := &radio{device: newDevice()}
	g := r.connections(t)
	ha := &homeAssistant{got: make(chan proto.Message, 64)}
	for address := range uint64(connectionSlots) {
		g.slots[address+1] = &slot{}
	}
	_, _ = g.handle(ha, &api.BluetoothDeviceRequest{
		Address:     peer,
		RequestType: api.BluetoothDeviceRequestType_BLUETOOTH_DEVICE_REQUEST_TYPE_CONNECT_V3_WITH_CACHE,
	})
	ha.expect(t, &api.BluetoothDeviceConnectionResponse{Address: peer, Error: errNoSlot})
}

func TestADeviceThatLostTheBondIsPairedAgain(t *testing.T) {
	r := &radio{device: newDevice()}
	g := r.connections(t)
	ha := &homeAssistant{got: make(chan proto.Message, 64)}
	connected(t, g, ha)

	if err := g.bonds.Put(peer, smp.Keys{LTK: [16]byte{1}, Size: 16}); err != nil {
		t.Fatal(err)
	}
	_, _ = g.handle(ha, &api.BluetoothDeviceRequest{
		Address:     peer,
		RequestType: api.BluetoothDeviceRequestType_BLUETOOTH_DEVICE_REQUEST_TYPE_PAIR,
	})
	select {
	case <-r.device.pairing:
	case <-time.After(2 * time.Second):
		t.Fatal("a refused bond did not lead to pairing")
	}
	r.device.drop(0x13)
	for {
		m := ha.next(t)
		if p, ok := m.(*api.BluetoothDevicePairingResponse); ok {
			if p.GetPaired() || p.GetError() == 0 {
				t.Fatalf("pairing over a dropped link answered %v", p)
			}
			break
		}
	}
	if _, ok := g.bonds.Get(peer); ok {
		t.Fatal("a bond the device refused is still kept")
	}
}

func TestErrorsReachHomeAssistantAsESPHomeCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		gatt int32
		pair int32
	}{
		{&gatt.Error{Code: 0x02}, 0x02, errGATT},
		{&smp.Error{Reason: 0x05, Remote: true}, errGATT, 0x05},
		{fmt.Errorf("starting: %w", hci.StatusKeyMissing), errGATT, 0x06},
		{gatt.ErrClosed, errNoConnection, errNoConnection},
		{hci.ErrClosed, errNoConnection, errNoConnection},
		{errors.New("timeout"), errGATT, errGATT},
	} {
		if got := gattError(tc.err); got != tc.gatt {
			t.Errorf("gattError(%v) = %#x, want %#x", tc.err, got, tc.gatt)
		}
		if got := pairingError(tc.err); got != tc.pair {
			t.Errorf("pairingError(%v) = %#x, want %#x", tc.err, got, tc.pair)
		}
	}
	if !bondRejected(fmt.Errorf("encrypting: %w", hci.StatusKeyMissing)) ||
		bondRejected(context.DeadlineExceeded) {
		t.Error("only a missing key means the bond is gone")
	}
}

func TestServicesTravelAsESPHomeDescribesThem(t *testing.T) {
	m := service(gatt.Service{
		UUID:   gatt.Short(0x180a),
		Handle: 10,
		Characteristics: []gatt.Characteristic{{
			UUID:        gatt.Short(0x2a29),
			Handle:      12,
			Properties:  0x12,
			Descriptors: []gatt.Descriptor{{UUID: gatt.Short(0x2902), Handle: 13}},
		}},
	})
	if m.GetHandle() != 10 || m.GetUuid()[0] != 0x0000180a00001000 ||
		m.GetUuid()[1] != 0x800000805f9b34fb {
		t.Fatalf("service = %v", m)
	}
	c := m.GetCharacteristics()[0]
	if c.GetHandle() != 12 || c.GetProperties() != 0x12 || c.GetDescriptors()[0].GetHandle() != 13 {
		t.Errorf("characteristic = %v", c)
	}
	if macBytes(peer) != [6]byte{0x68, 0x5e, 0xdd, 0x16, 0xf8, 0xe2} {
		t.Errorf("macBytes = %x", macBytes(peer))
	}
}
