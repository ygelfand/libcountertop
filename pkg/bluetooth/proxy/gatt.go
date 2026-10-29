package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/ygelfand/go-esphome-device/api"
	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/libcountertop/pkg/bluetooth/gatt"
	"github.com/ygelfand/libcountertop/pkg/bluetooth/hci"
	"github.com/ygelfand/libcountertop/pkg/bluetooth/smp"
)

// ESPHome's default on an ESP32.
const connectionSlots = 3

const (
	connectAttempts = 3
	connectTimeout  = 20 * time.Second
	secureTimeout   = 30 * time.Second
	opsQueued       = 256
	notesQueued     = 256
)

// ESPHome reports an HCI reason where one exists, else an ESP-IDF code.
const (
	errGATT         = 0x85
	errConnCancel   = 0x0100
	errNoConnection = 0x0101
	errNoSlot       = 0x09
	errNotConnected = -1
)

type Connector interface {
	Connect(ctx context.Context, address [6]byte, addressType byte) (*hci.Link, error)
	Pause() func()
	Address() ([6]byte, error)
}

type sender interface {
	Send(proto.Message) error
}

type link interface {
	gatt.Link
	smp.Link
	Reason() byte
	Update(minInterval, maxInterval, latency, timeout uint16) error
}

type connections struct {
	connect func(ctx context.Context, address [6]byte, addressType byte) (link, error)
	pause   func() func()
	local   func() ([6]byte, error)
	bonds   *smp.Bonds

	mu    sync.Mutex
	slots map[uint64]*slot
	free  sender
}

type slot struct {
	api    sender
	cancel context.CancelFunc
	link   link
	client *gatt.Client
	pairer *smp.Pairer
	notify map[uint16]bool
	ops    chan func()
}

func newConnections(c Connector, bonds string) *connections {
	return &connections{
		connect: func(ctx context.Context, address [6]byte, addressType byte) (link, error) {
			l, err := c.Connect(ctx, address, addressType)
			if err != nil {
				return nil, err
			}
			return l, nil
		},
		pause: c.Pause,
		local: c.Address,
		bonds: smp.NewBonds(bonds),
		slots: map[uint64]*slot{},
	}
}

func (g *connections) handle(conn sender, msg proto.Message) (bool, error) {
	switch m := msg.(type) {
	case *api.SubscribeBluetoothConnectionsFreeRequest:
		g.mu.Lock()
		g.free = conn
		g.mu.Unlock()
		return true, conn.Send(g.freeResponse())

	case *api.BluetoothDeviceRequest:
		return true, g.device(conn, m)

	case *api.BluetoothGATTGetServicesRequest:
		g.queue(m.GetAddress(), func() { g.services(conn, m.GetAddress()) })
	case *api.BluetoothGATTReadRequest:
		g.queue(m.GetAddress(), func() { g.read(conn, m.GetAddress(), m.GetHandle()) })
	case *api.BluetoothGATTReadDescriptorRequest:
		g.queue(m.GetAddress(), func() { g.read(conn, m.GetAddress(), m.GetHandle()) })
	case *api.BluetoothGATTWriteRequest:
		g.queue(m.GetAddress(), func() {
			g.write(conn, m.GetAddress(), m.GetHandle(), m.GetData(), m.GetResponse())
		})
	case *api.BluetoothGATTWriteDescriptorRequest:
		g.queue(
			m.GetAddress(),
			func() { g.write(conn, m.GetAddress(), m.GetHandle(), m.GetData(), true) },
		)
	case *api.BluetoothGATTNotifyRequest:
		return true, g.subscribe(conn, m.GetAddress(), m.GetHandle(), m.GetEnable())
	case *api.BluetoothSetConnectionParamsRequest:
		return true, g.timing(conn, m)
	default:
		return false, nil
	}
	return true, nil
}

func (g *connections) queue(address uint64, op func()) {
	g.mu.Lock()
	var ops chan func()
	if s := g.slots[address]; s != nil {
		ops = s.ops
	}
	g.mu.Unlock()
	if ops == nil {
		go op()
		return
	}
	ops <- op
}

