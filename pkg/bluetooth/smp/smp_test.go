package smp

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"sync"
	"testing"
	"time"
)

type air struct {
	mu      sync.Mutex
	handler func([]byte)
	done    chan struct{}
	queue   chan func()
	device  func([]byte)
	encrypt func(ltk [16]byte, ediv uint16, random [8]byte) error
}

func newAir(t *testing.T) *air {
	a := &air{done: make(chan struct{}), queue: make(chan func(), 64)}
	go func() {
		for {
			select {
			case <-a.done:
				return
			case f := <-a.queue:
				f()
			}
		}
	}()
	t.Cleanup(func() { close(a.done) })
	return a
}

func (a *air) Send(ctx context.Context, cid uint16, payload []byte) error {
	pdu := append([]byte(nil), payload...)
	a.queue <- func() {
		if a.device != nil {
			a.device(pdu)
		}
	}
	return nil
}

func (a *air) On(cid uint16, f func([]byte)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handler = f
}

func (a *air) Done() <-chan struct{} { return a.done }

func (a *air) Encrypt(ctx context.Context, ltk [16]byte, ediv uint16, random [8]byte) error {
	return a.encrypt(ltk, ediv, random)
}

func (a *air) say(pdu []byte) {
	a.mu.Lock()
	f := a.handler
	a.mu.Unlock()
	f(pdu)
}

type responder struct {
	t      *testing.T
	a      *air
	secure bool

	preq, pres [7]byte
	central    [7]byte
	device     [7]byte

	priv     *ecdh.PrivateKey
	pka, pkb [64]byte
	dhkey    [32]byte
	na, nb   [16]byte
	mconfirm [16]byte
	key      [16]byte

	ltk  [16]byte
	ediv uint16
	rnd  [8]byte
	irk  [16]byte
}

var (
	central = Address{Addr: [6]byte{0x66, 0x55, 0x44, 0x33, 0x22, 0x11}}
	device  = Address{Addr: [6]byte{0x68, 0x5e, 0xdd, 0x16, 0xf8, 0xe2}, Type: 1}
)

