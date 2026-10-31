package doorbell

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/camera/onvif"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func TestMuLawMatchesTheG711Table(t *testing.T) {
	cases := []struct {
		in   int16
		want byte
	}{{0, 0xff}, {-1, 0x7f}, {32767, 0x80}, {-32768, 0x00}, {1000, 0xce}}
	for _, c := range cases {
		if got := muLaw(c.in); got != c.want {
			t.Errorf("muLaw(%d) = %#02x, want %#02x", c.in, got, c.want)
		}
	}
}

func TestRFC3640SplitsAccessUnits(t *testing.T) {
	a, b := []byte{1, 2, 3}, []byte{4, 5}
	p := []byte{0, 32}
	p = binary.BigEndian.AppendUint16(p, uint16(len(a))<<3)
	p = binary.BigEndian.AppendUint16(p, uint16(len(b))<<3)
	p = append(append(p, a...), b...)
	got := rfc3640(p)
	if len(got) != 2 || string(got[0]) != string(a) || string(got[1]) != string(b) {
		t.Fatalf("got %v", got)
	}
}

func TestMicrosecondsCarryAcrossTheWrap(t *testing.T) {
	var m micros
	m.at(1<<32 - 1000)
	if got := m.at(500); got != 1500*time.Microsecond {
		t.Fatalf("got %s across the wrap, want 1.5ms", got)
	}
}

func TestTopicsMapToKinds(t *testing.T) {
	cases := map[string]Kind{
		"tns1:RuleEngine/MyRuleDetector/Visitor":       Visitor,
		"tns1:RuleEngine/MyRuleDetector/PeopleDetect":  Person,
		"tns1:RuleEngine/MyRuleDetector/VehicleDetect": Vehicle,
		"tns1:RuleEngine/MyRuleDetector/DogCatDetect":  Animal,
		"tns1:RuleEngine/MyRuleDetector/Package":       Package,
		"tns1:VideoSource/MotionAlarm":                 Motion,
		"tns1:RuleEngine/CellMotionDetector/Motion":    Motion,
	}
	for topic, want := range cases {
		if got, ok := topicKind(topic); !ok || got != want {
			t.Errorf("%s: got %q, want %q", topic, got, want)
		}
	}
	if _, ok := topicKind("tns1:Device/Trigger/DigitalInput"); ok {
		t.Error("a digital input became a doorbell event")
	}
}

func TestQualityPicksTheProfileBySize(t *testing.T) {
	profiles := []onvif.MediaProfile{
		{Token: "sub", Width: 640, Height: 480},
		{Token: "main", Width: 2560, Height: 1920},
		{Token: "ext", Width: 1280, Height: 960},
	}
	if got := pickProfile(profiles, schema.DoorbellMain).Token; got != "main" {
		t.Errorf("main picked %q", got)
	}
	if got := pickProfile(profiles, schema.DoorbellSub).Token; got != "sub" {
		t.Errorf("sub picked %q", got)
	}
}
