package detect

import (
	"log/slog"

	"github.com/ygelfand/libcountertop/pkg/inference/gemini"
	"github.com/ygelfand/libcountertop/pkg/inference/wake"
)

type geminiBackend struct {
	dets   map[string]*gemini.Detector
	scores map[string]float64
}

func (b *geminiBackend) load(key string, m wake.Model, _ int) error {
	det, err := gemini.NewDetector(m.Path)
	if err != nil {
		return err
	}
	b.dets[key] = det
	return nil
}

func (b *geminiBackend) unload(key string) {
	delete(b.dets, key)
	delete(b.scores, key)
}

func (b *geminiBackend) feed(frame []int16) (map[string]float64, bool) {
	fresh := false
	for key, det := range b.dets {
		score, scored, err := det.Process(frame)
		if err != nil {
			slog.Error("gemini scoring failed", "err", err)
			return nil, false
		}
		if scored {
			b.scores[key] = score
			fresh = true
		}
	}
	return b.scores, fresh
}

func (b *geminiBackend) close() { clear(b.dets) }
