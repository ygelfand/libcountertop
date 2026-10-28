package sealed

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/surface"
	"github.com/ygelfand/libcountertop/pkg/media/cenc"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

const (
	Rate     = 48000
	Channels = 2
	held     = 30
)

var drmIDs, audioIDs atomic.Uint32

type License func(ctx context.Context, challenge []byte) ([]byte, error)

type frame struct {
	data  []byte
	at    time.Duration
	crypt *surface.Crypt
}

type Player struct {
	Name string

	surf     *surface.Client
	license  License
	resample func(from int) func([]int16) []int16
	drm      uint32
	audio    uint32
	borrowed bool

	done      chan struct{}
	once      sync.Once
	ready     chan struct{}
	readyOnce sync.Once

	mu       sync.Mutex
	room     *sync.Cond
	pcm      []int16
	err      error
	origin   time.Duration
	based    bool
	early    bool
	heard    bool
	resamp   func([]int16) []int16
	rate     int
	opened   bool
	licMu    sync.Mutex
	licensed map[string]bool

	frames chan frame
}

func Open(
	name string,
	surf *surface.Client,
	cert []byte,
	license License,
	resample func(from int) func([]int16) []int16,
) (*Player, error) {
	return open(name, surf, true, cert, license, resample)
}

func OpenWithoutPrivacy(
	name string,
	surf *surface.Client,
	license License,
	resample func(from int) func([]int16) []int16,
) (*Player, error) {
	return open(name, surf, false, nil, license, resample)
}

func open(
	name string,
	surf *surface.Client,
	privacy bool,
	cert []byte,
	license License,
	resample func(from int) func([]int16) []int16,
) (*Player, error) {
	if surf == nil {
		return nil, errors.New(name + ": the display helper is not connected")
	}
	p := New(name, surf, license, resample)
	if err := surf.DRMOpen(p.drm, surface.Widevine, privacy, cert); err != nil {
		return nil, fmt.Errorf("%s: widevine: %w", name, err)
	}
	return p, nil
}

func New(
	name string,
	surf *surface.Client,
	license License,
	resample func(from int) func([]int16) []int16,
) *Player {
	p := &Player{
		Name: name, surf: surf, license: license, resample: resample,
		drm: 1 + drmIDs.Add(1), audio: 1 + audioIDs.Add(1),
		done: make(chan struct{}), ready: make(chan struct{}),
		licensed: map[string]bool{}, frames: make(chan frame, 300),
	}
	p.room = sync.NewCond(&p.mu)
	return p
}

func (p *Player) Again() *Player {
	q := New(p.Name, p.surf, p.license, p.resample)
	q.drm, q.borrowed = p.drm, true
	return q
}

func (p *Player) Request(init []byte) ([]byte, error) {
	challenge, err := p.surf.DRMRequest(p.drm, init)
	if err != nil {
		return nil, fmt.Errorf("%s: key request: %w", p.Name, err)
	}
	return challenge, nil
}

func (p *Player) Provide(license []byte) error {
	if err := p.surf.DRMProvide(p.drm, license); err != nil {
		return fmt.Errorf("%s: key response: %w", p.Name, err)
	}
	return nil
}

func widevine(boxes [][]byte) []byte {
	for _, b := range boxes {
		if len(b) >= 28 && bytes.Equal(b[12:28], surface.Widevine[:]) {
			return b
		}
	}
	return nil
}

func (p *Player) License(ctx context.Context, boxes [][]byte) error {
	init := widevine(boxes)
	if init == nil {
		return nil
	}
	key := string(init)
	p.licMu.Lock()
	defer p.licMu.Unlock()
	if p.licensed[key] {
		return nil
	}
	challenge, err := p.surf.DRMRequest(p.drm, init)
	if err != nil && len(p.licensed) > 0 {
		p.licensed[key] = true
		slog.Debug("kept the license already held", "player", p.Name, "err", err)
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s: key request: %w", p.Name, err)
	}
	lic, err := p.license(ctx, challenge)
	if err != nil {
		return err
	}
	if err := p.surf.DRMProvide(p.drm, lic); err != nil {
		return fmt.Errorf("%s: key response: %w", p.Name, err)
	}
	p.licensed[key] = true
	slog.Debug("licensed", "player", p.Name, "keys", len(p.licensed))
	return nil
}

func (p *Player) OpenAudio(t cenc.Track) error {
	p.mu.Lock()
	opened := p.opened
	p.opened = true
	p.mu.Unlock()
	if opened {
		return nil
	}
	if err := p.surf.AudioOpen(p.audio, p.drm, t.Rate, t.Channels, t.Config); err != nil {
		return fmt.Errorf("%s: audio decoder: %w", p.Name, err)
	}
	p.mu.Lock()
	p.rate = t.Rate
	p.mu.Unlock()
	return nil
}

