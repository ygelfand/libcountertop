package webreceiver

import (
	"context"
	"testing"
	"time"
)

func TestSamplesWaitForTheirKeysLicense(t *testing.T) {
	k := &keyring{}
	audio, video, other := [16]byte{1}, [16]byte{2}, [16]byte{3}
	k.sessions = map[string][][]byte{"s": {audio[:], video[:]}}
	done := make(chan error, 1)
	go func() { done <- k.wait(context.Background(), video) }()
	select {
	case err := <-done:
		t.Fatalf("a sample went ahead before its license: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	k.mu.Lock()
	k.licensed, k.any = map[[16]byte]bool{audio: true, video: true}, true
	k.cond().Broadcast()
	k.mu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a licensed key kept waiting")
	}
	if err := k.wait(context.Background(), other); err != nil {
		t.Fatalf("a key nobody asked for waited past the first license: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	k.mu.Lock()
	k.licensed, k.any = nil, false
	k.mu.Unlock()
	if err := k.wait(ctx, video); err == nil {
		t.Fatal("a cancelled wait reported a license")
	}
}

func TestReleasingTheLastSessionForgetsItsLicenses(t *testing.T) {
	k := &keyring{
		sessions: map[string][][]byte{"old": nil},
		licensed: map[[16]byte]bool{{1}: true},
		any:      true,
	}
	k.close("old")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := k.wait(ctx, [16]byte{9}); err == nil {
		t.Fatal("a new channel's key went ahead on the old channel's license")
	}
}
