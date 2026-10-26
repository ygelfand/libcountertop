package shell

import (
	"sync"
	"testing"
	"time"
)

type view struct {
	covers bool
	asleep bool
	wakes  bool
	wait   time.Duration
}

func (v *view) Covers() bool { return v.covers }
func (v *view) Asleep() bool { return v.asleep }
func (v *view) Wakes() bool  { return v.wakes }
func (v *view) Timeout() time.Duration {
	if v.wait == 0 {
		return SettingsTimeout
	}
	return v.wait
}

type kept struct{ view }

func (*kept) Keeps() bool { return true }

func TestBackLeavesAKeptViewAndPopsOthers(t *testing.T) {
	s := &Shell{}
	call := &kept{view{covers: true}}
	h := s.Hold(call)
	s.Pop()
	if got := s.Views(); len(got) != 1 || got[0] != call || !h.Held() {
		t.Fatalf("back took the call away: %v", got)
	}

	page := &view{covers: true}
	s.Push(page)
	s.Pop()
	if got := s.Views(); len(got) != 1 || got[0] != call {
		t.Errorf("back over the call left %v, want the call", got)
	}

	h.Release()
	if got := s.Views(); len(got) != 0 {
		t.Errorf("releasing the call left %v", got)
	}
}

func TestReleasingAHoldTakesAwayThatViewAndNoOther(t *testing.T) {
	s := &Shell{}
	player, page := &view{covers: true}, &view{covers: true}
	h := s.Hold(player)
	s.Push(page)
	h.Release()
	if got := s.Views(); len(got) != 1 || got[0] != page {
		t.Errorf("left %v, want the page", got)
	}
	if h.Held() {
		t.Error("still held after release")
	}
}

func TestIdlingKeepsWhatIsHeld(t *testing.T) {
	s := &Shell{}
	player, page := &view{covers: true}, &view{covers: true}
	s.Hold(player)
	s.Push(page)
	s.woke = time.Now().Add(-SettingsTimeout)
	s.idled(s.generation)
	if got := s.Views(); len(got) != 1 || got[0] != player {
		t.Errorf("left %v, want the held player", got)
	}
}

func TestQueuedOldIdleCannotCloseRefreshedStack(t *testing.T) {
	s := New()
	defer s.Close()
	page := &view{covers: true}
	s.Push(page)
	old := s.generation
	s.Touch()
	s.idled(old)
	if s.Top() != page {
		t.Fatal("stale idle callback removed refreshed page")
	}
	s.Close()
	s.Push(page)
	s.idled(old)
	if s.Top() != page {
		t.Fatal("stale callback removed reopened page")
	}
}

func TestRemovingAViewDropsItsHold(t *testing.T) {
	s := &Shell{}
	idle := &view{covers: true}
	s.Hold(idle)
	s.Remove(idle)
	if s.Holding(idle) || s.Open() {
		t.Error("a removed view is still held or the shell is still open")
	}
}

func TestASleepingScreenRefusesWhatDoesNotWakeIt(t *testing.T) {
	s := &Shell{}
	idle := &view{covers: true, asleep: true}
	s.Hold(idle)
	s.Push(&view{covers: true})
	if s.Top() != View(idle) {
		t.Error("a covering view replaced the sleeping screen")
	}
	video := &view{covers: true, wakes: true}
	s.Push(video)
	if s.Top() != View(video) {
		t.Error("a view that wakes did not come up over the sleeping screen")
	}
}

type docking struct {
	view
	docks bool
}

func (d *docking) Docks() bool { return d.docks }

func TestTheDockFollowsTheCoveringView(t *testing.T) {
	s := &Shell{}
	if !s.Docked() {
		t.Error("an empty stack has no dock")
	}
	s.Push(&view{})
	if !s.Docked() {
		t.Error("a view that covers nothing hid the dock")
	}
	plain := &view{covers: true}
	s.Push(plain)
	if s.Docked() {
		t.Error("a covering view that does not dock has the dock")
	}
	player := &docking{view: view{covers: true}, docks: true}
	s.Push(player)
	s.Push(&view{})
	if !s.Docked() {
		t.Error("a covering view that docks, under a card, has no dock")
	}
	player.docks = false
	if s.Docked() {
		t.Error("a covering view that stopped docking kept the dock")
	}
}

func TestVisibleIsTheCoveringViewAndWhatIsAboveIt(t *testing.T) {
	s := &Shell{}
	under, page, card := &view{covers: true}, &view{covers: true}, &view{}
	s.Push(under)
	s.Push(page)
	s.Push(card)
	if s.Visible(under) || !s.Visible(page) || !s.Visible(card) {
		t.Errorf(
			"visible: under %v page %v card %v",
			s.Visible(under),
			s.Visible(page),
			s.Visible(card),
		)
	}
}

func TestTheTopViewsTimeoutRunsFirst(t *testing.T) {
	s := &Shell{}
	s.Push(&view{wait: time.Hour})
	s.Push(&view{covers: true})
	if got, _ := s.next(); got != SettingsTimeout {
		t.Errorf("timeout %v, want %v", got, SettingsTimeout)
	}
}