func newResponder(t *testing.T, a *air, secure bool) *responder {
	rs := &responder{
		t:       t,
		a:       a,
		secure:  secure,
		ediv:    0x1234,
		rnd:     [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
		central: central.air(),
		device:  device.air(),
	}
	_, _ = rand.Read(rs.ltk[:])
	_, _ = rand.Read(rs.irk[:])
	a.device = rs.receive
	a.encrypt = rs.encrypt
	return rs
}

func (rs *responder) receive(pdu []byte) {
	switch pdu[0] {
	case pairingRequest:
		copy(rs.preq[:], pdu)
		auth := byte(authBonding)
		if rs.secure {
			auth |= authSC
		}
		rs.pres = [7]byte{
			pairingResponse,
			ioNoInputNoOutput,
			0,
			auth,
			16,
			0,
			distEncKey | distIDKey,
		}
		rs.a.say(rs.pres[:])

	case pairingPublicKey:
		copy(rs.pka[:], pdu[1:])
		rs.priv, _ = ecdh.P256().GenerateKey(rand.Reader)
		raw := rs.priv.PublicKey().Bytes()
		copy(rs.pkb[:32], reversed(raw[1:33]))
		copy(rs.pkb[32:], reversed(raw[33:]))
		peer, err := ecdh.P256().NewPublicKey(
			append(append([]byte{4}, reversed(rs.pka[:32])...), reversed(rs.pka[32:])...))
		if err != nil {
			rs.t.Errorf("central public key: %v", err)
			return
		}
		secret, _ := rs.priv.ECDH(peer)
		rs.dhkey = [32]byte(reversed(secret))
		_, _ = rand.Read(rs.nb[:])
		rs.a.say(append([]byte{pairingPublicKey}, rs.pkb[:]...))
		cb := f4([32]byte(rs.pkb[:32]), [32]byte(rs.pka[:32]), rs.nb, 0)
		rs.a.say(append([]byte{pairingConfirm}, cb[:]...))

	case pairingConfirm:
		copy(rs.mconfirm[:], pdu[1:])
		_, _ = rand.Read(rs.nb[:])
		sconfirm := rs.c1(rs.nb)
		rs.a.say(append([]byte{pairingConfirm}, sconfirm[:]...))

	case pairingRandom:
		copy(rs.na[:], pdu[1:])
		if !rs.secure {
			if rs.c1(rs.na) != rs.mconfirm {
				rs.a.say([]byte{pairingFailed, failConfirmValue})
				return
			}
			rs.key = s1([16]byte{}, rs.nb, rs.na)
		}
		rs.a.say(append([]byte{pairingRandom}, rs.nb[:]...))

	case pairingDHKeyChk:
		macKey, ltk := f5(rs.dhkey, rs.na, rs.nb, rs.central, rs.device)
		var zero [16]byte
		if f6(
			macKey,
			rs.na,
			rs.nb,
			zero,
			[3]byte(rs.preq[1:4]),
			rs.central,
			rs.device,
		) != [16]byte(
			pdu[1:],
		) {
			rs.a.say([]byte{pairingFailed, failDHKeyCheck})
			return
		}
		rs.key, rs.ltk = ltk, ltk
		eb := f6(macKey, rs.nb, rs.na, zero, [3]byte(rs.pres[1:4]), rs.device, rs.central)
		rs.a.say(append([]byte{pairingDHKeyChk}, eb[:]...))
	}
}

func (rs *responder) c1(r [16]byte) [16]byte {
	return c1(
		[16]byte{},
		r,
		rs.preq,
		rs.pres,
		0,
		[6]byte(rs.central[:6]),
		rs.device[6],
		[6]byte(rs.device[:6]),
	)
}

func (rs *responder) encrypt(ltk [16]byte, ediv uint16, random [8]byte) error {
	switch {
	case ltk == rs.key && ediv == 0 && random == [8]byte{}:
		rs.a.queue <- func() {
			if !rs.secure {
				rs.a.say(append([]byte{encryptionInfo}, rs.ltk[:]...))
				rs.a.say(append(binary.LittleEndian.AppendUint16([]byte{centralIdent}, rs.ediv), rs.rnd[:]...))
			}
			rs.a.say(append([]byte{identityInfo}, rs.irk[:]...))
			rs.a.say(append([]byte{identityAddress, 0x00}, rs.device[:6]...))
		}
		return nil
	case rs.secure && ltk == rs.ltk:
		return nil
	case !rs.secure && ltk == rs.ltk && ediv == rs.ediv && random == rs.rnd:
		return nil
	}
	return errors.New("key missing")
}

func TestPairingLeavesKeysThatEncryptAgain(t *testing.T) {
	for _, secure := range []bool{true, false} {
		name := "legacy"
		if secure {
			name = "secure connections"
		}
		t.Run(name, func(t *testing.T) {
			a := newAir(t)
			rs := newResponder(t, a, secure)
			p := New(a, central, device)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			k, err := p.Pair(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if k.Secure != secure || k.Size != 16 || k.LTK != rs.ltk || !k.HasIdentity ||
				k.IRK != rs.irk {
				t.Errorf("keys = %+v", k)
			}
			if !secure && (k.EDIV != rs.ediv || k.Rand != rs.rnd) {
				t.Errorf("central identification = %x %x", k.EDIV, k.Rand)
			}
			if k.Identity != device.Addr {
				t.Errorf("identity = %x", k.Identity)
			}

			if err := p.Encrypt(ctx, k); err != nil {
				t.Errorf("encrypting with the bond: %v", err)
			}
			if err := p.Encrypt(ctx, Keys{LTK: [16]byte{1}}); err == nil {
				t.Error("encrypting with a key the device does not have succeeded")
			}
		})
	}
}

func TestARefusedPairingReportsTheDevicesReason(t *testing.T) {
	a := newAir(t)
	a.device = func(pdu []byte) {
		if pdu[0] == pairingRequest {
			a.say([]byte{pairingFailed, 0x05})
		}
	}
	p := New(a, central, device)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := p.Pair(ctx)
	e, ok := errors.AsType[*Error](err)
	if !ok || !e.Remote || e.Reason != 0x05 {
		t.Errorf("err = %v", err)
	}
}

func TestASecurityRequestReachesItsHandler(t *testing.T) {
	a := newAir(t)
	p := New(a, central, device)
	asked := make(chan struct{})
	p.OnSecurityRequest(func() { close(asked) })
	a.say([]byte{securityRequest, authBonding})
	select {
	case <-asked:
	case <-time.After(2 * time.Second):
		t.Fatal("the security request went nowhere")
	}
}
