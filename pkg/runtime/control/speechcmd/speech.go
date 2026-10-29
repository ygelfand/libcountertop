package speechcmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/ygelfand/libcountertop/pkg/audio/capture"
	"github.com/ygelfand/libcountertop/pkg/inference/asr"
)

const (
	frame    = 320
	trailing = 2 * time.Second
	limit    = 2 * time.Minute
)

type Prepare func(ctx context.Context) (asr.Options, error)

func Command(prepare Prepare) *cobra.Command {
	return &cobra.Command{
		Use:   "transcribe WAV [PHRASE...]",
		Short: "Play a 16 kHz mono WAV into the speech recognizer at real-time pace and print what it heard",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			samples, rate, channels, err := capture.Read(args[0])
			if err != nil {
				return err
			}
			if rate != asr.Rate || channels != 1 {
				return fmt.Errorf("a 16 kHz mono WAV, not %d Hz with %d channels", rate, channels)
			}
			ctx, cancel := context.WithTimeout(c.Context(), limit)
			defer cancel()
			o, err := prepare(ctx)
			if err != nil {
				return err
			}
			return run(ctx, c, o, samples, args[1:])
		},
	}
}

type file struct {
	samples  []int16
	ended    chan time.Time
	finished chan struct{}
	once     sync.Once
}

func (f *file) Listen(string) (<-chan []int16, func()) {
	out := make(chan []int16)
	stop := make(chan struct{})
	f.once.Do(func() { go f.play(out, stop) })
	return out, func() { close(stop) }
}

func (f *file) play(out chan<- []int16, stop <-chan struct{}) {
	tick := time.NewTicker(frame * time.Second / asr.Rate)
	defer tick.Stop()
	send := func(s []int16) bool {
		for len(s) > 0 {
			n := min(frame, len(s))
			select {
			case <-stop:
				return false
			case <-tick.C:
			}
			select {
			case <-stop:
				return false
			case out <- s[:n]:
			}
			s = s[n:]
		}
		return true
	}
	if !send(f.samples) {
		return
	}
	f.ended <- time.Now()
	if send(make([]int16, int(trailing.Seconds()*asr.Rate))) {
		close(f.finished)
	}
}

func run(
	ctx context.Context,
	c *cobra.Command,
	o asr.Options,
	samples []int16,
	phrases []string,
) error {
	src := &file{samples: samples, ended: make(chan time.Time, 1), finished: make(chan struct{})}
	o.Source = src
	client := asr.New(o)
	if len(phrases) > 0 {
		if err := client.Keywords(phrases); err != nil {
			return err
		}
	}
	finals := make(chan asr.Transcript, 8)
	defer client.Final.Listen(func(t asr.Transcript) { finals <- t })()
	defer client.Partial.Listen(func(s string) { c.Printf("partial: %s\n", s) })()
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
			c.PrintErrf("the recognizer stopped: %v\n", err)
		}
	}()
	c.Printf("threads %d, %.1f s of audio\n", o.Threads, float64(len(samples))/asr.Rate)
	var ended time.Time
	var heard []string
	for {
		select {
		case ended = <-src.ended:
		case t := <-finals:
			heard = append(heard, t.Text)
			after := "-"
			if !ended.IsZero() {
				after = time.Since(ended).Round(time.Millisecond).String()
			}
			c.Printf(
				"final: %s\n  utterance %v, flush %v, after the audio ended %s\n",
				t.Text,
				t.Audio,
				t.Took,
				after,
			)
		case <-src.finished:
			if len(heard) == 0 {
				return errors.New("no speech heard")
			}
			c.Printf("heard: %s\n", strings.Join(heard, " "))
			return nil
		case err := <-done:
			done <- err
			return errors.Join(errors.New("the recognizer stopped"), err)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
