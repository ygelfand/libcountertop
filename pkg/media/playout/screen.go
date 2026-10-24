package playout

import (
	"context"
	"image"
	"time"

	"github.com/ygelfand/libcountertop/pkg/media/nowplaying"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

type Controls interface {
	nowplaying.Source
	Seek(to time.Duration)
	CanSeek() bool
	Marks() []playback.Mark
}

type Feed struct {
	Clock   func() (at time.Duration, paused bool)
	Started func()
	Waiting func(bool)
	Handing func() bool
}

type Pages interface {
	Hold(Controls)
	Open(Controls)
	Held() bool
	Release(handing bool)
	Leave()
}

type Notes interface {
	Say(note string)
	Fail(note string)
	Logo(raw []byte, id uint64)
	Backdrop(img *image.RGBA)
	Standby(c Controls, note string, raw []byte, id uint64)
}

type Pictures interface {
	Play(ctx context.Context, c Controls, pics playback.Pictures, f Feed) error
}

type Screen interface {
	Pages
	Notes
	Pictures
}
