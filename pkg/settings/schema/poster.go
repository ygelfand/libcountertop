package schema

import (
	"time"

	"github.com/ygelfand/libcountertop/pkg/say"
)

type Poster struct {
	Enabled bool            `json:"enabled"`
	Every   PosterEvery     `json:"every"`
	Fit     PhotoFit        `json:"fit"`
	Change  PhotoTransition `json:"transition"`
	Last    string          `json:"last"`
}

func DefaultPoster() Poster { return Poster{Every: PosterHourly, Fit: FitAuto, Change: TransitionFade} }

type PosterEvery string

const (
	PosterNever  PosterEvery = "never"
	PosterWake   PosterEvery = "wake"
	PosterHourly PosterEvery = "hourly"
	PosterDaily  PosterEvery = "daily"
)

var posterSpans = []Delay{Delay10s, Delay15s, Delay30s, Delay1m, Delay2m, Delay5m, Delay15m}

func PosterEveries() []PosterEvery {
	out := []PosterEvery{PosterWake}
	for _, d := range posterSpans {
		out = append(out, PosterEvery(d))
	}
	return append(out, PosterHourly, PosterDaily, PosterNever)
}

func (p PosterEvery) Label() string {
	switch p {
	case PosterNever:
		return say.T("poster.never")
	case PosterWake:
		return say.T("poster.wake")
	case PosterHourly:
		return say.T("poster.hourly")
	case PosterDaily:
		return say.T("poster.daily")
	}
	return Delay(p).Label()
}

func (p PosterEvery) Period() time.Duration {
	switch p {
	case PosterHourly:
		return time.Hour
	case PosterDaily:
		return 24 * time.Hour
	}
	return Delay(p).After()
}