func (g *connections) device(conn sender, m *api.BluetoothDeviceRequest) error {
	address := m.GetAddress()
	switch m.GetRequestType() {
	case api.BluetoothDeviceRequestType_BLUETOOTH_DEVICE_REQUEST_TYPE_CONNECT_V3_WITH_CACHE,
		api.BluetoothDeviceRequestType_BLUETOOTH_DEVICE_REQUEST_TYPE_CONNECT_V3_WITHOUT_CACHE:
		return g.open(conn, address, byte(m.GetAddressType()))

	case api.BluetoothDeviceRequestType_BLUETOOTH_DEVICE_REQUEST_TYPE_DISCONNECT:
		g.mu.Lock()
		s := g.slots[address]
		var l link
		if s != nil {
			l = s.link
		}
		g.mu.Unlock()
		if s == nil {
			return conn.Send(
				&api.BluetoothDeviceConnectionResponse{Address: address, Error: errNoConnection},
			)
		}
		s.cancel()
		if l != nil {
			go func() { _ = l.Disconnect() }()
		}
		return nil

	case api.BluetoothDeviceRequestType_BLUETOOTH_DEVICE_REQUEST_TYPE_PAIR:
		g.queue(address, func() { g.pair(conn, address) })
		return nil

	case api.BluetoothDeviceRequestType_BLUETOOTH_DEVICE_REQUEST_TYPE_UNPAIR:
		if err := g.bonds.Remove(address); err != nil {
			return conn.Send(
				&api.BluetoothDeviceUnpairingResponse{Address: address, Error: errGATT},
			)
		}
		return conn.Send(&api.BluetoothDeviceUnpairingResponse{Address: address, Success: true})

	case api.BluetoothDeviceRequestType_BLUETOOTH_DEVICE_REQUEST_TYPE_CLEAR_CACHE:
		return conn.Send(&api.BluetoothDeviceClearCacheResponse{Address: address, Success: true})
	}
	return nil
}

func (g *connections) open(conn sender, address uint64, addressType byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	g.mu.Lock()
	if _, busy := g.slots[address]; busy || len(g.slots) >= connectionSlots {
		g.mu.Unlock()
		cancel()
		code := int32(errNoSlot)
		if busy {
			code = errConnCancel
		}
		return conn.Send(&api.BluetoothDeviceConnectionResponse{Address: address, Error: code})
	}
	s := &slot{api: conn, cancel: cancel, notify: map[uint16]bool{}}
	g.slots[address] = s
	g.mu.Unlock()
	g.announceFree()

	go g.dial(ctx, s, address, addressType)
	return nil
}

func (g *connections) dial(ctx context.Context, s *slot, address uint64, addressType byte) {
	defer s.cancel()

	l, client, err := g.establish(ctx, address, addressType)
	if err != nil {
		code := int32(errGATT)
		if status, ok := errors.AsType[hci.Status](err); ok {
			code = int32(status)
		} else if errors.Is(err, context.Canceled) {
			code = errConnCancel
		}
		slog.Info("ble proxy connection failed", "address", mac(address), "err", err)
		_ = s.api.Send(&api.BluetoothDeviceConnectionResponse{Address: address, Error: code})
		g.release(address, s)
		return
	}

	local, err := g.local()
	if err != nil {
		slog.Warn("ble proxy could not read the controller address", "err", err)
	}
	pairer := smp.New(
		l,
		smp.Address{Addr: local},
		smp.Address{Addr: macBytes(address), Type: addressType},
	)
	pairer.OnSecurityRequest(func() {
		if err := g.secure(pairer, address); err != nil {
			slog.Info(
				"ble proxy could not secure the link the device asked for",
				"address",
				mac(address),
				"err",
				err,
			)
		}
	})

	notes := make(chan *api.BluetoothGATTNotifyDataResponse, notesQueued)
	go func() {
		for {
			select {
			case n := <-notes:
				_ = s.api.Send(n)
			case <-l.Done():
				return
			}
		}
	}()
	client.OnNotify(func(handle uint16, data []byte) {
		g.mu.Lock()
		on := s.notify[handle]
		g.mu.Unlock()
		if !on {
			return
		}
		select {
		case notes <- &api.BluetoothGATTNotifyDataResponse{Address: address, Handle: uint32(handle), Data: data}:
		default:
			slog.Debug(
				"ble notification dropped, Home Assistant is behind",
				"address",
				mac(address),
				"handle",
				handle,
			)
		}
	})

	g.mu.Lock()
	s.link, s.client, s.pairer = l, client, pairer
	s.ops = make(chan func(), opsQueued)
	go work(s.ops, l.Done())
	cancelled := errors.Is(ctx.Err(), context.Canceled)
	g.mu.Unlock()

	switch {
	case cancelled:
		_ = l.Disconnect()
	default:
		if err := s.api.Send(&api.BluetoothDeviceConnectionResponse{
			Address:   address,
			Connected: true,
			Mtu:       uint32(client.MTU()),
		}); err != nil {
			_ = l.Disconnect()
		}
	}

	<-l.Done()
	// Home Assistant warns about a stale connection when the slot frees before the link's end is reported.
	_ = s.api.Send(
		&api.BluetoothDeviceConnectionResponse{Address: address, Error: int32(l.Reason())},
	)
	g.release(address, s)
}

