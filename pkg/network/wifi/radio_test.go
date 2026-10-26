package wifi

import (
	"testing"

	"github.com/ygelfand/libcountertop/pkg/system/metrics"
)

func TestSignalPollReadsTheRSSILine(t *testing.T) {
	for out, want := range map[string]metrics.Reading{
		"RSSI=-52\nLINKSPEED=72\nNOISE=9999\nFREQUENCY=5180": {Value: -52, Known: true},
		"FAIL":     {},
		"RSSI=0":   {},
		"RSSI=bad": {},
	} {
		if got := parseSignal(out); got != want {
			t.Errorf("parseSignal(%q) = %+v, want %+v", out, got, want)
		}
	}
}

func TestRepeatedConnectedDoesNotQuerySupplicant(t *testing.T) {
	r := &Radio{up: true, ssid: "home", ctl: &Control{}}
	for range 100 {
		r.connected()
	}
	if !r.up || r.ssid != "home" {
		t.Fatal("repeated completion changed the association")
	}
}

func TestConnectedEventCompletesStartupWithoutVendorStateEvent(t *testing.T) {
	r := &Radio{ssid: "home"}
	r.connected()
	if p := r.Startup(); !p.Done || r.Network() != "home" || !r.Configured() {
		t.Fatalf("CONNECTED did not update association: %+v", p)
	}
	r.apply(false, "")
	if p := r.Startup(); p.Done || r.Network() != "" {
		t.Fatalf("disconnect retained association: %+v", p)
	}
}

func TestUnknownConfigurationDoesNotPretendThereIsNoNetwork(t *testing.T) {
	r := &Radio{}
	if p := r.Startup(); p.Failed {
		t.Fatalf("unchecked configuration reported failure: %+v", p)
	}
	r.configuredKnown = true
	if p := r.Startup(); !p.Failed {
		t.Fatalf("known empty configuration did not report failure: %+v", p)
	}
}
