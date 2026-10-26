package schema

import "github.com/ygelfand/libcountertop/pkg/say"

type Idle struct {
	After Delay `json:"after"`
	Media Delay `json:"media"`
	Face  Face  `json:"face"`
	Placement
	First  IdleVisual `json:"first"`
	Second IdleVisual `json:"second"`
	Saver  SaverMode  `json:"saver"`

	PhotoEvery  Delay           `json:"photo_every"`
	PhotoFit    PhotoFit        `json:"photo_fit"`
	PhotoChange PhotoTransition `json:"photo_transition"`

	Night      Night `json:"night"`
	NightLux   int   `json:"night_lux"`
	NightAfter Delay `json:"night_after"`
}

func (c Idle) Tessera() bool { return c.Saver == SaverTessera }

type IdleVisual struct {
	Kind   string `json:"kind"`
	Source Source `json:"source"`
}

const IdlePhotos = "photos"

func (v IdleVisual) On() bool { return v.Kind != "" }

func (v IdleVisual) Photos() bool { return v.Kind == IdlePhotos }

func IdlePhotoSpans() []Delay {
	return []Delay{Delay10s, Delay15s, Delay30s, Delay1m, Delay2m, Delay5m}
}

func IdlePositions() []Position { return append(Positions(), PositionRandom) }

func IdleAligns() []Align { return append(Aligns(), AlignRandom) }

type Align string

const (
	AlignLeft   Align = "left"
	AlignCenter Align = "center"
	AlignRight  Align = "right"
	AlignRandom Align = "random"
)

func (a Align) Label() string {
	switch a {
	case AlignLeft:
		return say.T("align.left")
	case AlignCenter:
		return say.T("align.center")
	case AlignRight:
		return say.T("align.right")
	case AlignRandom:
		return say.T("align.random")
	}
	return string(a)
}

func Aligns() []Align { return []Align{AlignLeft, AlignCenter, AlignRight} }

type Source string

const (
	SourceBoth    Source = "both"
	SourceMic     Source = "mic"
	SourceSpeaker Source = "speaker"
)

func (s Source) Label() string {
	switch s {
	case SourceBoth:
		return say.T("source.both")
	case SourceMic:
		return say.T("source.mic")
	case SourceSpeaker:
		return say.T("source.speaker")
	}
	return string(s)
}

func Sources() []Source { return []Source{SourceBoth, SourceMic, SourceSpeaker} }

func DefaultIdle() Idle {
	return Idle{
		After:       DefaultDelay,
		Media:       Delay5m,
		Face:        DefaultFace,
		Placement:   Placement{Position: PositionRandom, Align: AlignRandom, Size: DefaultSize},
		First:       IdleVisual{Source: SourceBoth},
		Second:      IdleVisual{Source: SourceBoth},
		Saver:       SaverNative,
		Night:       NightClock,
		NightLux:    DefaultNightLux,
		NightAfter:  DefaultNightAfter,
		PhotoEvery:  Delay30s,
		PhotoFit:    FitKenBurns,
		PhotoChange: TransitionFade,
	}
}
