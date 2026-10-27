package pcm

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/audio/background"
	"github.com/ygelfand/libcountertop/pkg/audio/ducking"
	"github.com/ygelfand/libcountertop/pkg/audio/noise"
	"github.com/ygelfand/libcountertop/pkg/fetch"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
)

const (
	pace = 100 * time.Millisecond

	chunk = 32 * 1024

	stall = 30 * time.Second
)

type Stream struct {
	lifecycle sync.Mutex
	running   bool
	out       Sink
	options   Options
	workers   sync.WaitGroup
	parent    context.Context
	changed   func()

	bg Arbitration

	mu    sync.Mutex
	track *track

	down   bool
	paused bool

	gate chan struct{}

	rewind []int16

	write sync.Mutex

	gain, target float32
}

type track struct {
	item   string
	sounds []string
	cancel context.CancelFunc
}

func New(options Options) *Stream {
	if options.Rate <= 0 {
		options.Rate = 48000
	}
	if options.Channels <= 0 {
		options.Channels = 2
	}
	if options.Changed == nil {
		options.Changed = func() {}
	}
	if options.DuckDB == nil {
		options.DuckDB = func() float64 { return ducking.DefaultDB }
	}
	return &Stream{
		options: options,
		out:     options.Sink,
		bg:      options.Arbitration,
		changed: options.Changed,
		gain:    1,
		target:  1,
		parent:  context.Background(),
		running: true,
	}
}
func (m *Stream) Start(ctx context.Context) {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	m.mu.Lock()
	m.parent = ctx
	m.running = true
	m.mu.Unlock()
}
func (m *Stream) Close() error {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	m.mu.Lock()
	m.running = false
	m.mu.Unlock()
	m.Stop()
	m.workers.Wait()
	return nil
}

func (m *Stream) Duck(on bool) {
	if m == nil {
		return
	}
	level := float32(1)
	if on {
		level = ducking.Gain(m.options.DuckDB())
	}
	m.write.Lock()
	m.target = level
	m.write.Unlock()
}

func (m *Stream) Play(url string) {
	if m == nil {
		slog.Warn("asked to play media with no speaker", "url", url)
		return
	}

	t, ctx := m.start(&track{item: url})
	if t == nil {
		return
	}
	slog.Info("playing media", "url", url)

	safe.Go("media", func() {
		defer m.workers.Done()
		err := m.run(ctx, t, url)
		if err != nil && ctx.Err() == nil {
			slog.Error("playing media failed", "err", err)
		}
		m.finished(t)
	})
}

func (m *Stream) PlayNoise(sounds ...string) {
	if m == nil {
		slog.Warn("asked to play noise with no speaker", "sounds", sounds)
		return
	}

	fill := noise.Mix(m.options.Rate, sounds...)
	if fill == nil {
		slog.Warn("no such sound", "sounds", sounds)
		return
	}

	t, ctx := m.start(&track{item: strings.Join(sounds, " and "), sounds: sounds})
	if t == nil {
		return
	}
	slog.Info("playing noise", "sounds", sounds)

	safe.Go("noise", func() {
		defer m.workers.Done()
		err := m.generate(ctx, t, fill)
		if err != nil && ctx.Err() == nil {
			slog.Error("playing noise failed", "err", err)
		}
		m.finished(t)
	})
}

func (m *Stream) Noise() []string {
	if m == nil {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.track == nil {
		return nil
	}
	return slices.Clone(m.track.sounds)
}

func (m *Stream) start(t *track) (*track, context.Context) {
	m.lifecycle.Lock()
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		m.lifecycle.Unlock()
		return nil, nil
	}
	ctx, cancel := context.WithCancel(m.parent)
	t.cancel = cancel
	previous := m.track
	m.track, m.paused, m.rewind = t, false, nil
	m.workers.Add(1)
	m.regate()
	m.mu.Unlock()
	previous.stop()
	m.flush()
	m.bg.Took(m)
	m.lifecycle.Unlock()
	m.changed()
	return t, ctx
}

func (m *Stream) Pause() {
	if m == nil {
		return
	}

	m.mu.Lock()
	if m.track == nil || m.paused {
		m.mu.Unlock()
		return
	}
	m.paused = true
	m.regate()
	m.mu.Unlock()

	if m.bg.Owns(m) {
		m.keep()
	}
	m.changed()
}

