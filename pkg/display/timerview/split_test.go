package timerview

import (
	"errors"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/assistant/timer"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func TestCellsForOneTwoThreeAndFourTimers(t *testing.T) {
	full := Cell{0, 0, 1920, 1200}
	tl, tr := Cell{0, 0, 960, 600}, Cell{960, 0, 960, 600}
	bl, br := Cell{0, 600, 960, 600}, Cell{960, 600, 960, 600}
	cases := map[int][]Cell{
		1: {full},
		2: {{0, 0, 960, 1200}, {960, 0, 960, 1200}},
		3: {tl, tr, bl},
		4: {tl, tr, bl, br},
	}
	for n, want := range cases {
		got := splitCells(n, 1920, 1200)
		if len(got) != len(want) {
			t.Fatalf("%d timers: %d cells, want %d", n, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%d timers, cell %d = %+v, want %+v", n, i, got[i], want[i])
			}
		}
	}
	if got := splitCells(0, 1920, 1200); len(got) != 0 {
		t.Errorf("no timers: %d cells, want 0", len(got))
	}
}

func TestFiveTimersShowFourAndPlusOne(t *testing.T) {
	if got := splitCells(5, 1920, 1200); len(got) != 4 {
		t.Errorf("5 timers: %d cells, want 4", len(got))
	}
	if got := plusMark(5); got != "+1" {
		t.Errorf("plusMark(5) = %q, want +1", got)
	}
	if got := plusMark(4); got != "" {
		t.Errorf("plusMark(4) = %q, want nothing", got)
	}
}

type stopper struct{ stopped int }

func (s *stopper) Stop() bool { s.stopped++; return true }

type acted struct {
	action, id string
	calls      int
	err        error
}

func viewFor(s *stopper, a *acted) *View {
	return &View{
		Timers: s,
		Act: func(action, id string) error {
			a.action, a.id = action, id
			a.calls++
			return a.err
		},
	}
}

func TestThePauseKeyPausesThatTimer(t *testing.T) {
	var s stopper
	var a acted
	v := viewFor(&s, &a)

	v.press(timer.Entry{ID: "t1", Left: time.Minute}, opPause)()
	if a.action != "voice_timer_control.pause" || a.id != "t1" {
		t.Errorf("called %q with %q, want voice_timer_control.pause t1", a.action, a.id)
	}

	v.press(timer.Entry{ID: "t1", Paused: true}, opPause)()
	if a.action != "voice_timer_control.resume" {
		t.Errorf("paused timer called %q, want voice_timer_control.resume", a.action)
	}

	v.press(timer.Entry{ID: "t2"}, opCancel)()
	if a.action != "voice_timer_control.cancel" || a.id != "t2" {
		t.Errorf("called %q with %q, want voice_timer_control.cancel t2", a.action, a.id)
	}
	if s.stopped != 0 {
		t.Errorf("stopped the ring %d times, want 0", s.stopped)
	}
}

func TestTheDomainIsASetting(t *testing.T) {
	var s stopper
	var a acted
	v := viewFor(&s, &a)
	v.Domain = func() string { return "echolocal" }

	v.press(timer.Entry{ID: "t1"}, opCancel)()
	if a.action != "echolocal.cancel" {
		t.Errorf("called %q, want echolocal.cancel", a.action)
	}
}

func TestTheXOnAFinishedTimerOnlyStopsTheRing(t *testing.T) {
	var s stopper
	var a acted
	v := viewFor(&s, &a)

	v.press(timer.Entry{ID: "t1", Done: true}, opCancel)()
	v.press(timer.Entry{ID: "t1", Done: true}, opPause)()
	if a.calls != 0 {
		t.Errorf("called Home Assistant %d times, want 0", a.calls)
	}
	if s.stopped != 2 {
		t.Errorf("stopped the ring %d times, want 2", s.stopped)
	}
}

func TestAFailedCallOnlyLogs(t *testing.T) {
	var s stopper
	a := acted{err: errors.New("no voice timer t1")}
	v := viewFor(&s, &a)

	v.press(timer.Entry{ID: "t1"}, opCancel)()
	if a.calls != 1 {
		t.Errorf("called %d times, want 1", a.calls)
	}

	v.Act = nil
	v.press(timer.Entry{ID: "t1"}, opCancel)() // no Act wired: must not panic
}

func TestSplitCoversWhileAnyTimerIsListed(t *testing.T) {
	paused := timer.Card{All: []timer.Entry{{ID: "a", Paused: true}}}
	if !Covers(paused, schema.TimerSplit) {
		t.Error("Split does not cover with only a paused timer")
	}
	if Covers(timer.Card{}, schema.TimerSplit) {
		t.Error("Split covers with no timers")
	}
	if Covers(paused, schema.TimerDial) {
		t.Error("Dial now covers: other styles must not change")
	}
	if !Covers(timer.Card{Showing: true}, schema.TimerFull) {
		t.Error("Full screen stopped covering")
	}
}

func TestShownEntriesAddARingWhenForgottenCellsLeftOnlyTheRing(t *testing.T) {
	got := shownEntries(timer.Card{Ringing: true, Showing: true, Name: "pasta"})
	if len(got) != 1 || !got[0].Done || got[0].Name != "pasta" {
		t.Errorf("got %+v, want one finished cell named pasta", got)
	}
	got = shownEntries(timer.Card{All: []timer.Entry{{ID: "a"}}})
	if len(got) != 1 || got[0].ID != "a" {
		t.Errorf("got %+v, want the listed timer", got)
	}
}
