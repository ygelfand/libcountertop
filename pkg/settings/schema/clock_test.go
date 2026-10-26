package schema

import (
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

func TestAFreshDeviceIdlesIntoAWanderingClock(t *testing.T) {
	c, idle := DefaultClock(), DefaultIdle()

	if idle.Face != c.Face || idle.Size != c.Size ||
		idle.Position != PositionRandom || idle.Align != AlignRandom {
		t.Errorf("idle %+v, clock %+v", idle, c)
	}
	if idle.First.On() || idle.Second.On() {
		t.Error("a fresh device idles with a visual")
	}
	if idle.After.After() <= 0 {
		t.Errorf("a fresh device has no wait, after %v", idle.After)
	}
}

func TestNeverIsNoWaitAtAll(t *testing.T) {
	if got := DelayNever.After(); got != 0 {
		t.Errorf("never is %v, want zero", got)
	}

	for _, d := range Delays() {
		if d == DelayNever {
			continue
		}
		if d.After() <= 0 {
			t.Errorf("%v waits %v", d, d.After())
		}
	}
}

func TestTheDelaysAreInOrder(t *testing.T) {
	delays := Delays()

	if got := delays[len(delays)-1]; got != DelayNever {
		t.Errorf("the last choice is %v, want Never", got)
	}

	var last time.Duration
	for _, d := range delays[:len(delays)-1] {
		if d.After() <= last {
			t.Errorf("%v comes after %v", d, last)
		}
		last = d.After()
	}
}

func TestEveryDelayHasItsOwnLabel(t *testing.T) {
	seen := map[string]Delay{}

	for _, d := range Delays() {
		label := d.Label()
		if had, ok := seen[label]; ok {
			t.Errorf("%v and %v are both shown as %q", had, d, label)
		}
		seen[label] = d
	}
}

func TestTheIdleFaceIsItsOwnSetting(t *testing.T) {
	s, _ := load(t)
	idle := NewIdleWriter(storage.Field(s, func(r *root) *Idle { return &r.Idle }))
	clock := NewClockWriter(storage.Field(s, func(r *root) *Clock { return &r.Clock }))

	if err := idle.Face(FaceNone); err != nil {
		t.Fatal(err)
	}
	if err := clock.Face(Face("analog-seconds")); err != nil {
		t.Fatal(err)
	}

	if got := s.Get(); got.Clock.Face != Face("analog-seconds") || got.Idle.Face != FaceNone {
		t.Errorf(
			"clock is %v and idle is %v, want analog-seconds and none",
			got.Clock.Face,
			got.Idle.Face,
		)
	}
}

func TestEachIdleVisualIsItsOwnSetting(t *testing.T) {
	s, _ := load(t)
	w := NewIdleWriter(storage.Field(s, func(r *root) *Idle { return &r.Idle }))

	if err := w.Visual(1, IdleVisual{Kind: "orb", Source: SourceMic}); err != nil {
		t.Fatal(err)
	}
	idle := s.Get().Idle
	if idle.First.On() || idle.Second != (IdleVisual{Kind: "orb", Source: SourceMic}) {
		t.Errorf("first %+v second %+v", idle.First, idle.Second)
	}
}
