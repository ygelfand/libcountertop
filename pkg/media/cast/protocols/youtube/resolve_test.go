package youtube

import (
	"net/url"
	"testing"

	yt "github.com/kkdai/youtube/v2"
)

func TestTheBestOpusFormatIsChosen(t *testing.T) {
	formats := yt.FormatList{
		{ItagNo: 140, MimeType: `audio/mp4; codecs="mp4a.40.2"`, Bitrate: 131104},
		{ItagNo: 249, MimeType: `audio/webm; codecs="opus"`, Bitrate: 55420},
		{ItagNo: 251, MimeType: `audio/webm; codecs="opus"`, Bitrate: 147426},
		{ItagNo: 248, MimeType: `video/webm; codecs="vp9"`, Bitrate: 2000000},
	}
	if f := bestOpus(formats); f == nil || f.ItagNo != 251 {
		t.Errorf("chose %+v", f)
	}
	if f := bestOpus(formats[:1]); f != nil {
		t.Errorf("chose %d from a list with no opus", f.ItagNo)
	}
}

func TestTheOriginalLanguageIsChosenOverADubOrAProcessedCopy(t *testing.T) {
	const opus = `audio/webm; codecs="opus"`
	at := func(xtags string) string {
		return "https://gv.example/videoplayback?itag=251&xtags=" + url.QueryEscape(xtags)
	}
	var def, dub yt.Format
	def.AudioTrack = &struct {
		DisplayName    string `json:"displayName"`
		ID             string `json:"id"`
		AudioIsDefault bool   `json:"audioIsDefault"`
	}{AudioIsDefault: true}
	dub.AudioTrack = &struct {
		DisplayName    string `json:"displayName"`
		ID             string `json:"id"`
		AudioIsDefault bool   `json:"audioIsDefault"`
	}{}
	format := func(track yt.Format, bitrate int, xtags string) yt.Format {
		track.ItagNo, track.MimeType, track.Bitrate, track.URL = 251, opus, bitrate, at(xtags)
		return track
	}

	for _, c := range []struct {
		name    string
		formats yt.FormatList
		want    int
	}{
		{"a louder dub, and processed copies of the original", yt.FormatList{
			format(dub, 158163, "acont=dubbed:lang=es-US"),
			format(def, 150000, "acont=original:lang=en-US:drc=1"),
			format(def, 140000, "acont=original:lang=en-US:vb=1"),
			format(def, 128000, "acont=original:lang=en-US"),
		}, 128000},
		{"only the default flag to go on", yt.FormatList{
			format(dub, 158163, ""),
			format(def, 120000, ""),
		}, 120000},
		{"one language, one of them compressed", yt.FormatList{
			format(yt.Format{}, 124446, "drc=1"),
			format(yt.Format{}, 112788, ""),
		}, 112788},
	} {
		if f := bestOpus(c.formats); f == nil || f.Bitrate != c.want {
			t.Errorf("%s: chose %+v, want the one at %d", c.name, f, c.want)
		}
	}
}

func TestAChunkIsFifteenSecondsOfItsStream(t *testing.T) {
	for _, at := range []struct {
		f    yt.Format
		want int64
	}{
		{yt.Format{ItagNo: 251, ContentLength: 3012048, ApproxDurationMs: "173121"}, 260970},
		{yt.Format{ItagNo: 248, ContentLength: 6934788, ApproxDurationMs: "173080"}, 600990},
		{yt.Format{ItagNo: 1, ContentLength: 3012048}, chunkFloor},
		{yt.Format{ItagNo: 2, ContentLength: 1000, ApproxDurationMs: "173121"}, chunkFloor},
	} {
		if got := chunkFor(&at.f); got != at.want {
			t.Errorf("itag %d: chunk %d, want %d", at.f.ItagNo, got, at.want)
		}
	}
}
