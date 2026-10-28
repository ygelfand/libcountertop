package videostream

import (
	"testing"

	"github.com/ygelfand/libcountertop/pkg/display/surface"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

func TestAKeptDecoderServesAnySizeOfTheSameCodecAndSession(t *testing.T) {
	c := &surface.Client{}
	var b Beneath
	kept := Spec{Codec: playback.H264, W: 426, H: 240, Session: 3}
	b.Adopt(c, 7, kept)
	for _, tc := range []struct {
		name string
		spec Spec
		ok   bool
	}{
		{"larger", Spec{Codec: playback.H264, W: 1280, H: 720, Session: 3}, true},
		{"smaller", Spec{Codec: playback.H264, W: 256, H: 144, Session: 3}, true},
		{"another session", Spec{Codec: playback.H264, W: 426, H: 240, Session: 4}, false},
		{"clear", Spec{Codec: playback.H264, W: 426, H: 240}, false},
		{"another codec", Spec{Codec: playback.H264 + 1, W: 426, H: 240, Session: 3}, false},
	} {
		id, spec, ok := b.Reuse(c, tc.spec)
		if ok != tc.ok || (ok && (id != 7 || spec != kept)) {
			t.Errorf("%s: Reuse(%+v) = %d, %+v, %v; want 7, %+v, %v",
				tc.name, tc.spec, id, spec, ok, kept, tc.ok)
		}
	}
	if _, _, ok := b.Reuse(&surface.Client{}, kept); ok {
		t.Error("another helper reused the kept decoder")
	}
}
