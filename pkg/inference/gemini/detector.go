package gemini

import (
	"fmt"
	"os"

	"github.com/zserge/microwakeword/audiofrontend"

	"github.com/ygelfand/libcountertop/pkg/inference/tflite"
)

// Frontend settings carried in the model's own header.
const (
	windowMs  = 25
	stepMs    = 10
	channels  = 40
	lowerHz   = 125
	upperHz   = 7800
	logScale  = 64
	stacked   = 3
	rate      = 16000
	window    = rate * windowMs / 1000
	step      = rate * stepMs / 1000
	hotword   = 1
	outputIdx = 1
)

type Detector struct {
	in *tflite.Interpreter
	fe *audiofrontend.Frontend

	pending []int16
	frames  []float32
	seen    int
}

func NewDetector(path string) (*Detector, error) {
	pak, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("gemini: %w", err)
	}
	res, err := resource(pak, hotwordResource)
	if err != nil {
		return nil, err
	}
	model, err := detector(res)
	if err != nil {
		return nil, err
	}
	m, err := tflite.Parse(model)
	if err != nil {
		return nil, fmt.Errorf("gemini: %w", err)
	}
	in, err := tflite.New(m)
	if err != nil {
		return nil, fmt.Errorf("gemini: %w", err)
	}
	if got := in.Input(0).Count(); got != stacked*channels {
		return nil, fmt.Errorf("gemini: detector takes %d values, want %d", got, stacked*channels)
	}
	return &Detector{
		in: in,
		fe: audiofrontend.New(audiofrontend.Config{
			SampleRate:   rate,
			WindowSizeMs: windowMs,
			StepSizeMs:   stepMs,
			NumChannels:  channels,
			LowerFreq:    lowerHz,
			UpperFreq:    upperHz,
		}),
	}, nil
}

func (d *Detector) Process(samples []int16) (float64, bool, error) {
	d.pending = append(d.pending, samples...)
	var best float64
	scored := false
	for len(d.pending) >= window {
		for _, v := range d.fe.ProcessFrame(d.pending[:window]) {
			d.frames = append(d.frames, float32(v)/logScale)
		}
		d.pending = d.pending[step:]
		d.seen++
		if len(d.frames) > stacked*channels {
			d.frames = d.frames[len(d.frames)-stacked*channels:]
		}
		if d.seen < stacked || d.seen%stacked != 0 {
			continue
		}
		copy(d.in.Input(0).F32, d.frames)
		if err := d.in.Invoke(); err != nil {
			return 0, false, fmt.Errorf("gemini: %w", err)
		}
		out := d.in.Output(outputIdx).F32
		best = max(best, float64(out[hotword]))
		scored = true
	}
	return best, scored, nil
}

func (d *Detector) Reset() {
	d.in.Reset()
	d.fe.Reset()
	d.pending = d.pending[:0]
	d.frames = d.frames[:0]
	d.seen = 0
}
