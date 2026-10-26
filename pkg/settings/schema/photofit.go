package schema

import "github.com/ygelfand/libcountertop/pkg/say"

type PhotoFit string

const (
	FitAuto     PhotoFit = "auto"
	FitCover    PhotoFit = "cover"
	FitContain  PhotoFit = "contain"
	FitBars     PhotoFit = "bars"
	FitFramed   PhotoFit = "framed"
	FitKenBurns PhotoFit = "kenburns"
)

func PhotoFits() []PhotoFit {
	return []PhotoFit{FitAuto, FitCover, FitContain, FitBars, FitFramed, FitKenBurns}
}

func (f PhotoFit) Label() string { return say.T("photofit." + string(f)) }

type PhotoTransition string

const (
	TransitionCut      PhotoTransition = "cut"
	TransitionFade     PhotoTransition = "fade"
	TransitionSlide    PhotoTransition = "slide"
	TransitionZoom     PhotoTransition = "zoom"
	TransitionWipe     PhotoTransition = "wipe"
	TransitionDissolve PhotoTransition = "dissolve"
)

func PhotoTransitions() []PhotoTransition {
	return []PhotoTransition{
		TransitionCut,
		TransitionFade,
		TransitionSlide,
		TransitionZoom,
		TransitionWipe,
		TransitionDissolve,
	}
}

func (t PhotoTransition) Label() string { return say.T("phototransition." + string(t)) }

type ImmichQuality string

const (
	ImmichPreview  ImmichQuality = "preview"
	ImmichFullsize ImmichQuality = "fullsize"
)

func ImmichQualities() []ImmichQuality { return []ImmichQuality{ImmichPreview, ImmichFullsize} }

func (q ImmichQuality) Label() string { return say.T("photos.quality." + string(q)) }
