package urlplayer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/media/cast"
	"github.com/ygelfand/libcountertop/pkg/media/nowplaying"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

const (
	videoLane = 1
	soundLane = 2
)

func feedFiles(t *testing.T) (video, sound []byte) {
	t.Helper()
	video, err := os.ReadFile("testdata/feed-video.mp4")
	if err != nil {
		t.Fatal(err)
	}
	sound, err = os.ReadFile("testdata/feed-audio.mp4")
	if err != nil {
		t.Fatal(err)
	}
	return video, sound
}

func appendInPieces(t *testing.T, f *Feed, lane int, data []byte, piece int) []Range {
	t.Helper()
	var ranges []Range
	for len(data) > 0 {
		n := min(piece, len(data))
		var err error
		if ranges, err = f.Append(lane, data[:n], 0); err != nil {
			t.Fatal(err)
		}
		data = data[n:]
	}
	return ranges
}

func fullFeed(t *testing.T) *Feed {
	t.Helper()
	video, sound := feedFiles(t)
	f := NewFeed()
	f.Add(videoLane, `video/mp4; codecs="avc1.64000a"`)
	f.Add(soundLane, `audio/mp4; codecs="mp4a.40.2"`)
	appendInPieces(t, f, videoLane, video, 777)
	appendInPieces(t, f, soundLane, sound, 333)
	return f
}

func TestAFeedTakesSegmentsInAnyPieces(t *testing.T) {
	video, _ := feedFiles(t)
	f := NewFeed()
	f.Add(videoLane, "video/mp4")
	if _, ok := f.Opened(); ok {
		t.Fatal("a feed with no init segment says it opened")
	}
	ranges := appendInPieces(t, f, videoLane, video, 101)
	o, ok := f.Opened()
	if !ok || o.Width != 64 || o.Height != 64 {
		t.Fatalf("opened %+v %v, want 64x64", o, ok)
	}
	if len(ranges) != 1 || ranges[0].Start > 100*time.Millisecond ||
		ranges[0].End < 1900*time.Millisecond {
		t.Fatalf("buffered %v, want one range over the 2s clip", ranges)
	}
}

func TestAFeedReadsBothLanesInDecodeOrder(t *testing.T) {
	f := fullFeed(t)
	f.End()
	next := f.reader(context.Background(), 0, videoLane, soundLane)
	var videos, sounds int
	last := -time.Hour
	for {
		s, err := next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if videos == 0 && s.video && !s.key {
			t.Fatal("the first picture is not a keyframe")
		}
		if s.video {
			videos++
		} else {
			sounds++
			if s.at < last {
				t.Fatalf("sound at %v came after sound at %v", s.at, last)
			}
			last = s.at
		}
	}
	if videos < 30 || sounds < 90 {
		t.Fatalf("read %d pictures and %d sound frames, want the whole 2s clip", videos, sounds)
	}
}

func TestAFeedStartsAtTheKeyframeBeforeWhereItIsAsked(t *testing.T) {
	f := fullFeed(t)
	f.End()
	next := f.reader(context.Background(), time.Second, videoLane)
	s, err := next()
	if err != nil {
		t.Fatal(err)
	}
	if !s.key || s.at > time.Second {
		t.Fatalf("first picture at %v key=%v, want a keyframe at or before 1s", s.at, s.key)
	}
}

