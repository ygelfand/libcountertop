package noiselayer

import (
	"slices"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/audio/noise"
)

type fakeStream struct {
	playing []string
	stops   int
}

func (f *fakeStream) PlayNoise(sounds ...string) { f.playing = sounds }
func (f *fakeStream) Noise() []string            { return f.playing }
func (f *fakeStream) Stop()                      { f.playing = nil; f.stops++ }

func selects(t *testing.T, l *Layers) []*esphome.Select {
	t.Helper()
	var out []*esphome.Select
	for _, e := range l.Entities() {
		out = append(out, e.(*esphome.Select))
	}
	if len(out) != count {
		t.Fatalf("%d layers, want %d", len(out), count)
	}
	return out
}

func TestLayersStartOffWithOffFirst(t *testing.T) {
	for _, sel := range selects(t, New(&fakeStream{}, 0)) {
		if sel.Get() != Off || sel.Options[0] != Off {
			t.Errorf(
				"%s is %q with options %v, want %q first and chosen",
				sel.ObjectID,
				sel.Get(),
				sel.Options,
				Off,
			)
		}
	}
}

func TestChoosingLayersPlaysThemTogether(t *testing.T) {
	s := &fakeStream{}
	sel := selects(t, New(s, 0))
	names := noise.Names()

	sel[0].OnCommand(names[0])
	sel[1].OnCommand(names[1])
	if !slices.Equal(s.playing, names[:2]) {
		t.Fatalf("playing %v, want %v", s.playing, names[:2])
	}

	sel[0].OnCommand(Off)
	if !slices.Equal(s.playing, names[1:2]) {
		t.Fatalf("playing %v, want only %v", s.playing, names[1])
	}

	sel[1].OnCommand(Off)
	if s.playing != nil || s.stops != 1 {
		t.Fatalf("playing %v after %d stops, want silence after one", s.playing, s.stops)
	}
}

func TestTurningOffDoesNotStopOtherMedia(t *testing.T) {
	s := &fakeStream{}
	sel := selects(t, New(s, 0))

	sel[0].OnCommand(Off)
	if s.stops != 0 {
		t.Errorf("stopped the stream %d times while it was not playing noise", s.stops)
	}
}

func TestAnUnknownSoundIsIgnored(t *testing.T) {
	s := &fakeStream{}
	sel := selects(t, New(s, 0))

	sel[0].OnCommand("off")
	if sel[0].Get() != Off || s.playing != nil {
		t.Errorf("layer is %q, playing %v", sel[0].Get(), s.playing)
	}
}

func TestSyncClearsLayersOnceNoiseStops(t *testing.T) {
	s := &fakeStream{}
	l := New(s, 0)
	sel := selects(t, l)

	sel[0].OnCommand(noise.Names()[0])
	l.Sync()
	if sel[0].Get() == Off {
		t.Fatal("sync cleared a layer that is still playing")
	}

	s.playing = nil
	l.Sync()
	if sel[0].Get() != Off {
		t.Errorf("layer is %q after the noise stopped, want %q", sel[0].Get(), Off)
	}
}

func TestSyncOnNoLayersIsSafe(t *testing.T) {
	var l *Layers
	l.Sync()
}
