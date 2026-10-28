package playoutscreen

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/videopage"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
	"github.com/ygelfand/libcountertop/pkg/media/playout"
	video "github.com/ygelfand/libcountertop/pkg/media/videostream"
)

type webm []byte

func (w webm) Pictured() bool { return w != nil }

func (w webm) Pictures(context.Context) (io.ReadCloser, time.Duration, error) {
	if w == nil {
		return nil, 0, nil
	}
	return io.NopCloser(bytes.NewReader(w)), 0, nil
}

func el(id []byte, body ...[]byte) []byte {
	b := bytes.Join(body, nil)
	return append(append(append([]byte(nil), id...), 0x80|byte(len(b))), b...)
}

func vp9Head() []byte {
	return el([]byte{0x18, 0x53, 0x80, 0x67}, el([]byte{0x16, 0x54, 0xAE, 0x6B}, el(
		[]byte{0xAE},
		el([]byte{0xD7}, []byte{1}),
		el([]byte{0x86}, []byte("V_VP9")),
		el(
			[]byte{0xE0},
			el([]byte{0xB0}, []byte{0x07, 0x80}),
			el([]byte{0xBA}, []byte{0x04, 0x38}),
		),
	)))
}

func feed() (playout.Feed, *[]string) {
	var log []string
	return playout.Feed{
		Clock:   func() (time.Duration, bool) { return 10 * time.Second, true },
		Started: func() { log = append(log, "started") },
		Waiting: func(w bool) {
			if w {
				log = append(log, "waiting")
			} else {
				log = append(log, "flowing")
			}
		},
		Handing: func() bool { return false },
	}, &log
}

func TestAWebMStreamPlaysOnTheOutputsClock(t *testing.T) {
	s := New(Options{})
	var got video.Stream
	s.play = func(_ context.Context, st video.Stream) (video.Report, error) {
		got = st
		st.Started()
		st.Waiting(true)
		st.Waiting(false)
		return video.Report{}, nil
	}
	f, log := feed()
	if err := s.Play(context.Background(), nil, webm(vp9Head()), f); err != nil {
		t.Fatal(err)
	}
	if got.Codec != playback.VP9 || got.Width != 1920 || got.Height != 1080 {
		t.Errorf("stream %+v", got)
	}
	if at, running := got.Clock(); at != 10*time.Second || !running {
		t.Errorf("clock %v running %v, want the output's", at, running)
	}
	if want := []string{"started", "waiting", "flowing"}; !equal(*log, want) {
		t.Errorf("feed saw %v, want %v", *log, want)
	}
}

func TestAPictureThatFailsReportsIt(t *testing.T) {
	s := New(Options{})
	s.play = func(context.Context, video.Stream) (video.Report, error) {
		return video.Report{}, errors.New("refused")
	}
	f, _ := feed()
	if err := s.Play(context.Background(), nil, webm(vp9Head()), f); err == nil {
		t.Error("a refused picture played without error")
	}
}

func TestNoStreamIsNoPicture(t *testing.T) {
	s := New(Options{})
	s.play = func(context.Context, video.Stream) (video.Report, error) {
		t.Error("played a stream that was never there")
		return video.Report{}, nil
	}
	f, _ := feed()
	if err := s.Play(context.Background(), nil, webm(nil), f); !errors.Is(err, errNoPicture) {
		t.Errorf("err %v, want %v", err, errNoPicture)
	}
}

type sparse struct{}

func (sparse) Pictured() bool { return true }

func (sparse) Pictures(context.Context) (io.ReadCloser, time.Duration, error) {
	return nil, 0, nil
}

func (sparse) Live(context.Context) (playback.Picture, error) {
	next := func(context.Context) ([]byte, time.Duration, error) { return nil, 0, io.EOF }
	return playback.Picture{
		Codec:  playback.H264,
		Width:  1088,
		Height: 1740,
		Next:   next,
		Sparse: true,
	}, nil
}

func TestASparsePictureReachesThePlayer(t *testing.T) {
	s := New(Options{})
	var got video.Stream
	s.play = func(_ context.Context, st video.Stream) (video.Report, error) {
		got = st
		st.Started()
		return video.Report{}, nil
	}
	f, _ := feed()
	if err := s.Play(context.Background(), nil, sparse{}, f); err != nil {
		t.Fatal(err)
	}
	if !got.Sparse || got.Codec != playback.H264 {
		t.Errorf("stream %+v, want a sparse H.264 stream", got)
	}
}

func TestRetirementJoinsThePageWorker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := New(Options{})
		cancelled, done := make(chan struct{}), make(chan struct{})
		s.view, s.viewStop, s.viewDone, s.under = &videopage.Page{}, func() { close(cancelled) }, done, &video.Beneath{}
		retired := make(chan struct{})
		go func() { s.retire(0); close(retired) }()
		<-cancelled
		synctest.Wait()
		select {
		case <-retired:
			t.Fatal("view retired before its page worker exited")
		default:
		}
		close(done)
		<-retired
	})
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type waking struct {
	playout.Controls
	on bool
}

func (w waking) Wakes() bool { return w.on }

func TestACastPageWakesTheScreenWhenItsSessionDoes(t *testing.T) {
	if !(marked{waking{on: true}}).Wakes() {
		t.Fatal("a session that wakes the screen was hidden behind its marks")
	}
	if (marked{waking{}}).Wakes() {
		t.Fatal("a session that lets the screen sleep woke it")
	}
	if (marked{}).Wakes() {
		t.Fatal("controls without a say woke the screen")
	}
}
