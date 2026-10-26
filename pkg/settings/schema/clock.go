package schema

import (
	"time"

	"github.com/ygelfand/libcountertop/pkg/say"
)

type Clock struct {
	Face Face `json:"face"`

	Date bool `json:"date"`

	Placement

	Ink Ink `json:"ink"`
}

type Placement struct {
	Position Position `json:"position"`
	Align    Align    `json:"align"`
	Size     Size     `json:"size"`
}

func (p Placement) TopRight() bool { return p.Position == PositionTop && p.Align == AlignRight }

type Delay string

const (
	DelayNever Delay = "never"
	Delay5s    Delay = "5s"
	Delay10s   Delay = "10s"
	Delay15s   Delay = "15s"
	Delay30s   Delay = "30s"
	Delay45s   Delay = "45s"
	Delay1m    Delay = "1m"
	Delay2m    Delay = "2m"
	Delay3m    Delay = "3m"
	Delay5m    Delay = "5m"
	Delay10m   Delay = "10m"
	Delay15m   Delay = "15m"
	Delay30m   Delay = "30m"
)

const DefaultDelay = Delay1m

func (d Delay) Label() string {
	switch d {
	case DelayNever:
		return say.T("delay.never")
	case Delay5s:
		return say.T("delay.5s")
	case Delay10s:
		return say.T("delay.10s")
	case Delay15s:
		return say.T("delay.15s")
	case Delay30s:
		return say.T("delay.30s")
	case Delay45s:
		return say.T("delay.45s")
	case Delay1m:
		return say.T("delay.1m")
	case Delay2m:
		return say.T("delay.2m")
	case Delay3m:
		return say.T("delay.3m")
	case Delay5m:
		return say.T("delay.5m")
	case Delay10m:
		return say.T("delay.10m")
	case Delay15m:
		return say.T("delay.15m")
	case Delay30m:
		return say.T("delay.30m")
	}
	return string(d)
}

func (d Delay) After() time.Duration {
	switch d {
	case Delay5s:
		return 5 * time.Second
	case Delay10s:
		return 10 * time.Second
	case Delay15s:
		return 15 * time.Second
	case Delay30s:
		return 30 * time.Second
	case Delay45s:
		return 45 * time.Second
	case Delay1m:
		return time.Minute
	case Delay2m:
		return 2 * time.Minute
	case Delay3m:
		return 3 * time.Minute
	case Delay5m:
		return 5 * time.Minute
	case Delay10m:
		return 10 * time.Minute
	case Delay15m:
		return 15 * time.Minute
	case Delay30m:
		return 30 * time.Minute
	}
	return 0
}

func Delays() []Delay {
	return []Delay{Delay30s, Delay1m, Delay2m, Delay5m, Delay15m, DelayNever}
}

func MediaDelays() []Delay {
	return []Delay{Delay1m, Delay3m, Delay5m, Delay10m, Delay15m, DelayNever}
}

type Position string

const (
	PositionTop    Position = "top"
	PositionCenter Position = "center"
	PositionBottom Position = "bottom"
	PositionRandom Position = "random"
)

const DefaultPosition = PositionCenter

func (p Position) Label() string {
	switch p {
	case PositionTop:
		return say.T("position.top")
	case PositionCenter:
		return say.T("position.center")
	case PositionBottom:
		return say.T("position.bottom")
	case PositionRandom:
		return say.T("position.random")
	}
	return string(p)
}

func Positions() []Position {
	return []Position{PositionTop, PositionCenter, PositionBottom}
}

type Size string

const (
	SizeNano   Size = "nano"
	SizeMicro  Size = "micro"
	SizeMini   Size = "mini"
	SizeSmall  Size = "small"
	SizeMedium Size = "medium"
	SizeLarge  Size = "large"
)

const DefaultSize = SizeLarge

func (s Size) Label() string {
	switch s {
	case SizeNano:
		return say.T("size.nano")
	case SizeMicro:
		return say.T("size.micro")
	case SizeMini:
		return say.T("size.mini")
	case SizeSmall:
		return say.T("size.small")
	case SizeMedium:
		return say.T("size.medium")
	case SizeLarge:
		return say.T("size.large")
	}
	return string(s)
}

func (s Size) Share() float64 {
	switch s {
	case SizeNano:
		return 0.16
	case SizeMicro:
		return 0.26
	case SizeMini:
		return 0.40
	case SizeSmall:
		return 0.58
	case SizeMedium:
		return 0.78
	}
	return 1
}

func Sizes() []Size {
	return []Size{SizeNano, SizeMicro, SizeMini, SizeSmall, SizeMedium, SizeLarge}
}

const DefaultFace Face = "plain"

const DefaultDate = true

func DefaultClock() Clock {
	return Clock{
		Face:      DefaultFace,
		Date:      DefaultDate,
		Placement: Placement{Position: DefaultPosition, Align: AlignCenter, Size: DefaultSize},
		Ink:       DefaultInk,
	}
}

type Face string

const FaceNone Face = "none"

func (f Face) Label() string { return say.T("face." + string(f)) }