func (m *Stream) Unpause() {
	if m == nil {
		return
	}

	m.mu.Lock()
	if m.track == nil || !m.paused {
		m.mu.Unlock()
		return
	}
	m.paused = false
	m.regate()
	m.mu.Unlock()

	m.changed()
}

func (m *Stream) Stop() {
	if m == nil {
		return
	}

	m.mu.Lock()
	t := m.track
	m.track, m.paused, m.rewind = nil, false, nil
	m.unblock()
	m.mu.Unlock()

	m.bg.Gave(m)

	if t == nil {
		return
	}
	t.stop()
	m.flush()
	m.changed()
}

func (m *Stream) Stand(down bool) {
	if m == nil {
		return
	}

	m.mu.Lock()
	first := down && !m.down
	take := first && m.track != nil
	m.down = down
	if down {
		m.block()
	} else if !m.paused {
		m.unblock()
	}
	m.mu.Unlock()

	if take {
		m.keep()
	}
}

func (m *Stream) Playing() (playing, paused bool) {
	if m == nil {
		return false, false
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	return m.track != nil && !m.paused, m.track != nil && m.paused
}

func (m *Stream) regate() {
	if m.down || m.paused {
		m.block()
		return
	}
	m.unblock()
}

func (m *Stream) block() {
	if m.gate == nil {
		m.gate = make(chan struct{})
	}
}

func (m *Stream) unblock() {
	if m.gate != nil {
		close(m.gate)
		m.gate = nil
	}
}

func (m *Stream) flush() {
	if !m.bg.Owns(m) {
		return
	}

	m.write.Lock()
	defer m.write.Unlock()
	m.out.Drain()
}

func (m *Stream) keep() {
	m.write.Lock()
	defer m.write.Unlock()

	kept := m.out.Take()
	if len(kept) == 0 {
		return
	}

	m.mu.Lock()
	m.rewind = kept
	m.mu.Unlock()
}

func (m *Stream) replay() {
	m.write.Lock()
	defer m.write.Unlock()

	m.mu.Lock()
	back := m.rewind
	if m.gate != nil || len(back) == 0 {
		m.mu.Unlock()
		return
	}
	m.rewind = nil
	m.mu.Unlock()

	m.out.Play(back)
}

func (m *Stream) finished(t *track) {
	m.mu.Lock()
	if m.track != t {
		m.mu.Unlock()
		return
	}
	m.track, m.paused, m.rewind = nil, false, nil
	m.unblock()
	m.mu.Unlock()

	m.bg.Gave(m)

	slog.Info("media finished", "item", t.item)
	m.changed()
}

func (t *track) stop() {
	if t != nil {
		t.cancel()
	}
}

func (m *Stream) run(ctx context.Context, t *track, url string) error {
	fetchCtx, giveUp := context.WithCancel(ctx)
	defer giveUp()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := fetch.Client(0).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}

	body := bufio.NewReaderSize(resp.Body, chunk)
	watchHeader := time.AfterFunc(stall, giveUp)
	err = Header(body, Format{Rate: m.options.Rate, Channels: m.options.Channels})
	watchHeader.Stop()
	if err != nil {
		return err
	}

	buf := make([]byte, chunk)
	for {
		if err := m.wait(ctx); err != nil {
			return err
		}

		watchdog := time.AfterFunc(stall, giveUp)
		n, err := io.ReadFull(body, buf)
		watchdog.Stop()

		frame := m.options.Channels * 2
		if n >= frame {
			m.feed(t, buf[:n-n%frame])
		}
		switch {
		case err == nil:
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			return m.settle(ctx)
		case fetchCtx.Err() != nil && ctx.Err() == nil:
			return fmt.Errorf("nothing arrived for %s", stall)
		default:
			return err
		}
	}
}

