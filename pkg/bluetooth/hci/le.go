package hci

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/bluetooth/pair"
)

const (
	hciLESetEventMask     = 0x2001
	hciDisconnect         = 0x0406
	hciLECreateConnection = 0x200d
	hciLECreateCancel     = 0x200e
	hciLEConnectionUpdate = 0x2013
	hciLEEnableEncryption = 0x2019
	hciLELTKNegativeReply = 0x201b
	hciLERemoteParamReply = 0x2020

	leLongTermKeyRequest     = 0x05
	leRemoteConnParamRequest = 0x06

	cidSignaling = 0x0005
)

// Initiator scan in 0.625 ms units, intervals in 1.25 ms, supervision in 10 ms.
const (
	initScanInterval   = 0x0060
	initScanWindow     = 0x0030
	connIntervalMin    = 24
	connIntervalMax    = 40
	supervisionTimeout = 400
)

const (
	StatusUnknownConnection Status = 0x02
	StatusKeyMissing        Status = 0x06
	StatusFailedToEstablish Status = 0x3e
	reasonRemoteUser               = 0x13
	reasonLocalHost                = 0x16
	inboxDepth                     = 256
	heldDepth                      = 16
	cancelled                      = 2 * time.Second
	leaving                        = 3 * time.Second
)

var ErrClosed = errors.New("ble: the link is closed")

// Connection complete, advertising report, connection update complete, remote features, LTK request.
var leEventMask = [8]byte{0x1f}

type Status byte

func (s Status) Error() string { return fmt.Sprintf("ble: status %#02x", byte(s)) }

type leSide struct {
	once    sync.Once
	mu      sync.Mutex
	links   map[Handle]*Link
	dialing chan dialed
	dial    sync.Mutex
}

type dialed struct {
	status byte
	link   *Link
}

type inbound struct {
	cid  uint16
	data []byte
}

type Link struct {
	s           *Session
	Handle      Handle
	Address     [6]byte
	AddressType byte

	mu       sync.Mutex
	handlers map[uint16]func([]byte)
	held     map[uint16][][]byte
	inbox    chan inbound
	unheld   chan struct{}
	crypt    chan byte

	life   context.Context
	end    context.CancelFunc
	closed sync.Once
	reason byte
}

func (s *Session) Start(opcode uint16, params ...byte) error {
	cmd, err := command(opcode, params...)
	if err != nil {
		return err
	}
	e, err := s.Ask(cmd, answers)
	if err != nil {
		return err
	}
	if e.Code == eventCommandStatus {
		if len(e.Params) < 1 {
			return errShort
		}
		if e.Params[0] != 0 {
			return Status(e.Params[0])
		}
		return nil
	}
	_, err = complete(e, opcode)
	return err
}

func (s *Session) Connect(ctx context.Context, address [6]byte, addressType byte) (*Link, error) {
	s.leStart()

	s.le.dial.Lock()
	defer s.le.dial.Unlock()

	wait := make(chan dialed, 1)
	s.le.mu.Lock()
	s.le.dialing = wait
	s.le.mu.Unlock()
	defer func() {
		s.le.mu.Lock()
		if s.le.dialing == wait {
			s.le.dialing = nil
		}
		s.le.mu.Unlock()
	}()

	params := make([]byte, 25)
	binary.LittleEndian.PutUint16(params[0:], initScanInterval)
	binary.LittleEndian.PutUint16(params[2:], initScanWindow)
	params[5] = addressType
	for i := range 6 {
		params[6+i] = address[5-i]
	}
	binary.LittleEndian.PutUint16(params[13:], connIntervalMin)
	binary.LittleEndian.PutUint16(params[15:], connIntervalMax)
	binary.LittleEndian.PutUint16(params[19:], supervisionTimeout)

	if err := s.Start(hciLECreateConnection, params...); err != nil {
		return nil, fmt.Errorf("ble: connecting: %w", err)
	}

	var got dialed
	select {
	case got = <-wait:
	case <-ctx.Done():
		if _, err := s.Command(hciLECreateCancel); err != nil {
			slog.Debug("ble connection cancel failed", "err", err)
		}
		select {
		case got = <-wait:
		case <-time.After(cancelled):
			return nil, fmt.Errorf("ble: connecting: %w", ctx.Err())
		}
		if got.status == byte(StatusUnknownConnection) {
			return nil, fmt.Errorf("ble: connecting: %w", ctx.Err())
		}
	}

	if got.status != 0 {
		return nil, Status(got.status)
	}
	return got.link, nil
}

func (s *Session) leStart() {
	s.le.once.Do(func() {
		s.le.mu.Lock()
		s.le.links = map[Handle]*Link{}
		s.le.mu.Unlock()

		if s.quirks&ControllerSetsParams != 0 {
			if _, err := s.Command(hciLESetEventMask, leEventMask[:]...); err != nil {
				slog.Warn("ble event mask", "err", err)
			}
		}

		got, _ := s.Take(s.leWants)
		go s.leLoop(got)
	})
}

