package webreceiver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/ygelfand/libcountertop/pkg/display/surface"
	"github.com/ygelfand/libcountertop/pkg/media/sealed"
	"github.com/ygelfand/libcountertop/pkg/media/urlplayer"
)

var errNoWidevine = errors.New("webreceiver: no widevine init data")

type keyring struct {
	r *Receiver

	mu       sync.Mutex
	changed  *sync.Cond
	drm      *sealed.Player
	cert     []byte
	sessions map[string][][]byte
	licensed map[[16]byte]bool
	any      bool
}

type forked struct {
	*sealed.Player
	k *keyring
}

func (f forked) Wait(ctx context.Context, key [16]byte) error { return f.k.wait(ctx, key) }

func (k *keyring) cond() *sync.Cond {
	if k.changed == nil {
		k.changed = sync.NewCond(&k.mu)
	}
	return k.changed
}

func (k *keyring) wait(ctx context.Context, key [16]byte) error {
	stop := context.AfterFunc(ctx, func() {
		k.mu.Lock()
		k.cond().Broadcast()
		k.mu.Unlock()
	})
	defer stop()
	k.mu.Lock()
	defer k.mu.Unlock()
	for !k.licensed[key] && (!k.any || k.requested(key)) {
		if err := ctx.Err(); err != nil {
			return err
		}
		k.cond().Wait()
	}
	return nil
}

func (k *keyring) requested(key [16]byte) bool {
	for _, kids := range k.sessions {
		for _, kid := range kids {
			if bytes.Equal(kid, key[:]) {
				return true
			}
		}
	}
	return false
}

func (k *keyring) available() bool {
	return k.r.o.Helper != nil && k.r.o.Helper() != nil
}

func (k *keyring) setCert(cert []byte) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.cert = bytes.Clone(cert)
}

func (k *keyring) open() (*sealed.Player, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.drm != nil {
		return k.drm, nil
	}
	if !k.available() {
		return nil, errors.New("webreceiver: this device has no widevine")
	}
	var p *sealed.Player
	var err error
	if len(k.cert) > 0 {
		p, err = sealed.Open("cast page", k.r.o.Helper(), k.cert, nil, k.r.env.Resample)
	} else {
		p, err = sealed.OpenWithoutPrivacy("cast page", k.r.o.Helper(), nil, k.r.env.Resample)
	}
	if err != nil {
		return nil, err
	}
	k.drm = p
	k.cond().Broadcast()
	return p, nil
}

func (k *keyring) request(session, kind string, data []byte) ([]byte, [][]byte, error) {
	init, err := initData(kind, data)
	if err != nil {
		return nil, nil, err
	}
	p, err := k.open()
	if err != nil {
		return nil, nil, err
	}
	challenge, err := p.Request(init)
	if err != nil {
		return nil, nil, err
	}
	kids := keyIDs(init)
	k.mu.Lock()
	if k.sessions == nil {
		k.sessions = map[string][][]byte{}
	}
	k.sessions[session] = kids
	k.mu.Unlock()
	return challenge, kids, nil
}

func (k *keyring) update(session string, license []byte) ([][]byte, error) {
	k.mu.Lock()
	p, kids := k.drm, k.sessions[session]
	k.mu.Unlock()
	if p == nil {
		return nil, errors.New("webreceiver: a license before any key request")
	}
	if err := p.Provide(license); err != nil {
		return kids, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.licensed == nil {
		k.licensed = map[[16]byte]bool{}
	}
	for _, kid := range kids {
		if len(kid) == 16 {
			k.licensed[[16]byte(kid)] = true
		}
	}
	k.any = true
	k.cond().Broadcast()
	return kids, nil
}

func (k *keyring) close(session string) {
	k.mu.Lock()
	delete(k.sessions, session)
	var p *sealed.Player
	if len(k.sessions) == 0 {
		p = k.drm
		k.drm, k.licensed, k.any = nil, nil, false
		k.cond().Broadcast()
	}
	k.mu.Unlock()
	if p != nil {
		p.Close()
	}
}

func (k *keyring) fork(ctx context.Context) (urlplayer.Keys, error) {
	stop := context.AfterFunc(ctx, func() {
		k.mu.Lock()
		k.cond().Broadcast()
		k.mu.Unlock()
	})
	defer stop()
	k.mu.Lock()
	defer k.mu.Unlock()
	for k.drm == nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		k.cond().Wait()
	}
	return forked{Player: k.drm.Again(), k: k}, nil
}

func (k *keyring) forget() {
	k.mu.Lock()
	p := k.drm
	k.drm, k.cert, k.sessions, k.licensed, k.any = nil, nil, nil, nil, false
	k.cond().Broadcast()
	k.mu.Unlock()
	if p != nil {
		p.Close()
	}
}

func initData(kind string, data []byte) ([]byte, error) {
	switch kind {
	case "keyids":
		var v struct{ Kids []string }
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("webreceiver: keyids init data: %w", err)
		}
		var kids [][]byte
		for _, s := range v.Kids {
			kid, err := base64.RawURLEncoding.DecodeString(s)
			if err != nil {
				return nil, fmt.Errorf("webreceiver: keyids init data: %w", err)
			}
			kids = append(kids, kid)
		}
		return widevineBox(kids), nil
	case "webm":
		return widevineBox([][]byte{data}), nil
	}
	for len(data) >= 32 {
		n := int(binary.BigEndian.Uint32(data))
		if n < 32 || n > len(data) {
			break
		}
		if string(data[4:8]) == "pssh" && bytes.Equal(data[12:28], surface.Widevine[:]) {
			return data[:n], nil
		}
		data = data[n:]
	}
	return nil, errNoWidevine
}

func widevineBox(kids [][]byte) []byte {
	var body []byte
	for _, kid := range kids {
		body = append(body, 0x12, byte(len(kid)))
		body = append(body, kid...)
	}
	out := binary.BigEndian.AppendUint32(nil, uint32(32+len(body)))
	out = append(out, "pssh"...)
	out = append(out, 0, 0, 0, 0)
	out = append(out, surface.Widevine[:]...)
	out = binary.BigEndian.AppendUint32(out, uint32(len(body)))
	return append(out, body...)
}

func keyIDs(box []byte) [][]byte {
	if len(box) < 32 {
		return nil
	}
	version, at := box[8], 28
	var kids [][]byte
	if version > 0 && len(box) >= at+4 {
		n := int(binary.BigEndian.Uint32(box[at:]))
		at += 4
		for i := 0; i < n && at+16 <= len(box); i++ {
			kids = append(kids, bytes.Clone(box[at:at+16]))
			at += 16
		}
	}
	listed := len(kids) > 0
	if len(box) < at+4 {
		return kids
	}
	size := int(binary.BigEndian.Uint32(box[at:]))
	at += 4
	if size > len(box)-at {
		return kids
	}
	data := box[at : at+size]
	for len(data) > 0 {
		tag, n := binary.Uvarint(data)
		if n <= 0 {
			break
		}
		data = data[n:]
		switch tag & 7 {
		case 0:
			_, m := binary.Uvarint(data)
			if m <= 0 {
				return kids
			}
			data = data[m:]
		case 2:
			l, m := binary.Uvarint(data)
			if m <= 0 || int(l) > len(data)-m {
				return kids
			}
			field := data[m : m+int(l)]
			if tag>>3 == 2 && !listed {
				kids = append(kids, bytes.Clone(field))
			}
			data = data[m+int(l):]
		default:
			return kids
		}
	}
	return kids
}
