package detect

import (
	"context"
	"log/slog"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/inference/wake"
	"github.com/ygelfand/libcountertop/pkg/inference/wakeassets"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
)

const StopSlot = 100

type StopOptions struct {
	Directory string
	DeviceID  uint32
	Read      func() float64
	Save      func(float64) error

	PryonOn    func() bool
	PryonModel func(context.Context) (wake.Model, error)
}

const pryonStopWait = 2 * time.Minute

type StopWord struct {
	Entity  *esphome.Number
	engine  Slots
	options StopOptions
}

func NewStopWord(engine Slots, options StopOptions) *StopWord {
	s := &StopWord{engine: engine, options: options}
	s.Entity = &esphome.Number{
		Base: esphome.Base{
			ObjectID: "stop_word_sensitivity",
			DeviceID: options.DeviceID,
			Name:     "Stop word sensitivity",
			Icon:     "mdi:hand-back-left",
			Category: esphome.CategoryConfig,
		},
		Min:  0.5,
		Max:  1,
		Step: 0.01,
		Mode: esphome.NumberBox,
	}
	s.Entity.OnCommand = func(v float32) {
		err := options.Save(float64(v))
		s.Restore(options.Read())
		s.Load()
		if err != nil {
			slog.Error("saving the stop threshold failed", "err", err)
		}
	}
	return s
}

func (s *StopWord) Restore(threshold float64) { s.Entity.Set(float32(threshold)) }

func (s *StopWord) Load() {
	threshold := s.options.Read()
	if threshold >= 1 {
		s.engine.Clear(StopSlot)
		slog.Info("stop word off", "threshold", threshold)
		return
	}
	if s.options.PryonOn != nil && s.options.PryonOn() {
		safe.Go("loading the pryon stop word", s.loadPryon)
		return
	}
	path, err := wakeassets.Stop(s.options.Directory)
	if err == nil {
		m := wake.Model{
			ID:     "stop",
			Phrase: wakeassets.StopPhrase,
			Kind:   wake.KindMicroWakeWord,
			Path:   path,
		}
		m.Config.ModelPath = path
		m.Config.SlidingWindowSize = wakeassets.StopWindowSize
		m.Config.FeaturesStepMs = wakeassets.StopFeatureStep
		err = s.engine.Use(StopSlot, m)
	}
	if err != nil {
		s.engine.Clear(StopSlot)
		slog.Error("loading the stop word failed", "err", err)
		return
	}
	slog.Info("stop word listening", "threshold", threshold)
}

func (s *StopWord) loadPryon() {
	ctx, cancel := context.WithTimeout(context.Background(), pryonStopWait)
	defer cancel()
	m, err := s.options.PryonModel(ctx)
	if err == nil && s.options.PryonOn() {
		err = s.engine.Use(StopSlot, m)
	}
	if err != nil {
		slog.Error("loading the pryon stop word failed", "err", err)
		return
	}
	slog.Info("stop word listening", "engine", m.Kind, "threshold", s.options.Read())
}