func (s *Session) leWants(g packet) bool {
	switch g.kind {
	case typeACL:
		return s.onLE(g.acl.Handle)
	case typeEvent:
		if g.event.Code == eventLEMeta {
			if len(g.event.Params) < 1 {
				return false
			}
			switch g.event.Params[0] {
			case leConnectionComplete, leEnhancedConnComplete,
				leLongTermKeyRequest, leRemoteConnParamRequest:
				return true
			}
			return false
		}
		if h, ok := eventHandle(g.event); ok {
			return s.onLE(h)
		}
	}
	return false
}

func (s *Session) leLoop(got <-chan packet) {
	parts := newReassembler()
	for g := range got {
		if g.kind == typeACL {
			s.leData(parts, g.acl)
			continue
		}
		s.leEvent(parts, g.event)
	}

	s.le.mu.Lock()
	links := make([]*Link, 0, len(s.le.links))
	for _, l := range s.le.links {
		links = append(links, l)
	}
	s.le.links = map[Handle]*Link{}
	s.le.mu.Unlock()
	for _, l := range links {
		l.close(reasonLocalHost)
	}
}

func (s *Session) leData(parts *reassembler, a aclPacket) {
	pdu, err := parts.add(a)
	if err != nil {
		slog.Debug("ble data", "handle", a.Handle, "err", err)
		return
	}
	if pdu == nil {
		return
	}
	l := s.link(a.Handle)
	if l == nil {
		return
	}
	cid := binary.LittleEndian.Uint16(pdu[2:])
	data := pdu[l2capHeader:]
	if cid == cidSignaling {
		l.signal(data)
		return
	}
	select {
	case l.inbox <- inbound{cid: cid, data: data}:
	default:
		slog.Warn("ble link is not keeping up, dropping a frame", "handle", a.Handle, "cid", cid)
	}
}

func (s *Session) leEvent(parts *reassembler, e event) {
	switch e.Code {
	case pair.EventDisconnectionComplete:
		if len(e.Params) < 4 {
			return
		}
		h := Handle(binary.LittleEndian.Uint16(e.Params[1:]) & handleMask)
		parts.forget(h)
		s.le.mu.Lock()
		l := s.le.links[h]
		delete(s.le.links, h)
		s.le.mu.Unlock()
		if l != nil {
			l.close(e.Params[3])
		}

	case eventEncryptionChange, eventKeyRefreshDone:
		if len(e.Params) < 3 {
			return
		}
		status := e.Params[0]
		if e.Code == eventEncryptionChange && status == 0 && len(e.Params) >= 4 &&
			e.Params[3] == 0 {
			status = byte(StatusFailedToEstablish)
		}
		if l := s.link(Handle(binary.LittleEndian.Uint16(e.Params[1:]) & handleMask)); l != nil {
			select {
			case l.crypt <- status:
			default:
			}
		}

	case eventLEMeta:
		s.leMeta(e.Params)
	}
}

func (s *Session) leMeta(p []byte) {
	switch p[0] {
	case leConnectionComplete, leEnhancedConnComplete:
		if len(p) < 12 {
			return
		}
		s.connected(p)

	case leLongTermKeyRequest:
		if len(p) >= 3 {
			h := []byte{p[1], p[2]}
			go func() { _, _ = s.Command(hciLELTKNegativeReply, h...) }()
		}

	case leRemoteConnParamRequest:
		if len(p) >= 11 {
			reply := make([]byte, 14)
			copy(reply, p[1:11])
			go func() { _, _ = s.Command(hciLERemoteParamReply, reply...) }()
		}
	}
}

// Status, handle, role, peer address type, peer address least significant octet first.
func (s *Session) connected(p []byte) {
	status := p[1]
	h := Handle(binary.LittleEndian.Uint16(p[2:]) & handleMask)

	var l *Link
	if status == 0 {
		life, end := context.WithCancel(context.Background())
		l = &Link{
			s:           s,
			Handle:      h,
			AddressType: p[5],
			handlers:    map[uint16]func([]byte){},
			held:        map[uint16][][]byte{},
			inbox:       make(chan inbound, inboxDepth),
			unheld:      make(chan struct{}, 1),
			crypt:       make(chan byte, 1),
			life:        life,
			end:         end,
		}
		for i := range 6 {
			l.Address[i] = p[11-i]
		}
	}

	s.le.mu.Lock()
	wait := s.le.dialing
	s.le.dialing = nil
	if l != nil && wait != nil {
		s.le.links[h] = l
	}
	s.le.mu.Unlock()

	switch {
	case wait != nil:
		if l != nil {
			go l.work()
		}
		wait <- dialed{status: status, link: l}
	case l != nil:
		go func() { _ = s.Start(hciDisconnect, byte(h), byte(h>>8), reasonRemoteUser) }()
	}
}

func (s *Session) link(h Handle) *Link {
	s.le.mu.Lock()
	defer s.le.mu.Unlock()
	return s.le.links[h]
}

func (l *Link) On(cid uint16, f func([]byte)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.handlers[cid] = f
	if len(l.held[cid]) > 0 {
		select {
		case l.unheld <- struct{}{}:
		default:
		}
	}
}

