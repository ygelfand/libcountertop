package schema

import "github.com/ygelfand/libcountertop/pkg/say"

type TimerStyle string

const (
	TimerBar    TimerStyle = "bar"
	TimerBanner TimerStyle = "banner"
	TimerDial   TimerStyle = "dial"
	TimerFull   TimerStyle = "full"
	TimerFace   TimerStyle = "face"
	TimerCorner TimerStyle = "corner"
	// TimerSplit shows every timer at once: 1 full, 2 side by side, 3-4 in a 2x2 grid.
	TimerSplit TimerStyle = "split"
)

const DefaultTimerStyle = TimerBar

func (s TimerStyle) Label() string {
	switch s {
	case TimerBar:
		return say.T("timerstyle.bar")
	case TimerBanner:
		return say.T("timerstyle.banner")
	case TimerDial:
		return say.T("timerstyle.dial")
	case TimerFull:
		return say.T("timerstyle.full")
	case TimerFace:
		return say.T("timerstyle.face")
	case TimerCorner:
		return say.T("timerstyle.corner")
	case TimerSplit:
		return say.T("timerstyle.split")
	}
	return string(s)
}

func TimerStyles() []TimerStyle {
	return []TimerStyle{TimerBar, TimerBanner, TimerDial, TimerFull, TimerFace, TimerCorner, TimerSplit}
}
