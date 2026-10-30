package detect

import (
	"log/slog"

	"github.com/ygelfand/libcountertop/pkg/inference/wake"
)

type Slots interface {
	Use(int, wake.Model) error
	Clear(int)
	Loaded(int) (wake.Model, bool)
}

func Choose(
	engine Slots,
	models []wake.Model,
	ids []string,
	count int,
) (accepted []string, loaded []int) {
	accepted = make([]string, count)
	for slot := range count {
		if slot >= len(ids) || ids[slot] == "" {
			engine.Clear(slot)
			continue
		}

		m, ok := wake.Find(models, ids[slot])
		if !ok {
			slog.Warn("a chosen wake word is not on the device", "slot", slot+1, "id", ids[slot])
		} else if err := engine.Use(slot, m); err != nil {
			slog.Error("loading a wake word failed", "slot", slot+1, "id", m.ID, "err", err)
		} else {
			accepted[slot] = m.ID
			loaded = append(loaded, slot)
			continue
		}

		if was, ok := engine.Loaded(slot); ok {
			slog.Info("keeping the wake word already running", "slot", slot+1, "id", was.ID)
			accepted[slot] = was.ID
			loaded = append(loaded, slot)
		}
	}
	return accepted, loaded
}

func LogInstalled(ours []wake.Model) {
	slog.Info("wake words installed", "count", len(ours),
		"openwakeword", len(wake.OfKind(ours, wake.KindOpenWakeWord)),
		"microwakeword", len(wake.OfKind(ours, wake.KindMicroWakeWord)),
		"pryon", len(wake.OfKind(ours, wake.KindPryon)),
		"gemini", len(wake.OfKind(ours, wake.KindGemini)))
}
