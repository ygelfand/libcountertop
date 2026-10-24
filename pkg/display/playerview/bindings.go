package playerview

import (
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/audio/volume"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	videoplayer "github.com/ygelfand/libcountertop/pkg/display/videopage"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/media/nowplaying"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type Player interface {
	Now() nowplaying.Now
	Named() string
	Transport() nowplaying.Controller
	Open()
}
type Dependencies struct {
	Player Player
	Volume volume.Channel
	Shell  *shell.Shell
	UI     *widgets.UI
	Dock   func() schema.Edge
}
type Renderer struct {
	options Dependencies
	kit     widgets.Toolkit
	seeking scrub
}

func New(o Dependencies) *Renderer { return &Renderer{options: o, kit: o.UI.Toolkit} }
func (p *Renderer) Close() {
	p.seeking.mu.Lock()
	if p.seeking.timer != nil {
		p.seeking.timer.Stop()
	}
	p.seeking.sk = nil
	p.seeking.mu.Unlock()
}
func reach() float32 { return style.Reach() }
func iconStyle(c gogui.Color) gogui.TextStyle {
	st := gogui.CurrentTheme().TextStyleIconMedium
	st.Color = c
	return st
}
func outOfReach(length, from, to time.Duration) style.Span {
	return style.Span{
		From:  float32(from) / float32(length),
		To:    float32(to) / float32(length),
		Color: gogui.Color{R: 0x20, G: 0x20, B: 0x20, A: 0xff},
	}
}

const tick = 0.006

func spans(marks []videoplayer.Mark, length time.Duration) []style.Span {
	if length <= 0 {
		return nil
	}
	out := make([]style.Span, 0, len(marks))
	for _, m := range marks {
		from, to := float32(m.From)/float32(length), float32(m.To)/float32(length)
		if to-from < tick {
			from, to = min(from, 1-tick), min(from, 1-tick)+tick
		}
		out = append(out, style.Span{From: from, To: to, Color: widgets.Color(m.Color)})
	}
	return out
}
