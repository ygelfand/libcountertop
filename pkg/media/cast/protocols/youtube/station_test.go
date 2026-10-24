package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/media/cast"
	"github.com/ygelfand/libcountertop/pkg/media/cast/preferences"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

func TestLoungeValuesReadAsStrings(t *testing.T) {
	got := fields(
		json.RawMessage(
			`{"videoId":"Y7nDX15KJ5M","currentIndex":"13","currentTime":0,"flag":true,"none":null}`,
		),
	)
	want := map[string]string{
		"videoId":      "Y7nDX15KJ5M",
		"currentIndex": "13",
		"currentTime":  "0",
		"flag":         "true",
	}
	if len(got) != len(want) {
		t.Errorf("read %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s reads %q, want %q", k, got[k], v)
		}
	}
	if len(fields(json.RawMessage(`nonsense`))) != 0 {
		t.Error("an unreadable payload yielded fields")
	}
}

func TestPlaylistNavigationStaysInsideTheList(t *testing.T) {
	s := &station{ids: []string{"a", "b", "c"}, index: 0}
	if prev, next := s.around(); prev || !next {
		t.Errorf("at the start: previous %v next %v", prev, next)
	}
	s.step(-1)
	if s.index != 0 {
		t.Errorf("stepping back from the start moved to %d", s.index)
	}

	s.index = 2
	if prev, next := s.around(); !prev || next {
		t.Errorf("at the end: previous %v next %v", prev, next)
	}
	s.step(1)
	if s.index != 2 {
		t.Errorf("stepping past the end moved to %d", s.index)
	}
}

func TestAPlayOrPauseWhileOpeningIsKeptForTheNextTrack(t *testing.T) {
	s := &station{opening: func() {}}
	s.pause()
	if !s.held {
		t.Error("a pause while the next track opened was lost")
	}
	s.play()
	if s.held {
		t.Error("a play while the next track opened did not undo the pause")
	}
	if !s.whileOpening(false) {
		t.Error("with a track opening and none loaded, a play was not kept for it")
	}
	s.cur = &track{}
	if s.whileOpening(true) {
		t.Error("with a track loaded, a pause was kept for later instead of applied")
	}
}

func TestOnlyYouTubeAndYouTubeMusicGetAScreen(t *testing.T) {
	k := &screens{id: preferences.YouTube{Video: "v", Music: "m"}}
	if _, _, ok := k.screen("32EAB1DF"); ok {
		t.Error("YouTube TV was offered a screen")
	}
	for _, app := range []string{"233637DE", "2DB7CC49"} {
		if _, _, ok := k.screen(app); !ok {
			t.Errorf("%s was given no screen to connect to", app)
		}
	}
}

func TestTimesAreSecondsToTheMillisecond(t *testing.T) {
	if got := secs(173114 * time.Millisecond); got != "173.114" {
		t.Errorf("read %s", got)
	}
}

type counter struct {
	mu      sync.Mutex
	samples int
	err     error
	done    chan struct{}
}

func (c *counter) Play(src playback.Source) {
	go func() {
		defer close(c.done)
		buf := make([]int16, 1920)
		for {
			n, err := src.Read(context.Background(), buf)
			c.mu.Lock()
			c.samples += n
			c.err = err
			c.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
}
func (c *counter) Stop(playback.Source)                 {}
func (c *counter) Changed(playback.Source)              {}
func (c *counter) Behind(playback.Source) time.Duration { return 0 }

func (c *counter) Handoff(src playback.Source) { c.Stop(src) }
func (c *counter) Pause(playback.Source)       {}
func (c *counter) Resume(playback.Source)      {}
func (c *counter) Attend(*playback.Session)    {}

func (c *counter) Position(src playback.Source) (time.Duration, bool) { return src.Start(), false }
func (c *counter) Failed(error)                                       {}

func TestAVideoPlaysThroughToItsEnd(t *testing.T) {
	if os.Getenv("COUNTERTOP_LIVE") == "" {
		t.Skip("set COUNTERTOP_LIVE=1 to play a real video")
	}
	out := &counter{done: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	st := newStation(
		ctx,
		ThemeMusic,
		cast.Env{HTTP: &http.Client{Timeout: time.Minute}, Output: out},
	)
	st.handle(
		Message{
			Name: "setPlaylist",
			Payload: json.RawMessage(
				`{"videoId":"Y7nDX15KJ5M","currentIndex":"0","currentTime":"0"}`,
			),
		},
	)

	select {
	case <-out.done:
	case <-ctx.Done():
		t.Fatal("the video did not play through")
	}
	if !errors.Is(out.err, io.EOF) {
		t.Errorf("the source ended with %v, want the end of the stream", out.err)
	}
	got := time.Duration(out.samples/channels) * time.Second / rate
	if got < 172*time.Second || got > 174*time.Second {
		t.Errorf("played %s, want about 2m53s", got)
	}
}

func TestTheStationPublishesToCast(t *testing.T) {
	var got []*cast.Published
	s := newStation(
		context.Background(),
		ThemeYouTube,
		cast.Env{Publish: func(_ cast.Controls, p *cast.Published) { got = append(got, p) }},
	)
	s.list, s.index = "PL1", 2
	s.reportLoading("vid", 0)
	s.report(stateStopped)
	if len(got) != 2 || got[1] != nil {
		t.Fatalf("published %+v", got)
	}
	p := got[0]
	if p.Media.ContentID != "vid" || p.Media.ContentType != "x-youtube/video" ||
		p.State != cast.StateBuffering ||
		p.Commands != castCommands {
		t.Errorf("loading %+v", p)
	}
	if string(p.Media.CustomData) != `{"currentIndex":2,"listId":"PL1"}` ||
		string(p.Custom) != `{"playerState":3}` {
		t.Errorf("custom %s %s", p.Media.CustomData, p.Custom)
	}
}
