package shell

import (
	"slices"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/hook"
)

const (
	DockTimeout     = 15 * time.Second
	SettingsTimeout = 45 * time.Second

	settling = 400 * time.Millisecond
)

type View interface {
	Covers() bool
}

type Timeouter interface {
	Timeout() time.Duration
}

type Sleeper interface{ Asleep() bool }

type Waker interface{ Wakes() bool }

type Keeper interface{ Keeps() bool }

type Docker interface{ Docks() bool }

type Shell struct {
	mu         sync.Mutex
	stack      []View
	visible    []View
	holds      map[View]bool
	timer      *time.Timer
	closed     time.Time
	woke       time.Time
	generation uint64
	reported   View
	pending    []notice
	notifying  bool

	dock      bool
	dockHide  time.Duration
	dockTimer *time.Timer
	dockGen   uint64

	Changed hook.Hook[Change]
	Redrawn hook.Hook[struct{}]
}

type Change struct{ From, To View }

type notice struct {
	closing []View
	change  *Change
}

type Stacked struct {
	View View
	Held bool
}

func New() *Shell { return &Shell{} }

func (s *Shell) Name() string { return "shell" }

func (s *Shell) Open() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.stack) > 0
}

func (s *Shell) Closing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.closed) < settling
}

func (s *Shell) Top() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.top()
}

func (s *Shell) top() View {
	if n := len(s.stack); n > 0 {
		return s.stack[n-1]
	}
	return nil
}

func (s *Shell) Visible(v View) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(Showing(s.stack), v)
}

func (s *Shell) Covered() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.ContainsFunc(s.stack, View.Covers)
}

func Showing(stack []View) []View {
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].Covers() {
			return stack[i:]
		}
	}
	return stack
}

func (s *Shell) Stack() []Stacked {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Stacked, len(s.stack))
	for i, v := range s.stack {
		out[i] = Stacked{View: v, Held: s.holds[v]}
	}
	return out
}

func (s *Shell) Views() []View {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.stack)
}

func (s *Shell) changed() {
	s.mu.Lock()
	previous := s.visible
	s.visible = slices.Clone(Showing(s.stack))
	n := notice{}
	for _, view := range previous {
		if !slices.Contains(s.visible, view) {
			n.closing = append(n.closing, view)
		}
	}
	if to := s.top(); to != s.reported {
		n.change = &Change{From: s.reported, To: to}
		s.reported = to
	}
	s.pending = append(s.pending, n)
	if s.notifying {
		s.mu.Unlock()
		return
	}
	s.notifying = true
	s.mu.Unlock()
	s.drain()
}

func (s *Shell) drain() {
	defer func() {
		if r := recover(); r != nil {
			s.mu.Lock()
			s.notifying, s.pending = false, nil
			s.mu.Unlock()
			panic(r)
		}
	}()
	for {
		s.mu.Lock()
		if len(s.pending) == 0 {
			s.notifying = false
			s.mu.Unlock()
			return
		}
		n := s.pending[0]
		s.pending = s.pending[1:]
		s.mu.Unlock()
		for _, view := range n.closing {
			if closer, ok := view.(interface{ Close() }); ok {
				closer.Close()
			}
		}
		if n.change != nil {
			s.Changed.Emit(*n.change)
		}
		s.Redraw()
	}
}

func (s *Shell) refuses(v View) bool {
	n := len(s.stack)
	if n == 0 || s.stack[n-1] == v {
		return false
	}
	if sl, ok := s.stack[n-1].(Sleeper); !ok || !sl.Asleep() {
		return false
	}
	if !v.Covers() {
		return false
	}
	w, ok := v.(Waker)
	return !ok || !w.Wakes()
}

func (s *Shell) Push(v View) {
	if s.add(v, false) {
		s.changed()
		s.wake()
	}
}

func (s *Shell) Hold(v View) *Hold {
	if !s.add(v, true) {
		return nil
	}
	s.changed()
	s.wake()
	return &Hold{shell: s, view: v}
}

func (s *Shell) add(v View, hold bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refuses(v) {
		return false
	}
	if hold {
		if s.holds == nil {
			s.holds = map[View]bool{}
		}
		s.holds[v] = true
	}
	if !slices.Contains(s.stack, v) {
		s.stack = append(s.stack, v)
	}
	if v.Covers() && s.dock && s.dockHide > 0 {
		s.hideDock()
	}
	return true
}

type Hold struct {
	shell *Shell
	view  View
}

func (h *Hold) Keep(on bool) {
	if h == nil {
		return
	}
	h.shell.mu.Lock()
	if on {
		if h.shell.holds == nil {
			h.shell.holds = map[View]bool{}
		}
		h.shell.holds[h.view] = true
	} else {
		delete(h.shell.holds, h.view)
	}
	h.shell.mu.Unlock()
	h.shell.wake()
}