func (l *Link) Done() <-chan struct{} { return l.life.Done() }

func (l *Link) Reason() byte {
	<-l.life.Done()
	return l.reason
}

func (l *Link) Addr() uint64 {
	var v uint64
	for _, b := range l.Address {
		v = v<<8 | uint64(b)
	}
	return v
}

func (l *Link) Send(ctx context.Context, cid uint16, payload []byte) error {
	if l.life.Err() != nil {
		return ErrClosed
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(l.life, cancel)
	defer stop()

	pdu := make([]byte, l2capHeader, l2capHeader+len(payload))
	binary.LittleEndian.PutUint16(pdu, uint16(len(payload)))
	binary.LittleEndian.PutUint16(pdu[2:], cid)
	if err := l.s.Send(ctx, l.Handle, append(pdu, payload...)); err != nil {
		if l.life.Err() != nil {
			return ErrClosed
		}
		return err
	}
	return nil
}

// Intervals in 1.25 ms units, latency in connection events, timeout in 10 ms units.
func (l *Link) Update(minInterval, maxInterval, latency, timeout uint16) error {
	if l.life.Err() != nil {
		return ErrClosed
	}
	params := make([]byte, 14)
	binary.LittleEndian.PutUint16(params, uint16(l.Handle))
	binary.LittleEndian.PutUint16(params[2:], minInterval)
	binary.LittleEndian.PutUint16(params[4:], maxInterval)
	binary.LittleEndian.PutUint16(params[6:], latency)
	binary.LittleEndian.PutUint16(params[8:], timeout)
	return l.s.Start(hciLEConnectionUpdate, params...)
}

func (l *Link) Disconnect() error {
	if l.life.Err() != nil {
		return nil
	}
	if err := l.s.Start(
		hciDisconnect,
		byte(l.Handle),
		byte(l.Handle>>8),
		reasonRemoteUser,
	); err != nil {
		return err
	}
	select {
	case <-l.life.Done():
	case <-time.After(leaving):
	}
	return nil
}

func (l *Link) Encrypt(ctx context.Context, ltk [16]byte, ediv uint16, random [8]byte) error {
	select {
	case <-l.crypt:
	default:
	}
	params := make([]byte, 28)
	binary.LittleEndian.PutUint16(params, uint16(l.Handle))
	copy(params[2:], random[:])
	binary.LittleEndian.PutUint16(params[10:], ediv)
	copy(params[12:], ltk[:])
	if err := l.s.Start(hciLEEnableEncryption, params...); err != nil {
		return fmt.Errorf("ble: starting encryption: %w", err)
	}
	select {
	case status := <-l.crypt:
		if status != 0 {
			return Status(status)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-l.life.Done():
		return ErrClosed
	}
}

func (l *Link) close(reason byte) {
	l.closed.Do(func() {
		l.reason = reason
		l.end()
		slog.Info("ble disconnected", "address", fmt.Sprintf("%012X", l.Addr()),
			"reason", fmt.Sprintf("%#02x", reason))
	})
}

func (l *Link) work() {
	for {
		select {
		case in := <-l.inbox:
			l.deliver(in)
		case <-l.unheld:
			l.release()
		case <-l.life.Done():
			return
		}
	}
}

func (l *Link) deliver(in inbound) {
	l.mu.Lock()
	f := l.handlers[in.cid]
	if f == nil || len(l.held[in.cid]) > 0 {
		if len(l.held[in.cid]) < heldDepth {
			l.held[in.cid] = append(l.held[in.cid], in.data)
		}
		l.mu.Unlock()
		if f != nil {
			l.release()
		}
		return
	}
	l.mu.Unlock()
	f(in.data)
}

func (l *Link) release() {
	type batch struct {
		f      func([]byte)
		frames [][]byte
	}
	var ready []batch
	l.mu.Lock()
	for cid, frames := range l.held {
		if f := l.handlers[cid]; f != nil {
			ready = append(ready, batch{f: f, frames: frames})
			delete(l.held, cid)
		}
	}
	l.mu.Unlock()
	for _, b := range ready {
		for _, frame := range b.frames {
			b.f(frame)
		}
	}
}

// Code, identifier, length, data.
func (l *Link) signal(p []byte) {
	if len(p) < 4 {
		return
	}
	code, id := p[0], p[1]
	switch code {
	case 0x12:
		if len(p) < 12 {
			return
		}
		go func() {
			_ = l.Send(context.Background(), cidSignaling, []byte{0x13, id, 0x02, 0x00, 0x00, 0x00})
			update := make([]byte, 14)
			binary.LittleEndian.PutUint16(update, uint16(l.Handle))
			copy(update[2:10], p[4:12])
			_ = l.s.Start(hciLEConnectionUpdate, update...)
		}()
	case 0x01, 0x13, 0x15, 0x16, 0x18:
	default:
		go func() {
			_ = l.Send(context.Background(), cidSignaling, []byte{0x01, id, 0x02, 0x00, 0x00, 0x00})
		}()
	}
}