func TestAFeedWaitsForSegmentsStillToCome(t *testing.T) {
	video, _ := feedFiles(t)
	f := NewFeed()
	f.Add(videoLane, "video/mp4")
	next := f.reader(context.Background(), 0, videoLane)
	got := make(chan error, 1)
	go func() {
		_, err := next()
		got <- err
	}()
	select {
	case err := <-got:
		t.Fatalf("read before anything was appended: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	appendInPieces(t, f, videoLane, video, len(video))
	select {
	case err := <-got:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the reader did not wake when segments arrived")
	}
}

func TestAFeedReaderStopsWithItsContext(t *testing.T) {
	f := NewFeed()
	f.Add(videoLane, "video/mp4")
	ctx, cancel := context.WithCancel(context.Background())
	next := f.reader(ctx, 0, videoLane)
	got := make(chan error, 1)
	go func() {
		_, err := next()
		got <- err
	}()
	cancel()
	select {
	case err := <-got:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("a cancelled reader returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled reader kept waiting")
	}
}

func TestAFeedForgetsWhatThePageRemoves(t *testing.T) {
	f := fullFeed(t)
	ranges, err := f.Remove(soundLane, 0, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranges) != 1 || ranges[0].Start < time.Second-50*time.Millisecond {
		t.Fatalf("buffered %v after removing the first second", ranges)
	}
}

func TestAFeedPlaysThroughTheTrack(t *testing.T) {
	f := fullFeed(t)
	f.SetDuration(2 * time.Second)
	f.End()
	tr := New(Options{Screen: true}).track(cast.Media{}, f, 0)
	defer tr.close()
	if err := tr.ensure(); err != nil {
		t.Fatal(err)
	}
	pic, err := tr.Live(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pic.Codec != playback.H264 || pic.Width != 64 || pic.Height != 64 {
		t.Fatalf("picture %08x %dx%d, want H264 64x64", pic.Codec, pic.Width, pic.Height)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, _, err := pic.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(first, []byte{0, 0, 0, 1}) || first[4]&0x1f != 7 {
		t.Fatalf("first frame starts % x, want an Annex-B SPS", first[:min(8, len(first))])
	}
	if got := readAll(t, tr); len(got) < 3*outRate {
		t.Fatalf(
			"decoded %d samples alongside the picture, want about two seconds of stereo",
			len(got),
		)
	}
	if s := tr.Showing(); s.Length != 2*time.Second {
		t.Fatalf("the card shows a length of %v, want the page's 2s", s.Length)
	}
}

func TestASeekBeforeTheFirstSegmentDoesNotWaitForIt(t *testing.T) {
	video, sound := feedFiles(t)
	f := NewFeed()
	f.Add(videoLane, `video/mp4; codecs="avc1.64000a"`)
	f.Add(soundLane, `audio/mp4; codecs="mp4a.40.2"`)
	out := &fakeOutput{}
	p := New(Options{Output: out})
	if err := p.LoadFeed(f, 0, true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	sought := make(chan error, 1)
	go func() { sought <- p.Seek(time.Second) }()
	select {
	case err := <-sought:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the seek waited for segments the page had not appended yet")
	}
	appendInPieces(t, f, videoLane, video, 777)
	appendInPieces(t, f, soundLane, sound, 333)
	out.wait(t, "play")
	defer p.Stop()
}

func TestAPicturedFeedIsReadyOnceItsFirstPictureIsUp(t *testing.T) {
	out := &fakeOutput{}
	p := New(Options{Output: out, Screen: true})
	if err := p.LoadFeed(fullFeed(t), 0, true); err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	out.wait(t, "play")
	if p.Ready() {
		t.Fatal("ready before the screen showed a picture")
	}
	out.last().Waiting(false)
	if !p.Ready() {
		t.Fatal("not ready after the screen showed a picture")
	}
	out.last().Waiting(true)
	if p.Ready() {
		t.Fatal("still ready while the screen waits for pictures")
	}
}

func TestASeekPastAKeyframeAlreadyPlayedContinuesInPlace(t *testing.T) {
	f := fullFeed(t)
	f.SetDuration(2 * time.Second)
	out := &fakeOutput{}
	p := New(Options{Output: out})
	if err := p.LoadFeed(f, 0, true); err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	out.wait(t, "play")
	first := out.last()
	if err := p.Seek(500 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if got := out.calls(); got != "play" {
		t.Fatalf(
			"output saw %q after a seek within the playing keyframe, want only the first play",
			got,
		)
	}
	if got := first.Start(); got != 500*time.Millisecond {
		t.Fatalf("the track starts at %v after the seek, want 500ms", got)
	}
	if err := p.Seek(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	restarted := p.cur != first
	p.mu.Unlock()
	if !restarted {
		t.Fatal("a seek past the buffered media continued in place")
	}
}

func TestANewSourceTakesOverFromWhatIsPlaying(t *testing.T) {
	out := &fakeOutput{}
	p := New(Options{Output: out})
	if err := p.LoadFeed(fullFeed(t), 0, true); err != nil {
		t.Fatal(err)
	}
	out.wait(t, "play")
	if err := p.LoadFeed(fullFeed(t), 0, true); err != nil {
		t.Fatal(err)
	}
	out.wait(t, "play handoff play")
	_ = p.Stop()
	if err := p.LoadFeed(fullFeed(t), 0, true); err != nil {
		t.Fatal(err)
	}
	out.wait(t, "play handoff play stop play")
	_ = p.Stop()
}

func TestAFeedWaitsForMediaBeforeLaterSegments(t *testing.T) {
	video, _ := feedFiles(t)
	f := NewFeed()
	f.Add(videoLane, `video/mp4; codecs="avc1.64000a"`)
	if _, err := f.Append(videoLane, video, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		at    time.Duration
		ready bool
	}{
		{0, false},
		{8 * time.Second, false},
		{9500 * time.Millisecond, true},
		{10500 * time.Millisecond, true},
	} {
		if _, ok := f.keyframe(tc.at); ok != tc.ready {
			t.Errorf("at %v: ready %v, want %v", tc.at, ok, tc.ready)
		}
	}
}

func TestTheCardShowsTheStatusThePlayerFollows(t *testing.T) {
	p := New(Options{})
	tr := p.track(cast.Media{}, NewFeed(), 0)
	if _, placed := tr.Place(); placed {
		t.Fatal("placed before any status")
	}
	p.Follow(&Status{At: 90 * time.Second, Length: time.Hour})
	if at, _ := tr.Place(); at != 90*time.Second {
		t.Fatalf("a paused status places at %v, want 90s", at)
	}
	if s := tr.Showing(); s.Length != time.Hour || s.LiveWithin != 0 {
		t.Fatalf(
			"the card shows %v long, live within %v; want an hour, not live",
			s.Length,
			s.LiveWithin,
		)
	}
	p.Follow(&Status{At: 90 * time.Second, Playing: true, Live: true, Length: time.Hour})
	time.Sleep(50 * time.Millisecond)
	if at, _ := tr.Place(); at < 50*time.Millisecond || at > time.Second {
		t.Fatalf("live playback places at %v, want it moving on from where it began", at)
	}
	if s := tr.Showing(); s.Length < 50*time.Millisecond || s.LiveWithin == 0 {
		t.Fatalf("a live status shows %v long, live within %v; want the window so far, live",
			s.Length, s.LiveWithin)
	}
	p.Follow(&Status{At: 100 * time.Second, Live: true})
	time.Sleep(50 * time.Millisecond)
	at, _ := tr.Place()
	if s := tr.Showing(); at != 10*time.Second || s.Length < at+50*time.Millisecond {
		t.Fatalf(
			"paused live playback places at %v of %v, want 10s with live moving on",
			at,
			s.Length,
		)
	}
	p.Follow(nil)
	if _, placed := tr.Place(); placed {
		t.Fatal("still placed after the status was dropped")
	}
}

func TestAnAdBreakRunsItsOwnClock(t *testing.T) {
	p := New(Options{})
	tr := p.track(cast.Media{}, NewFeed(), 0)
	p.Follow(&Status{
		At:      90 * time.Second,
		Playing: true,
		Length:  time.Hour,
		Break:   &nowplaying.Break{Clip: 1, Clips: 2, At: time.Second, Length: 30 * time.Second},
		Breaks:  []time.Duration{0, 20 * time.Minute},
	})
	time.Sleep(50 * time.Millisecond)
	if at, _ := tr.Place(); at != 90*time.Second {
		t.Fatalf("an ad places the episode at %v, want it held at 90s", at)
	}
	b := tr.Showing().Break
	if b == nil || b.At < time.Second+50*time.Millisecond || b.Length != 30*time.Second {
		t.Fatalf("the card shows the break as %+v, want its clip moving on from 1s of 30s", b)
	}
	if marks := tr.Marks(); len(marks) != 2 || marks[1].From != 20*time.Minute ||
		marks[1].To != marks[1].From {
		t.Fatalf("the breaks are marked as %+v, want a point at 0 and at 20m", marks)
	}
}

func TestAFeedReaderWaitsAtAGap(t *testing.T) {
	video, _ := feedFiles(t)
	f := NewFeed()
	f.Add(videoLane, `video/mp4; codecs="avc1.64000a"`)
	for _, at := range []time.Duration{0, 10 * time.Second} {
		if _, err := f.Append(videoLane, video, at); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	next := f.reader(ctx, 0, videoLane)
	for {
		s, err := next()
		if err != nil {
			break
		}
		if s.at >= 10*time.Second {
			t.Fatalf("the reader crossed the gap to %v", s.at)
		}
	}
}

func TestAFrameCountsOnceTheClockReachesIt(t *testing.T) {
	tr := New(Options{}).track(cast.Media{}, nil, 0)
	for _, at := range []time.Duration{0, 40, 80, 120} {
		tr.queue(at * time.Millisecond)
	}
	for _, tc := range []struct {
		clock time.Duration
		want  int64
	}{
		{0, 1},
		{50 * time.Millisecond, 2},
		{50 * time.Millisecond, 2},
		{time.Second, 4},
	} {
		if got := tr.shownBy(tc.clock); got != tc.want {
			t.Errorf("at %v: %d frames shown, want %d", tc.clock, got, tc.want)
		}
	}
}

func TestWebMSoundBesideMP4PicturesPlaysThroughTheTrack(t *testing.T) {
	video, _ := feedFiles(t)
	sound, err := os.ReadFile("testdata/feed-audio.webm")
	if err != nil {
		t.Fatal(err)
	}
	f := NewFeed()
	f.Add(videoLane, `video/mp4; codecs="avc1.64000a"`)
	f.Add(soundLane, `audio/webm; codecs="opus"`)
	appendInPieces(t, f, videoLane, video, 777)
	ranges := appendInPieces(t, f, soundLane, sound, 211)
	if len(ranges) != 1 || ranges[0].End < 1900*time.Millisecond {
		t.Fatalf("webm sound buffered %v, want one range over the 2s clip", ranges)
	}
	f.End()
	tr := New(Options{Screen: true}).track(cast.Media{}, f, 0)
	defer tr.close()
	if err := tr.ensure(); err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, tr); len(got) < 3*outRate {
		t.Fatalf("decoded %d samples of webm opus, want about two seconds of stereo", len(got))
	}
}

func TestOnlyContainersAPageAppendsAreFed(t *testing.T) {
	p := New(Options{Screen: true})
	for typ, want := range map[string]bool{
		`video/mp4; codecs="avc1.4d401e"`: true,
		`audio/webm; codecs="opus"`:       true,
		`video/webm; codecs="vp9"`:        false,
		`audio/mpeg`:                      false,
	} {
		if got := p.Feeds(typ, 0, 0, 0); got != want {
			t.Errorf("feeds %q = %v, want %v", typ, got, want)
		}
	}
}