func TestIdlingClosesPagesOverALongLivedApp(t *testing.T) {
	s := &Shell{}
	app, page, sub := &view{wait: time.Hour}, &view{covers: true}, &view{covers: true}
	s.Push(app)
	s.Push(page)
	s.Push(sub)
	s.woke = time.Now().Add(-SettingsTimeout)
	s.idled(s.generation)
	if got := s.Views(); len(got) != 1 || got[0] != app {
		t.Errorf("left %v, want the app", got)
	}
	if s.timer == nil {
		t.Error("no timer left for the app")
	}
}

func TestIdlingClosesAShortViewUnderALongerOne(t *testing.T) {
	s := &Shell{}
	s.Push(&view{wait: DockTimeout})
	s.Push(&view{covers: true})
	s.woke = time.Now().Add(-SettingsTimeout)
	s.idled(s.generation)
	if got := s.Views(); len(got) != 0 {
		t.Errorf("left %v, want nothing", got)
	}
}

func TestChangesArriveInOrderUnderRacingPushAndRemove(t *testing.T) {
	s := New()
	defer s.Close()
	base := &view{covers: true}
	s.Push(base)
	var mu sync.Mutex
	var heard []Change
	s.Changed.Listen(func(c Change) { mu.Lock(); heard = append(heard, c); mu.Unlock() })
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				v := &view{covers: true}
				s.Push(v)
				s.Remove(v)
			}
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	for i := 1; i < len(heard); i++ {
		if heard[i].From != heard[i-1].To {
			t.Fatalf(
				"change %d starts at %v, the one before ended at %v",
				i,
				heard[i].From,
				heard[i-1].To,
			)
		}
	}
	if n := len(heard); n > 0 && heard[n-1].To != s.Top() {
		t.Errorf("last change ended at %v, top is %v", heard[n-1].To, s.Top())
	}
}

func TestAListenerThatChangesTheStackIsHeardAfterwards(t *testing.T) {
	s := New()
	defer s.Close()
	first, second := &view{covers: true}, &view{covers: true}
	var heard []Change
	s.Changed.Listen(func(c Change) {
		heard = append(heard, c)
		if c.To == first {
			s.Push(second)
		}
	})
	s.Push(first)
	want := []Change{{From: nil, To: first}, {From: first, To: second}}
	if len(heard) != len(want) || heard[0] != want[0] || heard[1] != want[1] {
		t.Errorf("heard %v, want %v", heard, want)
	}
}

func (s *Shell) dockStamp() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dockGen
}

func TestASwipedDockHidesWhenItsTimeRunsOut(t *testing.T) {
	s := New()
	defer s.Close()
	s.ShowDock(time.Hour)
	if shown, pinned := s.Dock(); !shown || pinned {
		t.Fatalf("dock shown %v pinned %v, want shown and not pinned", shown, pinned)
	}
	s.dockIdled(s.dockStamp())
	if shown, _ := s.Dock(); shown {
		t.Error("the dock stayed after its time ran out")
	}
}

func TestATouchKeepsASwipedDockOpen(t *testing.T) {
	s := New()
	defer s.Close()
	s.ShowDock(time.Hour)
	old := s.dockStamp()
	s.Touch()
	s.dockIdled(old)
	if shown, _ := s.Dock(); !shown {
		t.Error("a timer from before the touch hid the dock")
	}
}

func TestAPinnedDockNeverTimesOut(t *testing.T) {
	s := New()
	defer s.Close()
	s.ShowDock(0)
	s.mu.Lock()
	timer := s.dockTimer
	s.mu.Unlock()
	if timer != nil {
		t.Error("a pinned dock started a timer")
	}
	if shown, pinned := s.Dock(); !shown || !pinned {
		t.Errorf("dock shown %v pinned %v, want both", shown, pinned)
	}
}

func TestACoveringViewHidesASwipedDockButNotAPinnedOne(t *testing.T) {
	s := New()
	defer s.Close()
	page := &view{covers: true}
	card := &view{}

	s.ShowDock(time.Hour)
	s.Push(card)
	if shown, _ := s.Dock(); !shown {
		t.Fatal("a view that does not cover hid the dock")
	}
	s.Push(page)
	if shown, _ := s.Dock(); shown {
		t.Fatal("a covering view left a swiped dock up")
	}

	s.Close()
	s.ShowDock(0)
	s.Push(page)
	if shown, pinned := s.Dock(); !shown || !pinned {
		t.Error("a covering view took a pinned dock away")
	}
}

func TestTheDockIsNotPartOfTheStack(t *testing.T) {
	s := New()
	defer s.Close()
	s.ShowDock(0)
	if s.Open() || s.Top() != nil || len(s.Views()) != 0 {
		t.Error("the dock counts as something open")
	}
}

func TestAHoldRefusedByASleepingTopIsNotRecorded(t *testing.T) {
	s := New()
	defer s.Close()
	s.Push(&view{covers: true, asleep: true})
	page := &view{covers: true}
	if h := s.Hold(page); h != nil {
		t.Fatal("a sleeping top let a covering view be held")
	}
	if s.Holding(page) {
		t.Error("a refused hold is still recorded")
	}
}
