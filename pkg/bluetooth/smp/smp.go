package smp

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const CID = 0x0006

const (
	pairingRequest   = 0x01
	pairingResponse  = 0x02
	pairingConfirm   = 0x03
	pairingRandom    = 0x04
	pairingFailed    = 0x05
	encryptionInfo   = 0x06
	centralIdent     = 0x07
	identityInfo     = 0x08
	identityAddress  = 0x09
	securityRequest  = 0x0b
	pairingPublicKey = 0x0c
	pairingDHKeyChk  = 0x0d
)

const (
	ioNoInputNoOutput = 0x03
	authBonding       = 0x01
	authSC            = 0x08
	distEncKey        = 0x01
	distIDKey         = 0x02
	maxKeySize        = 16
	minKeySize        = 7
	timeout           = 30 * time.Second
)

const (
	failConfirmValue   = 0x04
	failEncKeySize     = 0x06
	failCmdUnsupported = 0x07
	failUnspecified    = 0x08
	failInvalidParams  = 0x0a
	failDHKeyCheck     = 0x0b
)

var ErrClosed = errors.New("smp: the link is closed")

type Link interface {
	Send(ctx context.Context, cid uint16, payload []byte) error
	On(cid uint16, f func([]byte))
	Done() <-chan struct{}
	Encrypt(ctx context.Context, ltk [16]byte, ediv uint16, random [8]byte) error
}

type Address struct {
	Addr [6]byte
	Type byte
}

// Address then type, least significant octet first.
func (a Address) air() [7]byte {
	var out [7]byte
	for i := range 6 {
		out[i] = a.Addr[5-i]
	}
	out[6] = a.Type
	return out
}

type Error struct {
	Reason uint8
	Remote bool
}

func (e *Error) Error() string {
	side := "local"
	if e.Remote {
		side = "remote"
	}
	return fmt.Sprintf("smp: pairing failed (%s, reason %#02x)", side, e.Reason)
}

type Keys struct {
	LTK    [16]byte
	EDIV   uint16
	Rand   [8]byte
	Size   int
	Secure bool

	IRK          [16]byte
	Identity     [6]byte
	IdentityType uint8
	HasIdentity  bool
}

type Pairer struct {
	l           Link
	local, peer Address

	mu        sync.Mutex
	in        chan []byte
	onRequest func()

	pairing sync.Mutex
}

func New(l Link, local, peer Address) *Pairer {
	p := &Pairer{l: l, local: local, peer: peer}
	l.On(CID, p.receive)
	return p
}

func (p *Pairer) OnSecurityRequest(f func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onRequest = f
}

func (p *Pairer) Encrypt(ctx context.Context, k Keys) error {
	return p.l.Encrypt(ctx, k.LTK, k.EDIV, k.Rand)
}

func (p *Pairer) Pair(ctx context.Context) (Keys, error) {
	p.pairing.Lock()
	defer p.pairing.Unlock()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	in := make(chan []byte, 16)
	p.mu.Lock()
	p.in = in
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.in = nil
		p.mu.Unlock()
	}()

	run := &pairing{p: p, ctx: ctx, in: in}
	k, err := run.run()
	if err != nil {
		e, ok := errors.AsType[*Error](err)
		if !ok || !e.Remote {
			reason := uint8(failUnspecified)
			if ok {
				reason = e.Reason
			}
			_ = p.l.Send(context.Background(), CID, []byte{pairingFailed, reason})
		}
		return Keys{}, err
	}
	slog.Info(
		"ble paired",
		"address",
		fmt.Sprintf("%X", p.peer.Addr),
		"secure",
		k.Secure,
		"key_size",
		k.Size,
	)
	return k, nil
}

func (p *Pairer) receive(pdu []byte) {
	if len(pdu) == 0 {
		return
	}
	p.mu.Lock()
	in, onRequest := p.in, p.onRequest
	p.mu.Unlock()

	switch {
	case in != nil:
		select {
		case in <- append([]byte(nil), pdu...):
		default:
		}
	case pdu[0] == securityRequest:
		if onRequest != nil {
			go onRequest()
		}
	case pdu[0] == pairingRequest:
		go func() {
			_ = p.l.Send(context.Background(), CID, []byte{pairingFailed, failCmdUnsupported})
		}()
	}
}