func (m *Stream) wait(ctx context.Context) error {
	for {
		m.mu.Lock()
		gate := m.gate
		m.mu.Unlock()

		if gate != nil {
			select {
			case <-gate:
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		m.replay()
		if m.out.Queued() < m.options.Rate {
			return nil
		}

		select {
		case <-time.After(pace):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (m *Stream) settle(ctx context.Context) error {
	for {
		if err := m.wait(ctx); err != nil {
			return err
		}
		if m.out.Queued() == 0 {
			return nil
		}

		select {
		case <-time.After(pace):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (m *Stream) generate(ctx context.Context, t *track, fill noise.Fill) error {
	mono := make([]float32, chunk/(m.options.Channels*2))
	samples := make([]int16, len(mono)*m.options.Channels)

	for {
		if err := m.wait(ctx); err != nil {
			return err
		}

		fill(mono)
		for i, v := range mono {
			s := int16(v * math.MaxInt16)
			for channel := range m.options.Channels {
				samples[i*m.options.Channels+channel] = s
			}
		}
		m.queue(t, samples)
	}
}

func (m *Stream) feed(t *track, pcm []byte) {
	samples := make([]int16, len(pcm)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(pcm[i*2:]))
	}
	m.queue(t, samples)
}

func (m *Stream) queue(t *track, samples []int16) {
	m.write.Lock()
	defer m.write.Unlock()

	m.mu.Lock()
	stale := m.gate != nil || m.track != t
	m.mu.Unlock()
	if stale {
		return
	}

	m.attenuate(samples)
	m.out.Play(samples)
}

func (m *Stream) Requeue() {
	m.write.Lock()
	defer m.write.Unlock()

	m.out.Adjust(m.attenuate)
}

func (m *Stream) attenuate(samples []int16) {
	if m.gain == 1 && m.target == 1 {
		return
	}

	step := float32(1.0) / float32(m.options.Rate*m.options.Channels*60/1000)
	for i, s := range samples {
		switch {
		case m.gain < m.target:
			m.gain = min(m.gain+step, m.target)
		case m.gain > m.target:
			m.gain = max(m.gain-step, m.target)
		}
		samples[i] = int16(float32(s) * m.gain)
	}
}

// Home Assistant streams ffmpeg's WAV with header sizes written before the length was known.
func Header(r *bufio.Reader, format Format) error {
	var riff [12]byte
	if _, err := io.ReadFull(r, riff[:]); err != nil {
		return fmt.Errorf("reading the WAVE header: %w", err)
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return fmt.Errorf("not a WAVE stream: %q", riff[0:4])
	}

	for {
		var head [8]byte
		if _, err := io.ReadFull(r, head[:]); err != nil {
			return fmt.Errorf("reading a WAVE chunk: %w", err)
		}
		id := string(head[0:4])
		size := int64(binary.LittleEndian.Uint32(head[4:8]))

		if id == "data" {
			return nil
		}

		if size > chunk {
			return fmt.Errorf("%q chunk is %d bytes", id, size)
		}

		body := make([]byte, size+size%2)
		if _, err := io.ReadFull(r, body); err != nil {
			return fmt.Errorf("reading the %q chunk: %w", id, err)
		}
		if id == "fmt " {
			if err := supported(body[:size], format); err != nil {
				return err
			}
		}
	}
}

func supported(fmtChunk []byte, format Format) error {
	if len(fmtChunk) < 16 {
		return fmt.Errorf("short fmt chunk: %d bytes", len(fmtChunk))
	}

	channels := binary.LittleEndian.Uint16(fmtChunk[2:])
	rate := binary.LittleEndian.Uint32(fmtChunk[4:])
	bits := binary.LittleEndian.Uint16(fmtChunk[14:])

	if int(channels) != format.Channels || int(rate) != format.Rate || bits != 16 {
		return fmt.Errorf("cannot play %d Hz %d channel %d bit audio", rate, channels, bits)
	}
	return nil
}

type Sink interface {
	Play([]int16)
	Take() []int16
	Adjust(func([]int16))
	Drain()
	Queued() int
}

type Producer = background.Producer

type Arbitration interface {
	Took(Producer)
	Gave(Producer)
	Owns(Producer) bool
}
type Options struct {
	Sink           Sink
	Arbitration    Arbitration
	Rate, Channels int
	DuckDB         func() float64
	Changed        func()
}
type Format struct{ Rate, Channels int }
