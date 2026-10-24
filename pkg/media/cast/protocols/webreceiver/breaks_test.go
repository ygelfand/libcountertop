package webreceiver

import (
	"reflect"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/media/cast"
	"github.com/ygelfand/libcountertop/pkg/media/nowplaying"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

func TestAnAdBreakHoldsTheEpisodeAndCountsTheClip(t *testing.T) {
	r := New(
		cast.Env{Output: quietOutput{}, Video: func() playback.Target { return playback.Target{} }},
		Options{},
	)
	r.watch(`{"type":"MEDIA_STATUS","status":[{"mediaSessionId":2,"playerState":"PLAYING",` +
		`"currentTime":0,"breakStatus":{"breakId":"b0","breakClipId":"c1","currentBreakClipTime":4},` +
		`"media":{"streamType":"BUFFERED","duration":3136,` +
		`"breaks":[{"id":"b1","position":837,"breakClipIds":["c3"]},` +
		`{"id":"b0","position":0,"breakClipIds":["c0","c1"]}],` +
		`"breakClips":[{"id":"c0","duration":62},{"id":"c1","duration":32},{"id":"c3","duration":16}]}}]}`)

	if want := (&nowplaying.Break{Clip: 2, Clips: 2, At: 4 * time.Second, Length: 32 * time.Second}); !reflect.DeepEqual(
		r.shown.Break,
		want,
	) {
		t.Fatalf("the break is %+v, want %+v", r.shown.Break, want)
	}
	if want := []time.Duration{0, 837 * time.Second}; !reflect.DeepEqual(r.shown.Breaks, want) {
		t.Fatalf("breaks are marked at %v, want %v", r.shown.Breaks, want)
	}

	r.watch(
		`{"type":"MEDIA_STATUS","status":[{"mediaSessionId":2,"playerState":"PLAYING","currentTime":1}]}`,
	)
	if r.shown.Break != nil {
		t.Fatalf("the break is %+v after it ended, want none", r.shown.Break)
	}
	if want := []time.Duration{0, 837 * time.Second}; !reflect.DeepEqual(r.shown.Breaks, want) {
		t.Fatalf(
			"breaks are marked at %v after a status without media, want %v",
			r.shown.Breaks,
			want,
		)
	}

	r.watch(
		`{"type":"MEDIA_STATUS","status":[{"mediaSessionId":3,"playerState":"PLAYING","currentTime":1}]}`,
	)
	if len(r.shown.Breaks) != 0 {
		t.Fatalf("breaks are marked at %v for a new media session, want none", r.shown.Breaks)
	}
}
