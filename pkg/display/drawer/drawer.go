package drawer

import (
	"cmp"
	"slices"
	"sync"

	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/input/touch"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	OrderSettings  = 30
	OrderApps      = 35
	OrderFavorites = 40
	OrderIdle      = 90
)

type Entry struct {
	Name func() string

	Order int

	Icon string

	Open func()
}

func (e Entry) Label() string {
	if e.Name == nil {
		return ""
	}
	return e.Name()
}

type Options struct {
	Shell *shell.Shell
	Edge  func() schema.Edge
	Hide  func() schema.Delay
}

type Rail struct {
	opts Options

	mu      sync.Mutex
	entries []Entry
	lists   []func() []Entry
}

func New(opts Options) *Rail { return &Rail{opts: opts} }

func (r *Rail) Name() string { return "drawer" }

func (r *Rail) Add(e Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, e)
}

func (r *Rail) AddList(list func() []Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lists = append(r.lists, list)
}

func (r *Rail) Entries() []Entry {
	r.mu.Lock()
	out := append([]Entry(nil), r.entries...)
	lists := slices.Clone(r.lists)
	r.mu.Unlock()
	for _, list := range lists {
		out = append(out, list()...)
	}

	slices.SortStableFunc(out, func(a, b Entry) int {
		if v := cmp.Compare(a.Order, b.Order); v != 0 {
			return v
		}
		return cmp.Compare(a.Label(), b.Label())
	})
	return out
}

func (r *Rail) Gesture(g touch.Gesture) {
	s := r.opts.Shell
	if !s.Docked() || s.Closing() {
		return
	}
	if g.Kind != touch.Swipe || g.From != AsTouch(r.opts.Edge()) {
		return
	}
	if shown, _ := s.Dock(); !shown {
		s.ShowDock(r.opts.Hide().After())
	}
}

func (r *Rail) Settle() {
	s := r.opts.Shell
	never := r.opts.Hide().After() == 0
	switch _, pinned := s.Dock(); {
	case never && !pinned:
		s.ShowDock(0)
	case !never && pinned:
		s.HideDock()
	}
}

func AsTouch(e schema.Edge) touch.Edge {
	switch e {
	case schema.EdgeLeft:
		return touch.Left
	case schema.EdgeTop:
		return touch.Top
	case schema.EdgeBottom:
		return touch.Bottom
	}
	return touch.Right
}
