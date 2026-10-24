package nowplaying

import (
	"time"
)

type Now struct {
	Playing bool
	Paused  bool

	Hold bool

	Title  string
	Artist string
	Album  string

	Art   []byte
	ArtID uint64

	Mark   []byte
	MarkID uint64

	Elapsed  time.Duration
	Length   time.Duration
	Earliest time.Duration
	Edge     time.Duration

	LiveWithin time.Duration

	Break *Break

	Queue []Track

	Can Controls
}

type Break struct {
	Clip, Clips int
	At, Length  time.Duration
}

type Track struct {
	Title  string
	Artist string
	Length time.Duration

	Art   []byte
	ArtID uint64

	Play func()
}

type Seeker interface {
	Seek(to time.Duration)
	CanSeek() bool
}

type Opener interface {
	Open() bool
}

type Controls uint8

const (
	CanPause Controls = 1 << iota
	CanStop
	CanNext
	CanPrevious
)

func (c Controls) Has(want Controls) bool { return c&want == want }

type Kind uint8

const (
	FromQueue Kind = iota
	FromBluetooth
	FromGroup
	FromCast
)

type Details interface {
	Now() Now
	Next()
	Previous()

	Kind() Kind

	Label() string
}

type Controller interface {
	Play()
	Pause()
	Stop()
	Next()
	Previous()
}

type Source interface {
	Controller
	Details
}
