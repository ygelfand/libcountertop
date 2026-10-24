package playback

import (
	"context"
	"image"
	"io"
	"time"

	"github.com/ygelfand/libcountertop/pkg/media/cenc"
	"github.com/ygelfand/libcountertop/pkg/media/nowplaying"
)

type Branded interface {
	Logo() string
}

type Seeker interface {
	Seek(to time.Duration)
}

type Placer interface {
	Place() (at time.Duration, ok bool)
}

type Failer interface {
	Fail(err error)
}

type Backdrop interface {
	Backdrop(img *image.RGBA)
}

type Mark struct {
	From, To time.Duration
	RGB      uint32
}

type Marked interface {
	Marks() []Mark
}

type Target struct {
	Width, Height int
	Tallest       int
	Fastest       int
}

type Waiter interface {
	Waiting(w bool)
}

type Pictures interface {
	Pictured() bool
	Pictures(ctx context.Context) (webm io.ReadCloser, from time.Duration, err error)
}

type Picture struct {
	H264          bool
	Width, Height int
	Next          func(context.Context) (data []byte, at time.Duration, err error)
	Sparse        bool
	Session       uint32
	Sealed        func(context.Context) (data []byte, at time.Duration, crypt *cenc.Crypt, err error)
}

type Live interface {
	Live(ctx context.Context) (Picture, error)
}

type Attendant interface {
	Attend(s *Session)
	Failed(err error)
}

type Player interface {
	Play(src Source)
	Stop(src Source)
	Handoff(src Source)
}

type Pauser interface {
	Pause(src Source)
	Resume(src Source)
}

type Clock interface {
	Position(src Source) (at time.Duration, paused bool)
	Behind(src Source) time.Duration
}

type Output interface {
	Attendant
	Player
	Pauser
	Clock
	Changed(src Source)
}

type Session struct {
	Label    string
	Logo     string
	Pictured bool
}

type Stream interface {
	Read(ctx context.Context, pcm []int16) (int, error)
	Start() time.Duration
}

type Described interface {
	Showing() Showing
	Label() string
}

type Transport interface {
	Play()
	Pause()
	Stop()
}

type Skipper interface {
	Next()
	Previous()
}

type Source interface {
	Stream
	Described
	Transport
	Skipper
}

type Showing struct {
	Title  string
	Artist string
	Album  string

	Art string

	Mark string

	Length   time.Duration
	Earliest time.Duration
	Edge     time.Duration

	LiveWithin time.Duration

	Break *nowplaying.Break

	HasNext, HasPrevious bool
	NoPause              bool

	Upcoming []Upcoming
}

type Upcoming struct {
	Title  string
	Artist string
	Length time.Duration
	Art    string

	Play func()
}
