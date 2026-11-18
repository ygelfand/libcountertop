package airplay

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchAddressesRestartsOnANewLease(t *testing.T) {
	addressPoll = time.Millisecond
	var now atomic.Value
	now.Store([]net.IP{net.ParseIP("2a00::1")})
	read := func() []net.IP { return now.Load().([]net.IP) }

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	done := make(chan struct{})
	go func() { defer close(done); watchAddresses(ctx, "2a00::1", read, cancel) }()

	time.Sleep(20 * time.Millisecond)
	if ctx.Err() != nil {
		t.Fatal("restarted with the addresses unchanged")
	}
	now.Store([]net.IP(nil))
	time.Sleep(20 * time.Millisecond)
	if ctx.Err() != nil {
		t.Fatal("restarted while no address was up")
	}
	now.Store([]net.IP{net.ParseIP("2a00::1"), net.ParseIP("192.168.68.135")})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("not restarted after the IPv4 lease")
	}
	if !errors.Is(context.Cause(ctx), errAddressChanged) {
		t.Fatalf("cause = %v", context.Cause(ctx))
	}
}
