package detect

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/inference/wake"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
)

const Hold = 300 * time.Millisecond

const Refractory = 800 * time.Millisecond

const (
	NearMiss       = 0.5
	NearMissSettle = 700 * time.Millisecond
)

type Engine struct {
	source    Source
	callbacks sync.WaitGroup
	closed    bool

	mu sync.Mutex

	backends map[wake.Kind]backend

	spent map[wake.Kind]time.Duration
	slots []slot

	Threshold func(slot int) float64

	OnDetect func(slot int)

	OnReady func(slot int)

	Load func() error

	Paused func() bool

	// Optional synchronous observation hooks. Set before Start/Run and leave them
	// unchanged until Close. They must return promptly, never re-enter Engine,
	// and must not change the supplied audio. Nil hooks leave behavior unchanged.
	// OnFrame receives borrowed PCM before scoring (also while paused); copy
	// samples before retaining them. dropped is the source's cumulative counter.
	OnFrame func(frame []int16, paused bool, dropped uint64)
	// OnScore runs for every fresh score, including hold and refractory frames.
	// crossing means the frame starts a detection; suppressed means refractory.
	OnScore func(slot int, score, cutoff float64, crossing, suppressed bool, dropped uint64)
	// OnPeak reports the completed hold peak, not another detection.
	OnPeak func(slot int, peak float64)
	// OnModel runs after a successful model load, under the engine lock.
	// It may perform setup work but must not call Engine methods.
	OnModel func(slot int, model wake.Model)
}

type slot struct {
	model  wake.Model
	loaded bool

	scored bool

	quiet      time.Time
	peak       float64
	peakAt     time.Time
	suppressed int

	holding  bool
	holdEnds time.Time
	crossing float64
}

func New(slots int, source Source) *Engine {
	return &Engine{
		backends: map[wake.Kind]backend{},
		spent:    map[wake.Kind]time.Duration{},
		slots:    make([]slot, slots),
		source:   source,
	}
}

func (e *Engine) Name() string { return "wake" }

func (e *Engine) Start(context.Context) error {
	e.mu.Lock()
	e.reset()
	e.closed = false
	e.mu.Unlock()

	if e.Load == nil {
		return nil
	}
	return e.Load()
}

func (e *Engine) Close() error {
	e.mu.Lock()
	e.closed = true
	e.reset()
	e.mu.Unlock()
	e.callbacks.Wait()
	return nil
}

func (e *Engine) reset() {
	for k, b := range e.backends {
		b.close()
		delete(e.backends, k)
	}
	for i := range e.slots {
		e.slots[i] = slot{}
	}
}

var build = newBackend

func (e *Engine) engineFor(k wake.Kind) (backend, error) {
	if b, ok := e.backends[k]; ok {
		return b, nil
	}
	b, err := build(k)
	if err != nil {
		return nil, err
	}
	e.backends[k] = b
	return b, nil
}

func (e *Engine) Use(n int, m wake.Model) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if n < 0 || n >= len(e.slots) {
		return fmt.Errorf("wake: slot %d out of range", n)
	}
	if s := e.slots[n]; s.loaded && s.model.ID == m.ID {
		return nil
	}

	b, err := e.engineFor(m.Kind)
	if err != nil {
		return err
	}
	if err := b.load(m); err != nil {
		e.drop(slot{model: m, loaded: true})
		return err
	}

	if e.OnModel != nil {
		e.OnModel(n, m)
	}
	old := e.slots[n]
	e.slots[n] = slot{model: m, loaded: true}
	e.drop(old)

	slog.Info("wake word loaded", "slot", n+1, "phrase", m.Phrase, "id", m.ID, "engine", m.Kind)
	return nil
}

func (e *Engine) Clear(n int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if n < 0 || n >= len(e.slots) {
		return
	}
	if e.slots[n].loaded {
		slog.Info("wake word cleared", "slot", n+1, "id", e.slots[n].model.ID)
	}

	old := e.slots[n]
	e.slots[n] = slot{}
	e.drop(old)
}

func (e *Engine) drop(was slot) {
	if !was.loaded {
		return
	}
	b := e.backends[was.model.Kind]
	if b == nil {
		return
	}

	if !e.uses(was.model.ID) {
		b.unload(was.model.ID)
	}
	if !e.runs(was.model.Kind) {
		b.close()
		delete(e.backends, was.model.Kind)
	}
}

func (e *Engine) uses(id string) bool {
	for _, s := range e.slots {
		if s.loaded && s.model.ID == id {
			return true
		}
	}
	return false
}

func (e *Engine) runs(k wake.Kind) bool {
	for _, s := range e.slots {
		if s.loaded && s.model.Kind == k {
			return true
		}
	}
	return false
}

func (e *Engine) Loaded(n int) (wake.Model, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if n < 0 || n >= len(e.slots) {
		return wake.Model{}, false
	}
	return e.slots[n].model, e.slots[n].loaded
}

