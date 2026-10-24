package urlplayer

import (
	"testing"

	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

func TestPlaysAnswersFromWhatTheBoardCanShow(t *testing.T) {
	panel := New(Options{
		Screen: true,
		Target: func() playback.Target { return playback.Target{Tallest: 720, Fastest: 30} },
	})
	speaker := New(Options{})
	for _, c := range []struct {
		mime                string
		width, height, rate int
		panel, speaker      bool
	}{
		{`audio/mpeg`, 0, 0, 0, true, true},
		{`audio/mp4; codecs="mp4a.40.2"`, 0, 0, 0, true, true},
		{`audio/mp4; codecs="mp4a.40.5"`, 0, 0, 0, false, false},
		{`audio/webm; codecs="opus"`, 0, 0, 0, true, true},
		{`audio/webm; codecs="vorbis"`, 0, 0, 0, true, true},
		{`video/x-matroska; codecs="avc1.64001f, mp3"`, 1280, 720, 30, true, false},
		{`video/x-matroska; codecs="flac"`, 0, 0, 0, true, false},
		{`audio/x-matroska; codecs="flac"`, 0, 0, 0, true, true},
		{`audio/mp4; codecs="mp4a.40.34"`, 0, 0, 0, true, true},
		{`audio/flac`, 0, 0, 0, true, true},
		{`video/mp4; codecs="avc1.64001f, mp4a.40.2"`, 1280, 720, 30, true, false},
		{`video/mp4; codecs="avc1.4d401e"`, 1920, 1080, 30, false, false},
		{`video/mp4; codecs="avc1.64001f"`, 1280, 720, 60, false, false},
		{`video/mp4; codecs="avc1.6e001f"`, 1280, 720, 30, false, false},
		{`video/mp4; codecs="hvc1.1.6.L93.B0"`, 1280, 720, 30, false, false},
		{`video/webm; codecs="vp9, opus"`, 1280, 720, 30, false, false},
		{`video/mp4`, 0, 0, 0, true, false},
		{`application/x-mpegURL; codecs="avc1.42e01e, mp4a.40.2"`, 640, 360, 25, true, false},
		{`application/vnd.apple.mpegurl; codecs="mp4a.40.2"`, 0, 0, 0, true, true},
		{`audio/ogg`, 0, 0, 0, false, false},
		{`not a type`, 0, 0, 0, false, false},
	} {
		if got := panel.Plays(c.mime, c.width, c.height, c.rate); got != c.panel {
			t.Errorf(
				"panel Plays(%q, %dx%d@%d) = %v, want %v",
				c.mime,
				c.width,
				c.height,
				c.rate,
				got,
				c.panel,
			)
		}
		if got := speaker.Plays(c.mime, c.width, c.height, c.rate); got != c.speaker {
			t.Errorf(
				"speaker Plays(%q, %dx%d@%d) = %v, want %v",
				c.mime,
				c.width,
				c.height,
				c.rate,
				got,
				c.speaker,
			)
		}
	}
}
