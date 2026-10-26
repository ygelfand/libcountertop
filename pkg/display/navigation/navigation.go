package navigation

import (
	"cmp"
	"slices"
	"sync"

	gui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/shell"
)

type Screen struct {
	Title        string
	Build        func(*gui.Window) gui.View
	Foot         func(*gui.Window) gui.View
	Settings     func()
	Clear, Fixed bool
	View         shell.View
}
type Overlay struct {
	Priority int
	Build    func(*gui.Window) gui.View
	Covers   func() bool
	Covered  func()
}
type screenEntry struct {
	once   sync.Once
	screen *Screen
}

type Nav struct {
	For      func(shell.View) *Screen
	mu       sync.Mutex
	screens  map[shell.View]*screenEntry
	overlays []*Overlay
}

func (n *Nav) resolve(stack []shell.View) []*Screen {
	n.mu.Lock()
	if n.screens == nil {
		n.screens = make(map[shell.View]*screenEntry)
	}
	for view := range n.screens {
		if !slices.Contains(stack, view) {
			delete(n.screens, view)
		}
	}
	entries := make([]*screenEntry, len(stack))
	for i, view := range stack {
		entry := n.screens[view]
		if entry == nil {
			entry = &screenEntry{}
			n.screens[view] = entry
		}
		entries[i] = entry
	}
	n.mu.Unlock()
	out := make([]*Screen, 0, len(stack))
	for i, entry := range entries {
		entry.once.Do(func() {
			if n.For != nil {
				entry.screen = n.For(stack[i])
			}
		})
		if entry.screen != nil {
			out = append(out, entry.screen)
		}
	}
	return out
}

func (n *Nav) Showing(stack []shell.View) (covering *Screen, above []*Screen) {
	shown := n.resolve(shell.Showing(stack))
	if len(shown) > 0 && shown[0].View.Covers() {
		return shown[0], shown[1:]
	}
	return nil, shown
}
func Covering(overlays []*Overlay) int {
	for i := len(overlays) - 1; i >= 0; i-- {
		if o := overlays[i]; o.Covers != nil && o.Covers() {
			return i
		}
	}
	return -1
}

func (n *Nav) Show(overlay *Overlay) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !slices.Contains(n.overlays, overlay) {
		n.overlays = append(n.overlays, overlay)
		slices.SortStableFunc(
			n.overlays,
			func(a, b *Overlay) int { return cmp.Compare(a.Priority, b.Priority) },
		)
	}
}
func (n *Nav) Hide(overlay *Overlay) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.overlays = slices.DeleteFunc(n.overlays, func(x *Overlay) bool { return x == overlay })
}
func (n *Nav) Overlays() []*Overlay {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.overlays)
}

func BackSweep(from [2]int, x, y, width int) bool {
	dx, dy := from[0]-x, from[1]-y
	if dy < 0 {
		dy = -dy
	}
	return from[0] >= width-24 && dx >= 120 && dx > dy
}
