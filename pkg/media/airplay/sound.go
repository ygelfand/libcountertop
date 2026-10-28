package airplay

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/audio/analysis"
	"github.com/ygelfand/libcountertop/pkg/audio/resample"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

const (
	silenceLead = 100 * time.Millisecond
	audioGap    = 150 * time.Millisecond
	mostAudio   = 500 * time.Millisecond
)

var _ playback.Source = (*sound)(nil)

type sound struct {
	client     string
	rate       int
	channels   int
	disconnect func()

	done  chan struct{}
	ended sync.Once

	mu       sync.Mutex
	quiet    time.Time
	silenced int64
	pending  []int16
	heard    time.Time
	from     int
	convert  *resample.Rational
}

func newSound(client string, rate, channels int, disconnect func()) *sound {
	return &sound{
		client:     client,
		rate:       rate,
		channels:   channels,
		disconnect: disconnect,
		done:       make(chan struct{}),
	}
}

func (s *sound) hear(a audio) {
	pcm := remix(a.pcm, a.channels, s.channels)
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.rate != s.rate {
		if s.convert == nil || s.from != a.rate {
			s.convert, s.from = resample.NewRational(a.rate, s.rate, s.channels), a.rate
		}
		pcm = s.convert.Run(pcm)
	}
	s.pending = append(s.pending, pcm...)
	most := int(mostAudio*time.Duration(s.rate)/time.Second) * s.channels
	if len(s.pending) > most {
		s.pending = append(s.pending[:0], s.pending[len(s.pending)-most:]...)
	}
	s.heard = time.Now()
}

func (s *sound) end() { s.ended.Do(func() { close(s.done) }) }

func (s *sound) Read(ctx context.Context, pcm []int16) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	select {
	case <-s.done:
		return 0, io.EOF
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if len(s.pending) > 0 {
		n := copy(pcm[:len(pcm)/s.channels*s.channels], s.pending)
		s.pending = s.pending[n:]
		s.quiet = time.Time{}
		return n, nil
	}
	if !s.heard.IsZero() && now.Sub(s.heard) < audioGap {
		return 0, nil
	}
	if s.quiet.IsZero() {
		s.quiet, s.silenced = now, 0
	}
	due := int64((now.Sub(s.quiet) + silenceLead) * time.Duration(s.rate) / time.Second)
	n := min(int64(len(pcm)/s.channels), due-s.silenced)
	if n <= 0 {
		return 0, nil
	}
	s.silenced += n
	out := pcm[:int(n)*s.channels]
	clear(out)
	return len(out), nil
}

func (s *sound) Start() time.Duration { return 0 }

func (s *sound) Showing() playback.Showing {
	return playback.Showing{Title: s.client, NoPause: true}
}

func (s *sound) Label() string { return "AirPlay" }

func (s *sound) Play()     {}
func (s *sound) Pause()    {}
func (s *sound) Stop()     { s.disconnect() }
func (s *sound) Next()     {}
func (s *sound) Previous() {}

func remix(pcm []int16, from, to int) []int16 {
	if from == to {
		return pcm
	}
	mono := analysis.Mono(pcm, from, nil)
	if to == 1 {
		return mono
	}
	out := make([]int16, 0, len(mono)*to)
	for _, v := range mono {
		for range to {
			out = append(out, v)
		}
	}
	return out
}