type pairing struct {
	p    *Pairer
	ctx  context.Context
	in   chan []byte
	preq [7]byte
	pres [7]byte
}

func (r *pairing) send(pdu ...byte) error {
	if err := r.p.l.Send(r.ctx, CID, pdu); err != nil {
		select {
		case <-r.p.l.Done():
			return ErrClosed
		default:
			return err
		}
	}
	return nil
}

func (r *pairing) expect(code uint8, size int) ([]byte, error) {
	select {
	case pdu := <-r.in:
		if pdu[0] == pairingFailed && len(pdu) >= 2 {
			return nil, &Error{Reason: pdu[1], Remote: true}
		}
		if pdu[0] != code || len(pdu) != 1+size {
			return nil, &Error{Reason: failInvalidParams}
		}
		return pdu[1:], nil
	case <-r.ctx.Done():
		return nil, fmt.Errorf("smp: pairing: %w", r.ctx.Err())
	case <-r.p.l.Done():
		return nil, ErrClosed
	}
}

func (r *pairing) run() (Keys, error) {
	r.preq = [7]byte{
		pairingRequest,
		ioNoInputNoOutput,
		0x00,
		authBonding | authSC,
		maxKeySize,
		0x00,
		distEncKey | distIDKey,
	}
	if err := r.send(r.preq[:]...); err != nil {
		return Keys{}, err
	}
	rsp, err := r.expect(pairingResponse, 6)
	if err != nil {
		return Keys{}, err
	}
	r.pres[0] = pairingResponse
	copy(r.pres[1:], rsp)

	var k Keys
	k.Size = min(maxKeySize, int(r.pres[4]))
	if k.Size < minKeySize {
		return Keys{}, &Error{Reason: failEncKeySize}
	}
	k.Secure = r.pres[3]&authSC != 0

	var key [16]byte
	if k.Secure {
		key, err = r.secureConnections()
	} else {
		key, err = r.legacy()
	}
	if err != nil {
		return Keys{}, err
	}
	for i := k.Size; i < 16; i++ {
		key[i] = 0
	}
	if err := r.p.l.Encrypt(r.ctx, key, 0, [8]byte{}); err != nil {
		return Keys{}, err
	}
	if k.Secure {
		k.LTK = key
	}
	return k, r.distribution(&k)
}

func (r *pairing) legacy() ([16]byte, error) {
	var tk, mrand [16]byte
	if _, err := rand.Read(mrand[:]); err != nil {
		return tk, err
	}
	theirs, ours := r.p.peer.air(), r.p.local.air()
	ra, ia := [6]byte(theirs[:6]), [6]byte(ours[:6])
	confirm := func(n [16]byte) [16]byte {
		return c1(tk, n, r.preq, r.pres, ours[6], ia, theirs[6], ra)
	}

	mconfirm := confirm(mrand)
	if err := r.send(append([]byte{pairingConfirm}, mconfirm[:]...)...); err != nil {
		return tk, err
	}
	sconfirm, err := r.expect(pairingConfirm, 16)
	if err != nil {
		return tk, err
	}
	if err := r.send(append([]byte{pairingRandom}, mrand[:]...)...); err != nil {
		return tk, err
	}
	srandBytes, err := r.expect(pairingRandom, 16)
	if err != nil {
		return tk, err
	}
	srand := [16]byte(srandBytes)
	if confirm(srand) != [16]byte(sconfirm) {
		return tk, &Error{Reason: failConfirmValue}
	}
	return s1(tk, srand, mrand), nil
}

