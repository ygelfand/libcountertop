package detect

import (
	"errors"
	"log/slog"
	"sync"

	"github.com/ygelfand/libcountertop/pkg/inference/pryon"
	"github.com/ygelfand/libcountertop/pkg/inference/wake"
)

type pryonBackend struct {
	client *pryon.Client

	mu     sync.Mutex
	heard  map[string]float64
	scores map[string]float64
}

func newPryon(options func() (pryon.Options, error)) (backend, error) {
	if options == nil {
		return nil, errors.New("wake: pryon is not available on this device")
	}
	o, err := options()
	if err != nil {
		return nil, err
	}
	b := &pryonBackend{heard: map[string]float64{}, scores: map[string]float64{}}
	client, err := pryon.Start(o, b.detected)
	if err != nil {
		return nil, err
	}
	b.client = client
	return b, nil
}

func (b *pryonBackend) detected(d pryon.Detection) {
	slog.Debug("pryon accepted", "id", d.ID, "score", d.Score, "start", d.Start, "end", d.End)
	b.mu.Lock()
	b.heard[d.ID] = d.Score
	b.mu.Unlock()
}

func (b *pryonBackend) load(key string, m wake.Model, sensitivity int) error {
	if err := b.client.Load(key, m.Path, pryon.Aux(m.Keyword, sensitivity)); err != nil {
		return err
	}
	b.mu.Lock()
	b.scores[key] = 0
	b.mu.Unlock()
	return nil
}

func (b *pryonBackend) unload(key string) {
	b.client.Unload(key)
	b.mu.Lock()
	delete(b.scores, key)
	delete(b.heard, key)
	b.mu.Unlock()
}

func (b *pryonBackend) feed(frame []int16) (map[string]float64, bool) {
	if !b.client.Alive() {
		return nil, false
	}
	if err := b.client.Push(frame); err != nil {
		slog.Error("pryon audio failed", "err", err)
		return nil, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for id := range b.scores {
		b.scores[id] = b.heard[id]
		delete(b.heard, id)
	}
	return b.scores, true
}

func (b *pryonBackend) close() { b.client.Close() }
