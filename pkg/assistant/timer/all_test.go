package timer

import (
	"testing"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"
	"github.com/ygelfand/go-esphome-device/api"
)

func ids(c Card) []string {
	out := make([]string, 0, len(c.All))
	for _, e := range c.All {
		out = append(out, e.ID)
	}
	return out
}

func finishedEvent(id string) esphome.TimerEvent {
	return esphome.TimerEvent{
		Type:    api.VoiceAssistantTimerEvent_VOICE_ASSISTANT_TIMER_FINISHED,
		TimerID: id,
		Name:    id,
	}
}

func TestEveryTimerIsListedInTheOrderItWasStarted(t *testing.T) {
	ts := New(Options{})
	ts.Event(started("pasta", 600))
	ts.Event(started("eggs", 60))
	ts.Event(started("rice", 900))

	if got := ids(ts.Now()); len(got) != 3 || got[0] != "pasta" || got[1] != "eggs" || got[2] != "rice" {
		t.Errorf("listed %v, want [pasta eggs rice]", got)
	}
}

func TestAnUpdateKeepsATimersPlace(t *testing.T) {
	ts := New(Options{})
	ts.Event(started("pasta", 600))
	ts.Event(started("eggs", 60))
	e := started("pasta", 600)
	e.Type = api.VoiceAssistantTimerEvent_VOICE_ASSISTANT_TIMER_UPDATED
	e.SecondsLeft = 500
	ts.Event(e)

	if got := ids(ts.Now()); len(got) != 2 || got[0] != "pasta" {
		t.Errorf("listed %v, want pasta first", got)
	}
}

func TestAPausedTimerKeepsItsPlaceAndIsMarkedPaused(t *testing.T) {
	ts := New(Options{})
	ts.Event(started("pasta", 600))
	ts.Event(started("eggs", 60))
	e := started("pasta", 600)
	e.Type = api.VoiceAssistantTimerEvent_VOICE_ASSISTANT_TIMER_UPDATED
	e.IsActive = false
	ts.Event(e)

	all := ts.Now().All
	if len(all) != 2 || all[0].ID != "pasta" || !all[0].Paused || all[1].Paused {
		t.Errorf("got %+v, want pasta paused first, eggs running", all)
	}
}

func TestAFinishedTimerKeepsItsCellUntilTheRingStops(t *testing.T) {
	ts := New(Options{})
	t.Cleanup(func() { ts.Stop() })
	ts.Event(started("pasta", 600))
	ts.Event(started("eggs", 60))
	ts.Event(finishedEvent("eggs"))

	all := ts.Now().All
	if len(all) != 2 || all[1].ID != "eggs" || !all[1].Done || all[1].Left != 0 {
		t.Fatalf("got %+v, want eggs done in its place with nothing left", all)
	}
	if len(ts.held) != 1 {
		t.Errorf("holding %d running timers, want 1: a finished timer is gone at both ends", len(ts.held))
	}

	ts.Stop()
	waitQuiet(t, ts)
	if got := ids(ts.Now()); len(got) != 1 || got[0] != "pasta" {
		t.Errorf("listed %v after the ring stopped, want [pasta]", got)
	}
}

func TestTwoFinishedTimersShareOneRing(t *testing.T) {
	ts := New(Options{})
	t.Cleanup(func() { ts.Stop() })
	ts.Event(started("pasta", 600))
	ts.Event(started("eggs", 60))
	ts.Event(finishedEvent("pasta"))
	ts.Event(finishedEvent("eggs"))

	all := ts.Now().All
	if len(all) != 2 || !all[0].Done || !all[1].Done {
		t.Fatalf("got %+v, want both done", all)
	}
	if !ts.Stop() {
		t.Fatal("Stop reported nothing to stop")
	}
	waitQuiet(t, ts)
	if got := ts.Now().All; len(got) != 0 {
		t.Errorf("listed %+v after one stop, want nothing", got)
	}
}

func TestForgettingClearsFinishedCellsButNotTheRing(t *testing.T) {
	ts := New(Options{})
	t.Cleanup(func() { ts.Stop() })
	ts.Event(started("pasta", 600))
	ts.Event(finishedEvent("pasta"))
	ts.Forget()

	if got := ts.Now().All; len(got) != 0 {
		t.Errorf("listed %+v, want nothing", got)
	}
	if !ts.Ringing() {
		t.Error("stopped ringing")
	}
}

func TestTheSoonestCardIsUnchanged(t *testing.T) {
	ts := New(Options{})
	ts.Event(started("pasta", 600))
	ts.Event(started("eggs", 60))

	c := ts.Now()
	if c.Name != "eggs" || !c.Showing || c.Ringing || c.Of.Seconds() != 60 {
		t.Errorf("got %+v, want the soonest card for eggs", c)
	}
}

func TestTheCardChangesWhenAnyTimerChanges(t *testing.T) {
	ts := New(Options{})
	ts.Event(started("pasta", 600))
	ts.Event(started("eggs", 60))
	before := ts.Now().key()

	e := started("pasta", 600)
	e.Type = api.VoiceAssistantTimerEvent_VOICE_ASSISTANT_TIMER_UPDATED
	e.IsActive = false
	ts.Event(e)

	if ts.Now().key() == before {
		t.Error("pausing a timer that is not the soonest did not change the card")
	}
}

func waitQuiet(t *testing.T, ts *Timers) {
	t.Helper()
	for range 200 {
		if !ts.Ringing() {
			return
		}
		ts.Stop()
		sleepTick()
	}
	t.Fatal("still ringing")
}

func sleepTick() { time.Sleep(5 * time.Millisecond) }

func TestAnUnnamedTimerIsCalledTimer(t *testing.T) {
	ts := New(Options{})
	e := started("t1", 60)
	e.Name = ""
	ts.Event(e)
	if all := ts.Now().All; len(all) != 1 || all[0].Name != "Timer" {
		t.Errorf("got %+v, want it named Timer", all)
	}
}