func (r *pairing) secureConnections() ([16]byte, error) {
	var ltk [16]byte
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return ltk, err
	}
	// Uncompressed points are 0x04, X, Y, most significant octet first.
	raw := priv.PublicKey().Bytes()
	var pka [64]byte
	copy(pka[:32], reversed(raw[1:33]))
	copy(pka[32:], reversed(raw[33:65]))
	if err := r.send(append([]byte{pairingPublicKey}, pka[:]...)...); err != nil {
		return ltk, err
	}

	pkbBytes, err := r.expect(pairingPublicKey, 64)
	if err != nil {
		return ltk, err
	}
	pkb := [64]byte(pkbBytes)
	if pkb == pka {
		return ltk, &Error{Reason: failInvalidParams}
	}
	uncompressed := append([]byte{0x04}, reversed(pkb[:32])...)
	uncompressed = append(uncompressed, reversed(pkb[32:])...)
	peer, err := ecdh.P256().NewPublicKey(uncompressed)
	if err != nil {
		return ltk, &Error{Reason: failInvalidParams}
	}
	secret, err := priv.ECDH(peer)
	if err != nil {
		return ltk, &Error{Reason: failDHKeyCheck}
	}
	dhkey := [32]byte(reversed(secret))

	cb, err := r.expect(pairingConfirm, 16)
	if err != nil {
		return ltk, err
	}
	var na [16]byte
	if _, err := rand.Read(na[:]); err != nil {
		return ltk, err
	}
	if err := r.send(append([]byte{pairingRandom}, na[:]...)...); err != nil {
		return ltk, err
	}
	nbBytes, err := r.expect(pairingRandom, 16)
	if err != nil {
		return ltk, err
	}
	nb := [16]byte(nbBytes)
	if f4([32]byte(pkb[:32]), [32]byte(pka[:32]), nb, 0) != [16]byte(cb) {
		return ltk, &Error{Reason: failConfirmValue}
	}

	a1, a2 := r.p.local.air(), r.p.peer.air()
	macKey, ltk := f5(dhkey, na, nb, a1, a2)
	var zero [16]byte
	ea := f6(macKey, na, nb, zero, [3]byte(r.preq[1:4]), a1, a2)
	if err := r.send(append([]byte{pairingDHKeyChk}, ea[:]...)...); err != nil {
		return ltk, err
	}
	eb, err := r.expect(pairingDHKeyChk, 16)
	if err != nil {
		return ltk, err
	}
	if f6(macKey, nb, na, zero, [3]byte(r.pres[1:4]), a2, a1) != [16]byte(eb) {
		return ltk, &Error{Reason: failDHKeyCheck}
	}
	return ltk, nil
}

// Secure Connections derives the LTK; the peer never sends one.
func (r *pairing) distribution(k *Keys) error {
	want := r.pres[6] & r.preq[6]
	if k.Secure {
		want &^= distEncKey
	}
	var gotEnc, gotCentral, gotIRK, gotAddr bool
	for {
		if (want&distEncKey == 0 || gotEnc && gotCentral) &&
			(want&distIDKey == 0 || gotIRK && gotAddr) {
			return nil
		}
		select {
		case pdu := <-r.in:
			switch {
			case pdu[0] == pairingFailed && len(pdu) >= 2:
				return &Error{Reason: pdu[1], Remote: true}
			case pdu[0] == encryptionInfo && len(pdu) == 17:
				copy(k.LTK[:], pdu[1:])
				gotEnc = true
			case pdu[0] == centralIdent && len(pdu) == 11:
				k.EDIV = binary.LittleEndian.Uint16(pdu[1:])
				copy(k.Rand[:], pdu[3:])
				gotCentral = true
			case pdu[0] == identityInfo && len(pdu) == 17:
				copy(k.IRK[:], pdu[1:])
				gotIRK = true
			case pdu[0] == identityAddress && len(pdu) == 8:
				k.IdentityType = pdu[1]
				for i := range 6 {
					k.Identity[i] = pdu[7-i]
				}
				k.HasIdentity = true
				gotAddr = true
			}
		case <-r.ctx.Done():
			return fmt.Errorf("smp: waiting for keys: %w", r.ctx.Err())
		case <-r.p.l.Done():
			return ErrClosed
		}
	}
}
