package playout

import (
	"sync/atomic"
	"testing"

	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

type countingScreen struct {
	fakeScreen
	leaves, releases atomic.Int32
}

func (s *countingScreen) Leave()       { s.leaves.Add(1) }
func (s *countingScreen) Release(bool) { s.releases.Add(1) }

func counted() (*Output, *countingScreen) {
	o := newOutput()
	s := &countingScreen{}
	o.options.Screen = s
	return o, s
}

func TestAnEndedSessionTakesItsPageDown(t *testing.T) {
	o, s := counted()
	o.Attend(&playback.Session{Label: "app", Pictured: true})
	o.Attend(nil)
	if s.leaves.Load() != 1 || s.releases.Load() != 0 {
		t.Errorf("an ended session left %d times and released %d times, want 1 and 0",
			s.leaves.Load(), s.releases.Load())
	}
}

func TestAStoppedPlaybackWithNoSessionTakesItsPageDown(t *testing.T) {
	o, s := counted()
	src := &endless{}
	o.Play(src)
	before := s.leaves.Load()
	o.Stop(src)
	if got := s.leaves.Load() - before; got != 1 {
		t.Errorf("a stopped playback left its page %d times, want 1", got)
	}
}

func TestAStoppedPlaybackInASessionKeepsThePage(t *testing.T) {
	o, s := counted()
	o.Attend(&playback.Session{Label: "app", Pictured: true})
	src := &endless{}
	o.Play(src)
	before := s.leaves.Load()
	o.Stop(src)
	if got := s.leaves.Load() - before; got != 0 {
		t.Errorf("a playback stopped inside its session left the page %d times", got)
	}
}

func TestAHandoffKeepsThePage(t *testing.T) {
	o, s := counted()
	src := &endless{}
	o.Play(src)
	before := s.leaves.Load()
	o.Handoff(src)
	if got := s.leaves.Load() - before; got != 0 {
		t.Errorf("a handoff left the page %d times", got)
	}
	o.Stop(src)
}

func TestASessionEndedMidHandoffLeavesNothingPlaying(t *testing.T) {
	o, _ := counted()
	o.Attend(&playback.Session{Label: "app", Pictured: true})
	src := &endless{}
	o.Play(src)
	o.Handoff(src)
	if _, held := testPlayer.owner.Get(); !held {
		t.Fatal("a handoff let go of the playing card")
	}
	o.Attend(nil)
	if s, held := testPlayer.owner.Get(); held {
		t.Errorf("an ended session left %T holding the player", s)
	}
}

func TestStoppingAnotherSourceKeepsThePage(t *testing.T) {
	o, s := counted()
	src := &endless{}
	o.Play(src)
	t.Cleanup(func() { o.Stop(src) })
	before := s.leaves.Load()
	o.Stop(&endless{})
	if got := s.leaves.Load() - before; got != 0 {
		t.Errorf("stopping a source that was not playing left the page %d times", got)
	}
}
