package noiselayer

import (
	"fmt"
	"log/slog"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/audio/noise"
)

const Off = "None"

const count = 2

type Stream interface {
	PlayNoise(sounds ...string)
	Noise() []string
	Stop()
}

type Layers struct {
	stream  Stream
	selects []*esphome.Select
}

func New(stream Stream, device uint32) *Layers {
	l := &Layers{stream: stream}
	for i := 1; i <= count; i++ {
		sel := &esphome.Select{
			Base: esphome.Base{
				ObjectID: fmt.Sprintf("noise_layer_%d", i),
				DeviceID: device,
				Name:     fmt.Sprintf("White noise layer %d", i),
				Icon:     "mdi:blur",
			},
			Options: append([]string{Off}, noise.Names()...),
		}
		sel.OnCommand = func(chosen string) {
			if chosen != Off && !noise.Has(chosen) {
				slog.Warn("unknown option", "setting", sel.ObjectID, "value", chosen)
				return
			}
			sel.Set(chosen)
			l.play()
		}
		sel.Set(Off)
		l.selects = append(l.selects, sel)
	}
	return l
}

func (l *Layers) Entities() []esphome.Entity {
	out := make([]esphome.Entity, 0, len(l.selects))
	for _, sel := range l.selects {
		out = append(out, sel)
	}
	return out
}

func (l *Layers) Sync() {
	if l == nil || len(l.stream.Noise()) > 0 {
		return
	}
	for _, sel := range l.selects {
		if sel.Get() != Off {
			sel.Set(Off)
		}
	}
}

func (l *Layers) play() {
	var sounds []string
	for _, sel := range l.selects {
		if chosen := sel.Get(); chosen != Off && chosen != "" {
			sounds = append(sounds, chosen)
		}
	}
	if len(sounds) > 0 {
		l.stream.PlayNoise(sounds...)
		return
	}
	if len(l.stream.Noise()) > 0 {
		l.stream.Stop()
	}
}
