package callview

import (
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/audio/volume"
	"github.com/ygelfand/libcountertop/pkg/display/apps"
	"github.com/ygelfand/libcountertop/pkg/display/navigation"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/media/call"
	"github.com/ygelfand/libcountertop/pkg/network/peer"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type Peers interface {
	Peers() []peer.Peer
	Find(id string) (peer.Peer, bool)
}

type Talk struct{}

func (*Talk) Covers() bool { return true }

func (*Talk) Wakes() bool { return true }

func (*Talk) Keeps() bool { return true }

func (*Talk) Shows(s schema.Stream) bool { return s == schema.StreamVoice }

type Profile struct{ ID string }

func (*Profile) Covers() bool { return true }

type Directory struct{}

func (*Directory) Covers() bool { return true }

type Stage struct {
	shell *shell.Shell
	talk  Talk
}

func NewStage(s *shell.Shell) *Stage { return &Stage{shell: s} }

func (st *Stage) Hold() func() { return st.shell.Hold(&st.talk).Release }

func (st *Stage) Redraw() { st.shell.Redraw() }

type Options struct {
	UI    *widgets.UI
	Shell *shell.Shell
	Calls *call.Calls
	Peers Peers
	Voice volume.Channel
	Video bool
}

type View struct{ o Options }

func New(o Options) *View { return &View{o: o} }

func (cv *View) Screen(v shell.View) *navigation.Screen {
	switch t := v.(type) {
	case *Talk:
		return cv.callScreen(t)
	case *Profile:
		return cv.profileScreen(t)
	case *Directory:
		return cv.directoryScreen(t)
	}
	return nil
}

func (cv *View) App() apps.App {
	return apps.App{
		ID:   schema.AppCalls,
		Name: func() string { return say.T("call.tab") },
		Icon: "mdi:home-sound-in",
		Open: cv.browse,
	}
}

func (cv *View) browse() { cv.o.Shell.Push(&Directory{}) }

func (cv *View) open(p peer.Peer) { cv.o.Shell.Push(&Profile{ID: p.ID}) }

func (cv *View) directoryScreen(d *Directory) *navigation.Screen {
	return &navigation.Screen{
		Title: say.T("call.tab"),
		View:  d,
		Build: func(w *gogui.Window) gogui.View {
			_, vh := w.WindowSize()
			at := ui.Rect{W: int(widgets.PageWidth(w)), H: vh}
			return cv.callsBoard(at, cv.o.UI.Palette())
		},
	}
}

func iconStyle(c gogui.Color) gogui.TextStyle {
	st := gogui.CurrentTheme().TextStyleIconMedium
	st.Color = c
	return st
}

func reach() float32 { return style.Reach() }
