package webreceiver

import (
	"slices"
	"time"

	"github.com/ygelfand/libcountertop/pkg/media/nowplaying"
)

type castBreaks struct {
	Breaks []struct {
		ID           string   `json:"id"`
		Position     float64  `json:"position"`
		BreakClipIDs []string `json:"breakClipIds"`
	} `json:"breaks"`
	BreakClips []struct {
		ID       string  `json:"id"`
		Duration float64 `json:"duration"`
	} `json:"breakClips"`
}

type breakStatus struct {
	BreakID              string  `json:"breakId"`
	BreakClipID          string  `json:"breakClipId"`
	CurrentBreakClipTime float64 `json:"currentBreakClipTime"`
}

type adBreaks struct {
	at      []time.Duration
	clips   map[string][]string
	lengths map[string]time.Duration
}

func newAdBreaks(m castBreaks) adBreaks {
	a := adBreaks{clips: map[string][]string{}, lengths: map[string]time.Duration{}}
	for _, b := range m.Breaks {
		a.clips[b.ID] = b.BreakClipIDs
		if b.Position >= 0 {
			a.at = append(a.at, seconds(b.Position))
		}
	}
	slices.Sort(a.at)
	for _, c := range m.BreakClips {
		a.lengths[c.ID] = seconds(c.Duration)
	}
	return a
}

func (a adBreaks) now(s *breakStatus) *nowplaying.Break {
	if s == nil {
		return nil
	}
	b := &nowplaying.Break{At: seconds(s.CurrentBreakClipTime), Length: a.lengths[s.BreakClipID]}
	if clips := a.clips[s.BreakID]; len(clips) > 0 {
		b.Clip, b.Clips = slices.Index(clips, s.BreakClipID)+1, len(clips)
	}
	return b
}

func seconds(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }
