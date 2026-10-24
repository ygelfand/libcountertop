package cast

import "testing"

func TestDoneFinishesTheTrackTheReceiverPlayed(t *testing.T) {
	r, transport := launched(t)
	replyTo(t, r, toApp(SenderID, transport, NSMedia,
		`{"type":"LOAD","requestId":2,"media":{"contentId":"http://example/a.mp3"}}`))

	out := r.Done()
	if len(out) != 1 {
		t.Fatalf("Done sent %d messages, want one media status", len(out))
	}
	got := mediaStatus(t, out[0].Payload)
	if got[0].PlayerState != StateIdle || got[0].IdleReason != IdleFinished {
		t.Fatalf("state %q/%q, want idle/finished", got[0].PlayerState, got[0].IdleReason)
	}
	if r.Running() == "" {
		t.Fatal("finishing a track ended the app")
	}
}

func TestDoneEndsTheAppWhenNothingWasLoaded(t *testing.T) {
	r, _ := launched(t)
	r.Done()
	if r.Running() != "" {
		t.Fatalf("app %q still running after Done with nothing loaded", r.Running())
	}
}
