package airplay

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/audio/analysis"
	"github.com/ygelfand/libcountertop/pkg/audio/resample"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	readAhead   = 250 * time.Millisecond
	silenceLead = 100 * time.Millisecond
	audioGap    = 150 * time.Millisecond
	mostAudio   = 500 * time.Millisecond
	mostSynced  = 4 * time.Second
)

var (
	_ playback.Source    = (*sound)(nil)
	_ playback.ReadAhead = (*sound)(nil)
)

type sound struct {
	client     string
	rate       int
	channels   int
	mode       schema.AirPlayAudioMode
	latency    time.Duration
	disconnect func()

	done  chan struct{}
	ended sync.Once

	mu       sync.Mutex
	quiet    time.Time
	silenced int64
	pending  []chunk
	queued   int
	heard    time.Time
	from     int
	convert  *resample.Rational
}

type chunk struct {
	at  time.Time
	pcm []int16
}

func newSound(
	client string,
	rate, channels int,
	mode schema.AirPlayAudioMode,
	disconnect func(),
) *sound {
	return &sound{
		client:     client,
		rate:       rate,
		channels:   channels,
		mode:       mode,
		disconnect: disconnect,
		done:       make(chan struct{}),
	}
}

func (s *sound) hear(a audio) {
	pcm := remix(a.pcm, a.channels, s.channels)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.rate != s.rate {
		if s.convert == nil || s.from != a.rate {
			s.convert, s.from = resample.NewRational(a.rate, s.rate, s.channels), a.rate
		}
		pcm = s.convert.Run(pcm)
	}
	c := chunk{pcm: pcm}
	most := mostAudio
	if s.mode.Synced() {
		most = mostSynced
		if a.due > 0 {
			c.at = now.Add(a.due - readAhead - s.latency)
		}
	}
	s.pending = append(s.pending, c)
	s.queued += len(pcm)
	limit := int(most*time.Duration(s.rate)/time.Second) * s.channels
	for s.queued > limit {
		over, first := s.queued-limit, &s.pending[0]
		if len(first.pcm) > over {
			first.pcm = first.pcm[over:]
			s.queued = limit
			break
		}
		s.queued -= len(first.pcm)
		s.pending = s.pending[1:]
	}
	s.heard = now
}

func (s *sound) take(pcm []int16, now time.Time) int {
	n := 0
	for n < len(pcm) && len(s.pending) > 0 {
		c := &s.pending[0]
		if !c.at.IsZero() && now.Before(c.at) {
			break
		}
		k := copy(pcm[n:], c.pcm)
		n += k
		s.queued -= k
		c.pcm = c.pcm[k:]
		if len(c.pcm) == 0 {
			s.pending = s.pending[1:]
		}
	}
	return n
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
	if n := s.take(pcm[:len(pcm)/s.channels*s.channels], now); n > 0 {
		s.quiet = time.Time{}
		return n, nil
	}
	if len(s.pending) > 0 || (!s.heard.IsZero() && now.Sub(s.heard) < audioGap) {
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

func (s *sound) ReadAhead() time.Duration { return readAhead }

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
