package webreceiver

import (
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/media/sponsorblock"
)

func TestTheVideoIsReadFromThePagesAddress(t *testing.T) {
	for address, want := range map[string]string{
		"https://www.youtube.com/tv?castv=2.0#/watch?v=Gbf7tqEElUA&list=RQ": "Gbf7tqEElUA",
		"https://www.youtube.com/watch?v=jNQXAC9IVRw":                       "jNQXAC9IVRw",
		"https://www.youtube.com/tv?castv=2.0#/":                            "",
		"https://example.com/player?v=jNQXAC9IVRw":                          "",
		"about:blank": "",
	} {
		if got := videoID(address); got != want {
			t.Errorf("videoID(%q) = %q, want %q", address, got, want)
		}
	}
}

func TestSkipsWaitForTheVideoItselfNotAnAd(t *testing.T) {
	segs := []sponsorblock.Segment{
		{From: time.Minute, To: 2 * time.Minute, Length: 10 * time.Minute},
	}
	for playing, want := range map[time.Duration]bool{
		10 * time.Minute:               true,
		10*time.Minute + time.Second:   true,
		30 * time.Second:               false,
		0:                              false,
		10*time.Minute + 5*time.Second: false,
	} {
		if got := lengthMatches(playing, segs); got != want {
			t.Errorf("a %v stream matched %v, want %v", playing, got, want)
		}
	}
	if !lengthMatches(time.Minute, []sponsorblock.Segment{{To: time.Second}}) {
		t.Error("a stream was refused when SponsorBlock gave no length to compare")
	}
}
