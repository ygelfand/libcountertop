package castreceiver

import (
	"testing"
	"time"
)

func TestARefusedAppIsAllowedAgainLater(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	r := newRefused()
	r.now = func() time.Time { return now }
	if !r.allows("CC32E753") {
		t.Fatal("an app nobody refused was turned away")
	}
	r.add("CC32E753")
	if r.allows("CC32E753") {
		t.Error("a just-refused app was allowed")
	}
	if !r.allows("233637DE") {
		t.Error("refusing one app turned away another")
	}
	now = now.Add(refusedFor)
	if !r.allows("CC32E753") {
		t.Error("the refusal did not run out")
	}
}
