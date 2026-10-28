package urlplayer

import (
	"context"
	"time"

	"github.com/ygelfand/libcountertop/pkg/media/hls"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

const HLS = "hls"

type hlsAudio struct {
	s   *hls.Stream
	buf []int16
}

func (d *hlsAudio) rate() int { return outRate }

func (d *hlsAudio) next() ([]int16, error) {
	if d.buf == nil {
		d.buf = make([]int16, readFrames*outChannels)
	}
	n, err := d.s.Read(d.buf)
	if n == 0 {
		return nil, err
	}
	return append([]int16(nil), d.buf[:n]...), nil
}

type hlsCloser struct{ s *hls.Stream }

func (c hlsCloser) Close() error {
	c.s.Close()
	return nil
}

func (t *track) openHLS(ctx context.Context) error {
	o := hls.Options{HTTP: t.client, Resample: t.resample, At: t.at}
	if t.target != nil {
		limits := t.target()
		o.Tallest, o.Fastest = limits.Tallest, limits.Fastest
	}
	s, err := hls.Open(ctx, t.media.URL(), o)
	if err != nil {
		return err
	}
	if !t.hold(hlsCloser{s}) {
		s.Close()
		return context.Canceled
	}
	t.at, t.seekable = s.Start(), s.Ended()
	t.dec = &hlsAudio{s: s}
	t.convert = func(p []int16) []int16 { return p }
	if t.screen && s.Width > 0 {
		start := t.at
		t.picture = playback.Picture{
			Codec: playback.H264, Width: s.Width, Height: s.Height,
			Next: func(ctx context.Context) ([]byte, time.Duration, error) {
				f, err := s.Next(ctx)
				return f.Data, f.At + start, err
			},
		}
	}
	return nil
}