func (g *connections) establish(
	ctx context.Context,
	address uint64,
	addressType byte,
) (link, *gatt.Client, error) {
	resume := g.pause()
	defer resume()

	l, err := g.connect(ctx, macBytes(address), addressType)
	for attempt := 1; attempt < connectAttempts && ctx.Err() == nil; attempt++ {
		if status, ok := errors.AsType[hci.Status](
			err,
		); !ok ||
			status != hci.StatusFailedToEstablish {
			break
		}
		l, err = g.connect(ctx, macBytes(address), addressType)
	}
	if err != nil {
		return nil, nil, err
	}

	client := gatt.New(l)
	if _, err := client.ExchangeMTU(ctx); err != nil {
		_ = l.Disconnect()
		return nil, nil, err
	}
	return l, client, nil
}

func (g *connections) release(address uint64, s *slot) {
	g.mu.Lock()
	if g.slots[address] == s {
		delete(g.slots, address)
	}
	g.mu.Unlock()
	g.announceFree()
}

func (g *connections) up(address uint64) *slot {
	g.mu.Lock()
	defer g.mu.Unlock()
	if s := g.slots[address]; s != nil && s.link != nil {
		return s
	}
	return nil
}

func (g *connections) pair(conn sender, address uint64) {
	s := g.up(address)
	if s == nil {
		_ = conn.Send(&api.BluetoothDevicePairingResponse{Address: address, Error: errNoConnection})
		return
	}
	if err := g.secure(s.pairer, address); err != nil {
		slog.Info("ble proxy pairing failed", "address", mac(address), "err", err)
		_ = conn.Send(
			&api.BluetoothDevicePairingResponse{Address: address, Error: pairingError(err)},
		)
		return
	}
	_ = conn.Send(&api.BluetoothDevicePairingResponse{Address: address, Paired: true})
}

func (g *connections) secure(p *smp.Pairer, address uint64) error {
	ctx, cancel := context.WithTimeout(context.Background(), secureTimeout)
	defer cancel()

	if k, ok := g.bonds.Get(address); ok {
		err := p.Encrypt(ctx, k)
		if err == nil {
			return nil
		}
		if !bondRejected(err) {
			return err
		}
		slog.Info("ble bond not accepted, pairing again", "address", mac(address), "err", err)
		_ = g.bonds.Remove(address)
	}
	k, err := p.Pair(ctx)
	if err != nil {
		return err
	}
	if err := g.bonds.Put(address, k); err != nil {
		slog.Warn("ble keys could not be saved", "address", mac(address), "err", err)
	}
	return nil
}

func (g *connections) services(conn sender, address uint64) {
	s := g.up(address)
	if s == nil {
		_ = conn.Send(&api.BluetoothGATTErrorResponse{Address: address, Error: errNoConnection})
		return
	}
	services, err := s.client.Discover(context.Background())
	if err != nil {
		_ = conn.Send(&api.BluetoothGATTErrorResponse{Address: address, Error: gattError(err)})
		return
	}
	for _, sv := range services {
		err := conn.Send(&api.BluetoothGATTGetServicesResponse{
			Address:  address,
			Services: []*api.BluetoothGATTService{service(sv)},
		})
		if err != nil {
			return
		}
	}
	_ = conn.Send(&api.BluetoothGATTGetServicesDoneResponse{Address: address})
}

func (g *connections) read(conn sender, address uint64, handle uint32) {
	s := g.up(address)
	if s == nil {
		_ = conn.Send(
			&api.BluetoothGATTErrorResponse{
				Address: address,
				Handle:  handle,
				Error:   errNoConnection,
			},
		)
		return
	}
	data, err := s.client.Read(context.Background(), uint16(handle))
	if err != nil {
		_ = conn.Send(
			&api.BluetoothGATTErrorResponse{
				Address: address,
				Handle:  handle,
				Error:   gattError(err),
			},
		)
		return
	}
	_ = conn.Send(&api.BluetoothGATTReadResponse{Address: address, Handle: handle, Data: data})
}

// ESPHome confirms every write, with response or without, and Home Assistant waits for it.
func (g *connections) write(
	conn sender,
	address uint64,
	handle uint32,
	data []byte,
	response bool,
) {
	s := g.up(address)
	if s == nil {
		_ = conn.Send(
			&api.BluetoothGATTErrorResponse{
				Address: address,
				Handle:  handle,
				Error:   errNoConnection,
			},
		)
		return
	}
	if err := s.client.Write(context.Background(), uint16(handle), data, response); err != nil {
		_ = conn.Send(
			&api.BluetoothGATTErrorResponse{
				Address: address,
				Handle:  handle,
				Error:   gattError(err),
			},
		)
		return
	}
	_ = conn.Send(&api.BluetoothGATTWriteResponse{Address: address, Handle: handle})
}

