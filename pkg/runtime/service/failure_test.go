package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestFatalAcquisitionCancelsEarlierServicesAndClosesPartialAcquisition(t *testing.T) {
	working := &fake{name: "working", run: blocks}
	broken := &fake{name: "broken", startErr: errors.New("no device")}
	g := New()
	g.Add(working)
	g.Add(broken, Required())
	done := make(chan error, 1)
	go func() { done <- g.Run(context.Background()) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("want failure")
		}
	case <-time.After(time.Second):
		t.Fatal("startup failure hung waiting for uncanceled services")
	}
	if _, _, closes := broken.counts(); closes != 1 {
		t.Fatalf("partial acquisition closed %d times", closes)
	}
	if _, _, closes := working.counts(); closes != 1 {
		t.Fatalf("earlier service closed %d times", closes)
	}
}
func TestGroupCannotBeRunTwice(t *testing.T) {
	g := New()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- g.Run(ctx) }()
	waitFor(t, func() bool { g.mu.Lock(); defer g.mu.Unlock(); return g.started })
	if err := g.Run(ctx); err == nil {
		t.Fatal("second Run accepted")
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run: %v", err)
	}
}

type reacquireProbe struct {
	mu           sync.Mutex
	starts, runs int
	allowed      bool
	entered      chan struct{}
}

func (p *reacquireProbe) Name() string { return "probe" }
func (p *reacquireProbe) Start(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.starts++
	if p.starts == 1 || p.allowed {
		return nil
	}
	return errors.New("not yet")
}
func (p *reacquireProbe) Run(ctx context.Context) error {
	p.mu.Lock()
	p.runs++
	n := p.runs
	p.mu.Unlock()
	if n == 1 {
		return errors.New("lost")
	}
	close(p.entered)
	<-ctx.Done()
	return nil
}
func TestFailedReacquisitionNeverRunsWithoutHardware(t *testing.T) {
	p := &reacquireProbe{entered: make(chan struct{})}
	g := New()
	g.Add(p, Restart(time.Millisecond, 2*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- g.Run(ctx) }()
	waitFor(t, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.starts >= 3 })
	p.mu.Lock()
	if p.runs != 1 {
		t.Errorf("ran %d times after failed Start", p.runs)
	}
	p.allowed = true
	p.mu.Unlock()
	select {
	case <-p.entered:
	case <-time.After(time.Second):
		t.Fatal("never reacquired")
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run: %v", err)
	}
}

type slowStart struct {
	fake
	entered chan struct{}
}

func (s *slowStart) Start(ctx context.Context) error {
	close(s.entered)
	<-ctx.Done()
	return ctx.Err()
}

func TestShutdownDuringARequiredStartIsNotAFailure(t *testing.T) {
	svc := &slowStart{fake: fake{name: "slow"}, entered: make(chan struct{})}
	g := New()
	g.Add(svc, Required())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- g.Run(ctx) }()
	<-svc.entered
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run = %v, want nil on shutdown", err)
	}
	if _, _, closes := svc.counts(); closes != 1 {
		t.Errorf("closed %d times, want 1", closes)
	}
}
