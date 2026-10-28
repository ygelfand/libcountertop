package airplay

import (
	"context"
	"io"
	"time"

	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

const frameDepth = 64

var (
	_ playback.Source   = (*mirror)(nil)
	_ playback.Pictures = (*mirror)(nil)
	_ playback.Live     = (*mirror)(nil)
)

type mirror struct {
	*sound
	size   mirroring
	frames chan frame
	first  time.Duration
}

func newMirror(client string, size mirroring, rate, channels int, disconnect func()) *mirror {
	return &mirror{
		sound:  newSound(client, rate, channels, disconnect),
		size:   size,
		frames: make(chan frame, frameDepth),
		first:  -1,
	}
}

func (m *mirror) push(ctx context.Context, f frame) {
	select {
	case m.frames <- f:
	case <-m.done:
	case <-ctx.Done():
	}
}

func (m *mirror) Pictured() bool { return true }

func (m *mirror) Pictures(context.Context) (io.ReadCloser, time.Duration, error) {
	return nil, 0, nil
}

func (m *mirror) Live(context.Context) (playback.Picture, error) {
	return playback.Picture{
		H264:   true,
		Width:  int(m.size.width),
		Height: int(m.size.height),
		Next:   m.next,
		Sparse: true,
	}, nil
}

func (m *mirror) next(ctx context.Context) ([]byte, time.Duration, error) {
	select {
	case f := <-m.frames:
		m.mu.Lock()
		if m.first < 0 {
			m.first = f.at
		}
		at := f.at - m.first
		m.mu.Unlock()
		return f.data, at, nil
	case <-m.done:
		return nil, 0, io.EOF
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}
}
