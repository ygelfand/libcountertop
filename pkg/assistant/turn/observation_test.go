package turn

import (
	"context"
	"testing"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"
	"github.com/ygelfand/go-esphome-device/api"

	"github.com/ygelfand/libcountertop/pkg/assistant/activity"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type observationMic struct{}

func (observationMic) Listen(
	string,
) (<-chan []int16, func()) {
	return make(chan []int16), func() {}
}
func (observationMic) Recent(time.Duration) []int16 { return nil }
func (observationMic) Gain() float64                { return 1 }
func (observationMic) SetAdapting(bool)             {}
func (observationMic) Clipped() uint64              { return 0 }

type observationSound struct{}

func (observationSound) Claim(string, func(context.Context, Speaker) error) Claim { return nil }
func (observationSound) Busy() bool                                               { return false }
func (observationSound) Silence()                                                 {}
func (observationSound) Duck(bool)                                                {}

type observationRecorder struct{}

func (observationRecorder) Opens(string, int)      {}
func (observationRecorder) Frame([]byte)           {}
func (observationRecorder) Closes()                {}
func (observationRecorder) Seconds(string) float64 { return 0 }

type observationWords struct{}

func (observationWords) Read() schema.Wake               { return schema.Wake{} }
func (observationWords) Update(func(*schema.Wake)) error { return nil }

func TestObservationDistinguishesAcceptedStarts(t *testing.T) {
	for _, followUp := range []bool{false, true} {
		vs := &esphome.VoiceSatellite{}
		if err := vs.Handle(
			context.Background(),
			&esphome.Conn{},
			&api.SubscribeVoiceAssistantRequest{Subscribe: true},
		); err != nil {
			t.Fatal(err)
		}
		c := NewConversation(vs, Dependencies{
			Source:   observationMic{},
			Sound:    observationSound{},
			Log:      activity.New(activity.Options{}),
			Recorder: observationRecorder{},
			Words: Words{
				Settings: observationWords{},
			},
			Muted: func() (bool, error) { return false, nil },
		})
		c.Word = func(int) (string, bool) { return "synthetic", true }
		var kinds []string
		c.OnTrace = func(kind string, slot int) {
			if slot != 1 {
				t.Errorf("slot=%d", slot)
			}
			kinds = append(kinds, kind)
		}
		c.start(nextTurn{slot: 1, followUp: followUp})
		c.start(nextTurn{slot: 1}) // Ignored while already listening.
		c.stopStreaming()
		c.disarm()
		want := "turn_start"
		if followUp {
			want = "follow_up"
		}
		if len(kinds) != 1 || kinds[0] != want || c.phase != Listening {
			t.Fatalf("trace=%v phase=%v", kinds, c.phase)
		}
	}
}

func TestObservationDoesNotReportMutedStart(t *testing.T) {
	c := NewConversation(&esphome.VoiceSatellite{}, Dependencies{
		Muted: func() (bool, error) { return true, nil }, Failure: func() {},
	})
	c.OnTrace = func(string, int) { t.Error("muted start was reported as accepted") }
	c.start(nextTurn{})
	if c.phase != Idle {
		t.Fatal("muted start changed phase")
	}
}