func (e *Engine) Ready(n int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	if n < 0 || n >= len(e.slots) {
		return false
	}
	return e.slots[n].scored
}

func (e *Engine) Listening() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, s := range e.slots {
		if s.loaded {
			return true
		}
	}
	return false
}

func (e *Engine) Run(ctx context.Context) error {
	if e.source == nil {
		return errors.New("wake: no microphone source")
	}
	frames, unlisten := e.source.Listen("wake")
	defer unlisten()

	var (
		took, worst time.Duration
		got         int
		since       = time.Now()
	)

	for {
		select {
		case <-ctx.Done():
			return nil
		case frame, ok := <-frames:
			if !ok {
				return errors.New("wake: the microphones stopped")
			}
			paused := e.Paused != nil && e.Paused()
			if e.OnFrame != nil {
				e.OnFrame(frame, paused, e.Dropped())
			}
			if paused {
				continue
			}

			began := time.Now()
			e.score(frame)
			spent := time.Since(began)

			got++
			took += spent
			worst = max(worst, spent)

			if elapsed := time.Since(since); elapsed >= 30*time.Second {
				e.mu.Lock()
				by := make([]any, 0, 8)
				for k, d := range e.spent {
					by = append(by, string(k)+"_us", d.Microseconds()/int64(max(got, 1)))
					e.spent[k] = 0
				}
				e.mu.Unlock()

				slog.Debug("wake frames", append([]any{
					"per_second", float64(got) / elapsed.Seconds(),
					"mean_us", took.Microseconds() / int64(max(got, 1)),
					"worst_ms", worst.Milliseconds(),
					"busy_pct", int(100 * took.Seconds() / elapsed.Seconds()),
				}, by...)...)
				took, worst, got, since = 0, 0, 0, time.Now()
			}
		}
	}
}

func (e *Engine) score(frame []int16) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return
	}

	// openWakeWord scores every 80 ms.
	heard := make(map[wake.Kind]map[string]float64, len(e.backends))
	for k, b := range e.backends {
		began := time.Now()
		scores, fresh := b.feed(frame)
		e.spent[k] += time.Since(began)
		if fresh {
			heard[k] = scores
		}
	}

	now := time.Now()
	for i := range e.slots {
		s := &e.slots[i]
		if !s.loaded {
			continue
		}
		score, ok := heard[s.model.Kind][s.model.ID]
		if !ok {
			continue
		}
		if !s.scored {
			s.scored = true
			slog.Info("wake word ready", "slot", i+1, "id", s.model.ID)
			if e.OnReady != nil {
				n := i
				e.notify("wake word ready", func() { e.OnReady(n) })
			}
		}
		e.judge(i, s, score, now)
	}
}

func (e *Engine) judge(n int, s *slot, score float64, now time.Time) {
	cutoff := wake.DefaultThreshold
	if e.Threshold != nil {
		cutoff = e.Threshold(n)
	}

	if e.OnScore != nil {
		e.OnScore(
			n,
			score,
			cutoff,
			!now.Before(s.quiet) && !s.holding && score >= cutoff,
			now.Before(s.quiet),
			e.Dropped(),
		)
	}
	if now.Before(s.quiet) {
		s.suppressed++
		return
	}
	if s.suppressed > 0 {
		slog.Debug("detections suppressed while settling", "slot", n+1, "frames", s.suppressed)
		s.suppressed = 0
	}

	if s.holding {
		if score > s.peak {
			s.peak = score
		}
		if now.Before(s.holdEnds) {
			return
		}

		slog.Info("wake detected", "slot", n+1, "id", s.model.ID, "peak", s.peak,
			"crossing", s.crossing, "cutoff", cutoff, "dropped", e.Dropped())
		if e.OnPeak != nil {
			e.OnPeak(n, s.peak)
		}
		s.holding, s.peak = false, 0
		s.quiet = now.Add(Refractory)
		return
	}

	if score < cutoff {
		if score > s.peak {
			s.peak, s.peakAt = score, now
		}
		if s.peak >= NearMiss && now.Sub(s.peakAt) > NearMissSettle {
			slog.Debug("wake near miss", "slot", n+1, "id", s.model.ID, "peak", s.peak,
				"cutoff", cutoff, "dropped", e.Dropped())
			s.peak = 0
		}
		return
	}

	s.holding, s.holdEnds = true, now.Add(Hold)
	s.crossing, s.peak = score, score

	if e.OnDetect != nil {
		e.notify("wake detected", func() { e.OnDetect(n) })
	}
}

type Source interface {
	Listen(string) (<-chan []int16, func())
	Dropped() uint64
}

func (e *Engine) Dropped() uint64 {
	if e.source == nil {
		return 0
	}
	return e.source.Dropped()
}

func (e *Engine) notify(name string, call func()) {
	e.callbacks.Add(1)
	safe.Go(name, func() { defer e.callbacks.Done(); call() })
}
