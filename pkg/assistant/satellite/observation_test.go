package satellite

import (
	"context"
	"reflect"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"
	"github.com/ygelfand/go-esphome-device/api"

	"github.com/ygelfand/libcountertop/pkg/assistant/turn"
)

func TestObservationTracksSubscriptionTransitions(t *testing.T) {
	vs := &esphome.VoiceSatellite{}
	type trace struct {
		Kind string
		Slot int
	}
	var got []trace
	s := New(vs, turn.Dependencies{}, Options{Slots: 2, Timers: &fakeTimers{},
		OnTrace: func(kind string, slot int) { got = append(got, trace{kind, slot}) },
	})
	if s.Turn().OnTrace == nil {
		t.Fatal("turn observer was not connected")
	}
	conn := &esphome.Conn{}
	for _, subscribed := range []bool{true, true, false} {
		if err := vs.Handle(
			context.Background(),
			conn,
			&api.SubscribeVoiceAssistantRequest{Subscribe: subscribed},
		); err != nil {
			t.Fatal(err)
		}
	}
	want := []trace{{"api_subscribed", -1}, {"api_unsubscribed", -1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("traces=%v, want %v", got, want)
	}
	if vs.Subscribed() {
		t.Fatal("observer changed subscription state")
	}
}