func (g *connections) subscribe(conn sender, address uint64, handle uint32, enable bool) error {
	s := g.up(address)
	if s == nil {
		return conn.Send(
			&api.BluetoothGATTErrorResponse{
				Address: address,
				Handle:  handle,
				Error:   errNoConnection,
			},
		)
	}
	g.mu.Lock()
	if enable {
		s.notify[uint16(handle)] = true
	} else {
		delete(s.notify, uint16(handle))
	}
	g.mu.Unlock()
	return conn.Send(&api.BluetoothGATTNotifyResponse{Address: address, Handle: handle})
}

func (g *connections) timing(conn sender, m *api.BluetoothSetConnectionParamsRequest) error {
	address := m.GetAddress()
	s := g.up(address)
	if s == nil {
		return conn.Send(
			&api.BluetoothSetConnectionParamsResponse{Address: address, Error: errNotConnected},
		)
	}
	short := func(v uint32) uint16 { return uint16(min(v, math.MaxUint16)) }
	err := s.link.Update(
		short(m.GetMinInterval()),
		short(m.GetMaxInterval()),
		short(m.GetLatency()),
		short(m.GetTimeout()),
	)
	return conn.Send(
		&api.BluetoothSetConnectionParamsResponse{Address: address, Error: timingError(err)},
	)
}

func timingError(err error) int32 {
	switch {
	case err == nil:
		return 0
	case closed(err):
		return errNotConnected
	}
	if status, ok := errors.AsType[hci.Status](err); ok {
		return int32(status)
	}
	return errGATT
}

func (g *connections) closeAll() {
	g.mu.Lock()
	type held struct {
		cancel context.CancelFunc
		link   link
	}
	all := make([]held, 0, len(g.slots))
	for _, s := range g.slots {
		all = append(all, held{cancel: s.cancel, link: s.link})
	}
	g.mu.Unlock()
	for _, h := range all {
		h.cancel()
		if h.link != nil {
			_ = h.link.Disconnect()
		}
	}
}

func (g *connections) freeResponse() *api.BluetoothConnectionsFreeResponse {
	g.mu.Lock()
	defer g.mu.Unlock()
	r := &api.BluetoothConnectionsFreeResponse{
		Free:  uint32(connectionSlots - len(g.slots)),
		Limit: connectionSlots,
	}
	for address := range g.slots {
		r.Allocated = append(r.Allocated, address)
	}
	return r
}

func (g *connections) announceFree() {
	g.mu.Lock()
	conn := g.free
	g.mu.Unlock()
	if conn == nil {
		return
	}
	if err := conn.Send(g.freeResponse()); err != nil {
		g.mu.Lock()
		if g.free == conn {
			g.free = nil
		}
		g.mu.Unlock()
	}
}

func work(ops chan func(), done <-chan struct{}) {
	for {
		select {
		case op := <-ops:
			op()
		case <-done:
			for {
				select {
				case op := <-ops:
					op()
				default:
					return
				}
			}
		}
	}
}

func bondRejected(err error) bool {
	status, ok := errors.AsType[hci.Status](err)
	return ok && status == hci.StatusKeyMissing
}

func closed(err error) bool {
	return errors.Is(err, hci.ErrClosed) || errors.Is(err, gatt.ErrClosed) ||
		errors.Is(err, smp.ErrClosed)
}

func pairingError(err error) int32 {
	if e, ok := errors.AsType[*smp.Error](err); ok {
		return int32(e.Reason)
	}
	if status, ok := errors.AsType[hci.Status](err); ok {
		return int32(status)
	}
	if closed(err) {
		return errNoConnection
	}
	return errGATT
}

func gattError(err error) int32 {
	if e, ok := errors.AsType[*gatt.Error](err); ok {
		return int32(e.Code)
	}
	if closed(err) {
		return errNoConnection
	}
	return errGATT
}

func service(s gatt.Service) *api.BluetoothGATTService {
	out := &api.BluetoothGATTService{Uuid: halves(s.UUID), Handle: uint32(s.Handle)}
	for _, c := range s.Characteristics {
		ch := &api.BluetoothGATTCharacteristic{
			Uuid:       halves(c.UUID),
			Handle:     uint32(c.Handle),
			Properties: uint32(c.Properties),
		}
		for _, d := range c.Descriptors {
			ch.Descriptors = append(ch.Descriptors, &api.BluetoothGATTDescriptor{
				Uuid:   halves(d.UUID),
				Handle: uint32(d.Handle),
			})
		}
		out.Characteristics = append(out.Characteristics, ch)
	}
	return out
}

func halves(u gatt.UUID) []uint64 {
	high, low := u.Halves()
	return []uint64{high, low}
}

// ESPHome carries an address as a big-endian integer.
func macBytes(address uint64) [6]byte {
	var b [6]byte
	for i := range 6 {
		b[i] = byte(address >> (8 * (5 - i)))
	}
	return b
}

func mac(address uint64) string { return fmt.Sprintf("%012X", address) }
