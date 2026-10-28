package call

import (
	"context"
	"net/http"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"

	discovery "github.com/ygelfand/libcountertop/pkg/network/peer"
)

func TestCallControlActionsAreDeclaredForHAAndVoice(t *testing.T) {
	for _, action := range (&Calls{}).ControlActions() {
		if action.Name != "call_answer" && action.Name != "call_hangup" {
			t.Fatalf("unexpected action %q", action.Name)
		}
		if len(action.Triggers) == 0 || action.Run == nil {
			t.Fatalf("incomplete action %+v", action)
		}
		if reply, err := action.Run(esphome.Call{}); err != nil || reply == "" {
			t.Fatalf("idle %s = %q, %v", action.Name, reply, err)
		}
	}
}

func TestAnswerCallDefaultsToAudio(t *testing.T) {
	for _, video := range []bool{false, true} {
		quiet := 0
		s := &session{
			Call:    Call{State: Ringing, Video: true},
			quiet:   func() { quiet++ },
			stopped: true,
		}
		c := &Calls{now: s}
		args, err := esphome.NewCall(map[string]any{"video": video})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.ControlActions()[0].Run(args); err != nil {
			t.Fatal(err)
		}
		if s.Video != video || quiet != 1 {
			t.Fatalf("video = %v, quiet = %d", s.Video, quiet)
		}
	}
}

type actionLine struct{ ended chan Reason }

func (l actionLine) send(_ context.Context, _ string, m envelope) (int, error) {
	l.ended <- m.Reason
	return http.StatusOK, nil
}

func TestHangupCallHandlesEveryActiveState(t *testing.T) {
	for _, tc := range []struct {
		state  State
		reason Reason
	}{{Calling, ReasonCancelled}, {Ringing, ReasonDeclined}, {Talking, ReasonHangup}} {
		t.Run(tc.state.String(), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			l := actionLine{ended: make(chan Reason, 1)}
			c := New(Dependencies{Settings: &callSettings{}})
			defer c.stop()
			c.now = &session{
				Call:   Call{State: tc.state},
				ctx:    ctx,
				stop:   cancel,
				unhook: func() {},
				line:   l,
			}
			if _, err := c.ControlActions()[1].Run(esphome.Call{}); err != nil {
				t.Fatal(err)
			}
			if _, ok := c.Now(); ok {
				t.Fatal("call is still active")
			}
			select {
			case reason := <-l.ended:
				if reason != tc.reason {
					t.Fatalf("ended with %s, want %s", reason, tc.reason)
				}
			case <-t.Context().Done():
				t.Fatal("call end was not sent")
			}
		})
	}
}

func TestASpokenNameFindsItsDevice(t *testing.T) {
	peers := []discovery.Peer{
		{ID: "abc", Name: "Kitchen Blueberry"},
		{ID: "def", Name: "Athena"},
		{ID: "ghi", Name: "Office Echo"},
		{ID: "jkl", Name: "Office Show"},
	}
	for _, c := range []struct {
		said, want string
	}{
		{"athena", "def"},
		{"the Athena", "def"},
		{"kitchen blueberry", "abc"},
		{"blueberry", "abc"},
		{"def", "def"},
		{"office", ""},
		{"mom", ""},
		{"", ""},
	} {
		p, ok := Find(peers, c.said)
		if got := map[bool]string{true: p.ID, false: ""}[ok]; got != c.want {
			t.Errorf("Find(%q) = %q, want %q", c.said, got, c.want)
		}
	}
}