func (h *Hold) Release() {
	if h == nil {
		return
	}
	h.shell.Remove(h.view)
}

func (h *Hold) Held() bool {
	if h == nil {
		return false
	}
	h.shell.mu.Lock()
	defer h.shell.mu.Unlock()
	return h.shell.holds[h.view]
}

func (s *Shell) Holding(v View) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.holds[v]
}

func (s *Shell) Remove(v View) {
	s.mu.Lock()
	s.stack = slices.DeleteFunc(s.stack, func(have View) bool { return have == v })
	delete(s.holds, v)
	if len(s.stack) == 0 {
		s.closed = time.Now()
		s.generation++
		if s.timer != nil {
			s.timer.Stop()
			s.timer = nil
		}
	}
	s.mu.Unlock()
	s.changed()
}

func (s *Shell) Pop() {
	s.mu.Lock()
	gone := s.top()
	s.mu.Unlock()
	if k, ok := gone.(Keeper); ok && k.Keeps() {
		return
	}
	if gone != nil {
		s.Remove(gone)
	}
}

func (s *Shell) Close() {
	s.mu.Lock()
	timer := s.timer
	s.closed = time.Now()
	s.stack, s.timer = nil, nil
	s.generation++
	clear(s.holds)
	s.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
	s.changed()
}

func (s *Shell) Redraw() { s.Redrawn.Emit(struct{}{}) }

func (s *Shell) Touch() {
	s.mu.Lock()
	if s.dock && s.dockHide > 0 {
		s.armDock()
	}
	s.mu.Unlock()
	if s.Open() {
		s.wake()
	}
}

func (s *Shell) Docked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return docks(s.stack)
}

func docks(stack []View) bool {
	shown := Showing(stack)
	if len(shown) == 0 || !shown[0].Covers() {
		return true
	}
	d, ok := shown[0].(Docker)
	return ok && d.Docks()
}

func (s *Shell) Dock() (shown, pinned bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dock, s.dock && s.dockHide == 0
}

func (s *Shell) ShowDock(hide time.Duration) {
	s.mu.Lock()
	s.dock, s.dockHide = true, hide
	s.armDock()
	s.mu.Unlock()
	s.Redraw()
}

func (s *Shell) HideDock() {
	s.mu.Lock()
	s.hideDock()
	s.mu.Unlock()
	s.Redraw()
}

func (s *Shell) hideDock() {
	s.dock = false
	s.stopDock()
}

func (s *Shell) armDock() {
	s.stopDock()
	if !s.dock || s.dockHide <= 0 {
		return
	}
	generation := s.dockGen
	s.dockTimer = time.AfterFunc(s.dockHide, func() { s.dockIdled(generation) })
}

func (s *Shell) stopDock() {
	s.dockGen++
	if s.dockTimer != nil {
		s.dockTimer.Stop()
		s.dockTimer = nil
	}
}

func (s *Shell) dockIdled(generation uint64) {
	s.mu.Lock()
	if generation != s.dockGen {
		s.mu.Unlock()
		return
	}
	s.hideDock()
	s.mu.Unlock()
	s.Redraw()
}

func (s *Shell) idled(generation uint64) {
	s.mu.Lock()
	if generation != s.generation {
		s.mu.Unlock()
		return
	}
	s.timer = nil
	idle := time.Since(s.woke)
	for i := len(s.stack) - 1; i >= 0; i-- {
		v := s.stack[i]
		if s.holds[v] {
			continue
		}
		if expiry(v) > idle {
			break
		}
		s.stack = slices.Delete(s.stack, i, i+1)
	}
	if next, ok := s.next(); ok {
		generation := s.generation
		s.timer = time.AfterFunc(next-idle, func() { s.idled(generation) })
	}
	if len(s.stack) == 0 {
		s.closed = time.Now()
		s.generation++
		if s.timer != nil {
			s.timer.Stop()
			s.timer = nil
		}
	}
	s.mu.Unlock()
	s.changed()
}

func (s *Shell) wake() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if len(s.stack) == 0 {
		return
	}
	s.woke = time.Now()
	next, ok := s.next()
	if !ok {
		return
	}
	generation := s.generation
	s.timer = time.AfterFunc(next, func() { s.idled(generation) })
}

func (s *Shell) next() (time.Duration, bool) {
	for i := len(s.stack) - 1; i >= 0; i-- {
		if v := s.stack[i]; !s.holds[v] {
			return expiry(v), true
		}
	}
	return 0, false
}

func expiry(v View) time.Duration {
	if t, ok := v.(Timeouter); ok {
		return t.Timeout()
	}
	return DockTimeout
}
