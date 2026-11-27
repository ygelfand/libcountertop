package activity

import (
	"sync"
	"testing"
	"time"
)

func TestAKnownSpeakerRidesAlongWithTheTurn(t *testing.T) {
	log, got := caught()
	turn := log.Begin(1, "Hey Google")
	turn.Awaits()
	turn.Spoke("Alex", "person.alex", 0.613)
	turn.Ends(Completed)
	if len(*got) != 1 {
		t.Fatalf("%d events, want 1", len(*got))
	}
	e := (*got)[0]
	if e["speaker"] != "Alex" || e["speaker_person"] != "person.alex" ||
		e["speaker_score"] != "0.61" {
		t.Errorf("speaker fields %v", e)
	}
	if e["version"] != Version {
		t.Errorf("version %q, want %q", e["version"], Version)
	}
}

func TestAnUnrecognizedVoiceIsUnknownWithItsScore(t *testing.T) {
	log, got := caught()
	turn := log.Begin(1, "Hey Google")
	turn.Awaits()
	turn.Spoke("", "", 0.42)
	turn.Ends(Completed)
	e := (*got)[0]
	if e["speaker"] != unknown || e["speaker_score"] != "0.42" || e["speaker_person"] != "" {
		t.Errorf("speaker fields %v", e)
	}
}

func TestATurnNobodyMatchesCarriesNoSpeaker(t *testing.T) {
	log, got := caught()
	log.Begin(1, "Hey Google").Ends(Completed)
	if _, ok := (*got)[0]["speaker"]; ok {
		t.Errorf("a turn with no voice match named a speaker: %v", (*got)[0])
	}
}

func TestALateMatchIsWaitedFor(t *testing.T) {
	var mu sync.Mutex
	var got []map[string]string
	sent := make(chan struct{}, 2)
	log := New(Options{Emit: func(f map[string]string) {
		mu.Lock()
		got = append(got, f)
		mu.Unlock()
		sent <- struct{}{}
	}})
	turn := log.Begin(1, "Hey Google")
	turn.Awaits()
	turn.Ends(Completed)
	select {
	case <-sent:
		t.Fatal("the event went out before the awaited match")
	case <-time.After(100 * time.Millisecond):
	}
	turn.Spoke("Alex", "person.alex", 0.7)
	<-sent
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0]["speaker"] != "Alex" {
		t.Errorf("events %v, want one naming Alex", got)
	}
}

func TestTheSpeakerIsAskedOfTheTurnInProgress(t *testing.T) {
	log, _ := caught()
	if got := log.Speaker(); got.Match != NoTurn {
		t.Errorf("before any turn: %+v", got)
	}

	turn := log.Begin(1, "Hey Google")
	if got := log.Speaker(); got.Match != Unknown || got.Turn != turn.ID() {
		t.Errorf("a turn with no voice match: %+v", got)
	}
	turn.Awaits()
	if got := log.Speaker(); got.Match != Pending {
		t.Errorf("before the match lands: %+v", got)
	}
	turn.Spoke("Alex", "person.alex", 0.61)
	if got := log.Speaker(); got.Match != Known || got.Person != "person.alex" ||
		got.Name != "Alex" {
		t.Errorf("after the match: %+v", got)
	}
	turn.Ends(Completed)
	if got := log.Speaker(); got.Match != NoTurn {
		t.Errorf("after the turn: %+v", got)
	}
}

func TestAnUnrecognizedVoiceIsAskedAsUnknown(t *testing.T) {
	log, _ := caught()
	turn := log.Begin(1, "Hey Google")
	turn.Awaits()
	turn.Spoke("", "", 0.3)
	if got := log.Speaker(); got.Match != Unknown || got.Person != "" {
		t.Errorf("%+v", got)
	}
}

func TestAskingDuringAPendingMatchWaitsForIt(t *testing.T) {
	log, _ := caught()
	turn := log.Begin(1, "Hey Google")
	turn.Awaits()
	time.AfterFunc(50*time.Millisecond, func() { turn.Spoke("Alex", "person.alex", 0.7) })
	if got := log.SpeakerWithin(time.Second); got.Match != Known || got.Person != "person.alex" {
		t.Errorf("%+v", got)
	}
}

func TestAMissedMatchEndsTheWaitAsUnknown(t *testing.T) {
	log, _ := caught()
	turn := log.Begin(1, "Hey Google")
	turn.Awaits()
	time.AfterFunc(50*time.Millisecond, turn.Missed)
	start := time.Now()
	if got := log.SpeakerWithin(time.Second); got.Match != Unknown {
		t.Errorf("%+v", got)
	}
	if waited := time.Since(start); waited > 500*time.Millisecond {
		t.Errorf("waited %v after the match was missed", waited)
	}
}

func TestAMatchThatNeverSettlesStopsAtTheBound(t *testing.T) {
	log, _ := caught()
	turn := log.Begin(1, "Hey Google")
	turn.Awaits()
	if got := log.SpeakerWithin(50 * time.Millisecond); got.Match != Pending {
		t.Errorf("%+v", got)
	}
}

func TestAMissedMatchDoesNotHoldTheTurnEvent(t *testing.T) {
	log, got := caught()
	turn := log.Begin(1, "Hey Google")
	turn.Awaits()
	turn.Missed()
	turn.Ends(Completed)
	if len(*got) != 1 {
		t.Fatalf("%d events right after the turn ended, want 1", len(*got))
	}
	if _, ok := (*got)[0]["speaker"]; ok {
		t.Errorf("a missed match named a speaker: %v", (*got)[0])
	}
}
