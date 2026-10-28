package syncview

import (
	"image"
	"log/slog"
	"slices"
	"sync"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/navigation"
	"github.com/ygelfand/libcountertop/pkg/display/peerboard"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/network/peer"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/configsync"
)

type Peers interface {
	Peers() []peer.Peer
}

type Directory struct{}

func (*Directory) Covers() bool { return true }

type Pairing struct{}

func (*Pairing) Covers() bool { return true }

func (*Pairing) Wakes() bool { return true }

type Stage struct {
	shell   *shell.Shell
	pairing Pairing
}

func NewStage(s *shell.Shell) *Stage { return &Stage{shell: s} }

func (st *Stage) Hold() func() { return st.shell.Hold(&st.pairing).Release }

func (st *Stage) Redraw() { st.shell.Redraw() }

type Options struct {
	UI    *widgets.UI
	Shell *shell.Shell
	Sync  *configsync.Sync
	Peers Peers
	Logo  func() image.Image
}

type View struct {
	o Options

	pad *widgets.Keypad

	mu    sync.Mutex
	round string
}

func New(o Options) *View {
	return &View{o: o, pad: &widgets.Keypad{ID: "sync", Redraw: o.Shell.Redraw}}
}

func (v *View) Page() shell.View { return &Directory{} }

func (v *View) Screen(sv shell.View) *navigation.Screen {
	switch t := sv.(type) {
	case *Directory:
		return v.directory(t)
	case *Pairing:
		return &navigation.Screen{Fixed: true, Clear: true, View: t, Build: v.pairing}
	}
	return nil
}

func (v *View) directory(d *Directory) *navigation.Screen {
	return &navigation.Screen{
		Title: say.T("sync.title"),
		View:  d,
		Build: func(w *gogui.Window) gogui.View {
			_, vh := w.WindowSize()
			at := ui.Rect{W: int(widgets.PageWidth(w)), H: vh}
			return peerboard.Board{
				UI:      v.o.UI,
				ID:      "sync-peers",
				Empty:   say.T("sync.empty"),
				Actions: v.actions,
			}.View(at, v.peers(), v.o.UI.Palette())
		},
	}
}

func (v *View) peers() []peer.Peer {
	return slices.DeleteFunc(v.o.Peers.Peers(), func(p peer.Peer) bool { return !p.Syncs() })
}

func (v *View) actions(p peer.Peer) []peerboard.Action {
	return []peerboard.Action{
		{
			Name: "to",
			Icon: gogui.IconUpload,
			Do:   func(gogui.EventCtx) { v.start(p, v.o.Sync.Send) },
		},
		{
			Name: "from",
			Icon: gogui.IconDownload,
			Do:   func(gogui.EventCtx) { v.start(p, v.o.Sync.Fetch) },
		},
	}
}

func (v *View) start(p peer.Peer, begin func(peer.Peer) error) {
	if err := begin(p); err != nil {
		slog.Warn("sync not started", "with", p.Name, "err", err)
	}
}
