package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestKeepRestartsAFailureUntilItIsPermanent(t *testing.T) {
	stop := errors.New("wrong version")
	calls := 0
	err := Keep(context.Background(), "probe", func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("crashed")
		}
		return Permanent(stop)
	}, Restart(time.Millisecond, 2*time.Millisecond))
	if !errors.Is(err, stop) {
		t.Fatalf("Keep returned %v, want %v", err, stop)
	}
	if calls != 3 {
		t.Fatalf("ran %d times, want 3", calls)
	}
}

func TestKeepEndsWhenTheRunFinishes(t *testing.T) {
	calls := 0
	err := Keep(context.Background(), "probe", func(context.Context) error {
		calls++
		return nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("Keep returned %v after %d runs, want nil after 1", err, calls)
	}
}

func TestKeepStopsRestartingWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Keep(ctx, "probe", func(context.Context) error {
			return errors.New("crashed")
		}, Restart(time.Millisecond, time.Millisecond))
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Keep returned %v after the context ended, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Keep kept restarting after the context ended")
	}
}

func TestKeepWithOnceDoesNotRestart(t *testing.T) {
	crashed := errors.New("crashed")
	calls := 0
	err := Keep(context.Background(), "probe", func(context.Context) error {
		calls++
		return crashed
	}, Once())
	if !errors.Is(err, crashed) || calls != 1 {
		t.Fatalf("Keep returned %v after %d runs, want %v after 1", err, calls, crashed)
	}
}

func TestGroupGivesUpOnAPermanentFailure(t *testing.T) {
	f := &fake{name: "versioned", run: func(context.Context) error {
		return Permanent(errors.New("wrong version"))
	}}
	g := New()
	g.Add(f, Restart(time.Millisecond, time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- g.Run(ctx) }()
	waitFor(t, func() bool { return g.Status()[0].State == StateFailed })
	time.Sleep(20 * time.Millisecond)
	if _, runs, _ := f.counts(); runs != 1 {
		t.Fatalf("permanent failure ran %d times, want 1", runs)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run: %v", err)
	}
}