func crypt(t cenc.Track, s cenc.Sample, subs []cenc.Subsample) *surface.Crypt {
	if !s.Encrypted {
		return nil
	}
	c := &cenc.Crypt{
		Mode:       cenc.CryptCENC,
		Key:        s.KeyID,
		IV:         s.IV,
		Subsamples: slices.Clone(subs),
	}
	if t.Scheme == cenc.SchemeCBCS {
		c.Mode, c.Encrypt, c.Skip = cenc.CryptCBCS, uint32(t.Crypt), uint32(t.Skip)
	}
	return c
}

func (p *Player) anchor(at time.Duration) (time.Duration, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.based {
		p.origin, p.based = at, true
		p.room.Broadcast()
	}
	return at - p.origin, p.early
}

func (p *Player) Begin(at time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.origin, p.based, p.early = at, true, true
	p.room.Broadcast()
}

func (p *Player) Origin() (time.Duration, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.origin, p.based
}

func (p *Player) Picture(ctx context.Context, t cenc.Track, s cenc.Sample) error {
	data, subs, err := t.AnnexB(s)
	if err != nil {
		return err
	}
	at, early := p.anchor(s.At)
	if at < 0 && !early {
		return nil
	}
	select {
	case p.frames <- frame{data: data, at: at, crypt: crypt(t, s, subs)}:
		p.readyOnce.Do(func() { close(p.ready) })
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return io.EOF
	}
}

func (p *Player) Sound(t cenc.Track, s cenc.Sample) error {
	p.mu.Lock()
	for !p.based {
		select {
		case <-p.done:
			p.mu.Unlock()
			return io.EOF
		default:
		}
		p.room.Wait()
	}
	origin, first := p.origin, !p.heard
	p.mu.Unlock()
	if s.At < origin {
		return nil
	}
	if first {
		p.mu.Lock()
		p.heard = true
		gap := int(int64(s.At-origin) * Rate / int64(time.Second))
		p.pcm = append(p.pcm, make([]int16, gap*Channels)...)
		p.mu.Unlock()
	}
	k := crypt(t, s, s.Subsamples)
	for {
		out, err := p.surf.AudioSample(p.audio, s.At-origin, 0, k, s.Data)
		if len(out.Data) > 0 {
			if err := p.keep(out); err != nil {
				return err
			}
		}
		if !errors.Is(err, surface.Full) {
			if err != nil {
				return fmt.Errorf("%s: audio: %w", p.Name, err)
			}
			return nil
		}
		select {
		case <-p.done:
			return io.EOF
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func (p *Player) keep(out surface.PCM) error {
	pcm := make([]int16, len(out.Data)/2)
	for i := range pcm {
		pcm[i] = int16(binary.LittleEndian.Uint16(out.Data[2*i:]))
	}
	if out.Channels == 1 {
		st := make([]int16, 2*len(pcm))
		for i, v := range pcm {
			st[2*i], st[2*i+1] = v, v
		}
		pcm = st
	}
	p.mu.Lock()
	if p.resamp == nil || out.Rate != p.rate {
		p.rate = out.Rate
		if out.Rate != Rate && p.resample != nil {
			p.resamp = p.resample(out.Rate)
		} else {
			p.resamp = func(x []int16) []int16 { return x }
		}
	}
	pcm = p.resamp(pcm)
	for len(p.pcm) > held*Rate*Channels {
		p.room.Wait()
		select {
		case <-p.done:
			p.mu.Unlock()
			return io.EOF
		default:
		}
	}
	p.pcm = append(p.pcm, pcm...)
	p.mu.Unlock()
	return nil
}

func (p *Player) Read(out []int16) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := copy(out, p.pcm)
	p.pcm = p.pcm[n:]
	if n > 0 {
		p.room.Broadcast()
		return n, nil
	}
	select {
	case <-p.done:
		return 0, p.err
	default:
		return 0, nil
	}
}

func (p *Player) next(ctx context.Context) ([]byte, time.Duration, *surface.Crypt, error) {
	select {
	case <-ctx.Done():
		return nil, 0, nil, ctx.Err()
	case f := <-p.frames:
		return f.data, f.at, f.crypt, nil
	case <-p.done:
		return nil, 0, nil, p.Failure()
	}
}

func (p *Player) Video(width, height int) playback.Picture {
	return playback.Picture{
		Codec:   playback.H264,
		Width:   width,
		Height:  height,
		Session: p.drm,
		Sealed:  p.next,
	}
}

func (p *Player) Ready() <-chan struct{} { return p.ready }
func (p *Player) Done() <-chan struct{}  { return p.done }

func (p *Player) Fail(err error) {
	p.mu.Lock()
	if p.err == nil {
		p.err = err
	}
	p.mu.Unlock()
	p.stop()
}

func (p *Player) stop() {
	p.once.Do(func() {
		close(p.done)
		p.mu.Lock()
		p.room.Broadcast()
		p.mu.Unlock()
	})
}

func (p *Player) Failure() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err == nil {
		return io.EOF
	}
	return p.err
}

func (p *Player) Close() {
	p.Fail(io.EOF)
	_ = p.surf.AudioClose(p.audio)
	if !p.borrowed {
		_ = p.surf.DRMClose(p.drm)
	}
}
