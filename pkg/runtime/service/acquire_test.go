package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAServiceNotThereYetIsAskedForAgain(t *testing.T) {
	svc := &fake{name: "microphones", startErr: errors.New("no such device")}

	g := New()
	g.Add(svc, Restart(time.Millisecond, 5*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- g.Run(ctx) }()

	waitFor(t, func() bool {
		starts, _, _ := svc.counts()
		return starts >= 3
	})

	svc.mu.Lock()
	svc.startErr = nil
	svc.mu.Unlock()

	waitFor(t, func() bool { return statusOf(g, "microphones").State == StateRunning })

	if _, runs, _ := svc.counts(); runs != 1 {
		t.Errorf("Run was called %d times, want 1: it must not run before it has the device", runs)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run: %v", err)
	}
}

func TestAServiceWithNoRestartIsAskedForOnce(t *testing.T) {
	svc := &fake{name: "camera", startErr: errors.New("no such device")}

	g := New()
	g.Add(svc, Once())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- g.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)

	starts, runs, _ := svc.counts()
	if starts != 1 {
		t.Errorf("asked for the device %d times, want 1", starts)
	}
	if runs != 0 {
		t.Errorf("Run was called %d times for a device it never got", runs)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run: %v", err)
	}
}

func TestARequiredServiceStillGivesUp(t *testing.T) {
	svc := &fake{name: "display", startErr: errors.New("no panel")}

	g := New()
	g.Add(svc, Required(), Restart(time.Millisecond, 5*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := g.Run(ctx); err == nil {
		t.Fatal("a required service that could not be acquired did not stop the group")
	}
}
