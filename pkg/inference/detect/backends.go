package detect

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/zserge/microwakeword"

	"github.com/ygelfand/libcountertop/pkg/inference/gemini"
	"github.com/ygelfand/libcountertop/pkg/inference/oww"
	"github.com/ygelfand/libcountertop/pkg/inference/wake"
)

type backend interface {
	load(key string, m wake.Model, sensitivity int) error

	unload(key string)

	feed(frame []int16) (map[string]float64, bool)

	close()
}

func newBackend(k wake.Kind) (backend, error) {
	if k == wake.KindGemini {
		return &geminiBackend{
			dets:   map[string]*gemini.Detector{},
			scores: map[string]float64{},
		}, nil
	}
	if k == wake.KindOpenWakeWord {
		front, err := oww.New()
		if err != nil {
			return nil, err
		}
		return &owwBackend{front: front, scores: map[string]float64{}}, nil
	}
	return &microBackend{
		dets:   map[string]*microwakeword.Detector{},
		scores: map[string]float64{},
	}, nil
}

type microBackend struct {
	dets   map[string]*microwakeword.Detector
	scores map[string]float64
}

const neverFires = 1.1

func (b *microBackend) load(key string, m wake.Model, _ int) error {
	cfg := m.Config
	cfg.ProbabilityCutoff = neverFires

	det, err := microwakeword.NewDetector(cfg)
	if err != nil {
		return fmt.Errorf("wake: loading %s: %w", m.Path, err)
	}
	b.dets[key] = det
	return nil
}

func (b *microBackend) unload(key string) {
	delete(b.dets, key)
	delete(b.scores, key)
}

func (b *microBackend) feed(frame []int16) (map[string]float64, bool) {
	for id, det := range b.dets {
		det.ProcessAudio(frame)
		b.scores[id] = det.SlidingAverage()
	}
	return b.scores, true
}

func (b *microBackend) close() { clear(b.dets) }

type owwBackend struct {
	front  *oww.Engine
	scores map[string]float64
}

func (b *owwBackend) load(key string, m wake.Model, _ int) error {
	model, err := os.ReadFile(m.Path)
	if err != nil {
		return fmt.Errorf("wake: reading %s: %w", m.Path, err)
	}
	_, err = b.front.Load(key, model)
	return err
}

func (b *owwBackend) unload(key string) {
	b.front.Unload(key)
	delete(b.scores, key)
}

func (b *owwBackend) feed(frame []int16) (map[string]float64, bool) {
	scores, err := b.front.Process(frame)
	if err != nil {
		slog.Error("wake scoring failed", "err", err)
		return nil, false
	}
	if len(scores) == 0 {
		return nil, false
	}

	for id, v := range scores {
		b.scores[id] = float64(v)
	}
	return b.scores, true
}

func (b *owwBackend) close() {}
