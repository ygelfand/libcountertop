package detect

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/inference/pryon"
	"github.com/ygelfand/libcountertop/pkg/inference/wake"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const Hold = 300 * time.Millisecond

const (
	loosest   = 0.5
	strictest = 0.99
)

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

	Pryon func() (pryon.Options, error)
}

type slot struct {
	model  wake.Model
	key    string
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
	var b backend
	var err error
	if k == wake.KindPryon {
		b, err = newPryon(e.Pryon)
	} else {
		b, err = build(k)
	}
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
	return e.use(n, m)
}

func (e *Engine) use(n int, m wake.Model) error {
	key := e.loadKey(n, m)
	if s := e.slots[n]; s.loaded && s.key == key {
		return nil
	}

	b, err := e.engineFor(m.Kind)
	if err != nil {
		return err
	}
	if err := b.load(key, m, e.sensitivity(n)); err != nil {
		e.drop(slot{model: m, key: key, loaded: true})
		return err
	}

	old := e.slots[n]
	e.slots[n] = slot{model: m, key: key, loaded: true, scored: old.scored && old.model.ID == m.ID}
	e.drop(old)

	slog.Info("wake word loaded", "slot", n+1, "phrase", m.Phrase, "id", m.ID, "engine", m.Kind,
		"key", key)
	return nil
}

func (e *Engine) loadKey(n int, m wake.Model) string {
	if m.Kind == wake.KindPryon {
		return strconv.Itoa(n) + "/" + m.ID + "/" + strconv.Itoa(e.sensitivity(n))
	}
	return m.ID
}

func (e *Engine) cutoff(n int) float64 {
	if e.Threshold != nil {
		return e.Threshold(n)
	}
	return wake.DefaultThreshold
}

// Pryon's sensitivityValue runs -10..10 between a word's min and max accept; 0 is its own accept.
func (e *Engine) sensitivity(n int) int {
	center := wake.DefaultThreshold
	if n == StopSlot {
		center = schema.DefaultStopThreshold
	}
	t := e.cutoff(n)
	span := center - loosest
	if t > center {
		span = strictest - center
	}
	return min(max(int(math.Round(10*(t-center)/span)), -10), 10)
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

func (e *Engine) ClearKind(k wake.Kind) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for n, s := range e.slots {
		if !s.loaded || s.model.Kind != k {
			continue
		}
		slog.Info("wake word cleared", "slot", n+1, "id", s.model.ID)
		e.slots[n] = slot{}
		e.drop(s)
	}
}

func (e *Engine) drop(was slot) {
	if !was.loaded {
		return
	}
	b := e.backends[was.model.Kind]
	if b == nil {
		return
	}

	if !e.uses(was.key) {
		b.unload(was.key)
	}
	if !e.runs(was.model.Kind) {
		b.close()
		delete(e.backends, was.model.Kind)
	}
}

func (e *Engine) uses(key string) bool {
	for _, s := range e.slots {
		if s.loaded && s.key == key {
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
			if e.Paused != nil && e.Paused() {
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
		if s.model.Kind == wake.KindPryon && e.loadKey(i, s.model) != s.key {
			if err := e.use(i, s.model); err != nil {
				slog.Error("retuning a wake word failed", "slot", i+1, "id", s.model.ID, "err", err)
			}
			continue
		}
		score, ok := heard[s.model.Kind][s.key]
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
	cutoff := e.cutoff(n)
	if s.model.Kind == wake.KindPryon {
		cutoff = math.SmallestNonzeroFloat64
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
