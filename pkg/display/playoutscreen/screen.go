package playoutscreen

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/videopage"
	"github.com/ygelfand/libcountertop/pkg/media/cenc"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
	"github.com/ygelfand/libcountertop/pkg/media/playout"
	video "github.com/ygelfand/libcountertop/pkg/media/videostream"
)

const (
	viewGrace   = 5 * time.Second
	pictureWait = 30 * time.Second
)

var errNoPicture = errors.New("playout: no picture")

type Options struct {
	NewPage  func(videopage.Controls) *videopage.Page
	NewUnder func() *video.Beneath
	Video    func(context.Context, *video.Beneath, video.Stream) (video.Report, error)
}

type Screen struct {
	options Options
	play    func(context.Context, video.Stream) (video.Report, error)

	mu        sync.Mutex
	view      *videopage.Page
	viewStop  context.CancelFunc
	viewDone  chan struct{}
	under     *video.Beneath
	viewGen   int
	viewTimer *time.Timer
}

var _ playout.Screen = (*Screen)(nil)

func New(o Options) *Screen { return &Screen{options: o} }

func (s *Screen) Hold(c playout.Controls) { s.hold(c) }

func (s *Screen) Open(c playout.Controls) {
	s.mu.Lock()
	view := s.view
	s.mu.Unlock()
	if view != nil {
		view.Show()
		return
	}
	s.hold(c)
}

func (s *Screen) Held() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view != nil
}

func (s *Screen) Release(handing bool) {
	s.mu.Lock()
	view := s.view
	if view == nil {
		s.mu.Unlock()
		return
	}
	gen := s.viewGen
	if s.viewTimer != nil {
		s.viewTimer.Stop()
	}
	s.viewTimer = time.AfterFunc(viewGrace, func() { s.retire(gen) })
	s.mu.Unlock()
	if handing {
		view.SetLoading(true)
	} else {
		view.SetPicture(false)
	}
}

func (s *Screen) Leave() {
	s.mu.Lock()
	gen := s.viewGen
	s.mu.Unlock()
	s.retire(gen)
}

func (s *Screen) Say(note string) {
	if view := s.current(); view != nil {
		view.SetNote(note)
	}
}

func (s *Screen) Fail(note string) {
	if view := s.current(); view != nil {
		view.SetPicture(false)
		view.SetNote(note)
	}
}

func (s *Screen) Logo(raw []byte, id uint64) {
	view := s.current()
	if view == nil {
		return
	}
	if img := ui.Picture(raw, id); img != nil {
		view.SetLogo(img)
	}
}

func (s *Screen) Backdrop(img *image.RGBA) {
	if view := s.current(); view != nil {
		view.SetNote("")
		view.SetBackdrop(ui.FromRGBA(img))
	}
}

func (s *Screen) Standby(c playout.Controls, note string, raw []byte, id uint64) {
	view, _ := s.hold(c)
	view.SetPicture(false)
	view.SetLoading(false)
	view.SetNote(note)
	if img := ui.Picture(raw, id); img != nil {
		view.SetLogo(img)
	}
}

func (s *Screen) Play(
	ctx context.Context,
	c playout.Controls,
	pics playback.Pictures,
	f playout.Feed,
) error {
	play := s.play
	var view *videopage.Page
	if play == nil {
		v, under := s.hold(c)
		view = v
		defer func() { s.Release(f.Handing()) }()
		play = func(ctx context.Context, st video.Stream) (video.Report, error) {
			return s.options.Video(ctx, under, st)
		}
	}
	return s.once(ctx, pics, f, play, view)
}

func (s *Screen) current() *videopage.Page {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view
}

func (s *Screen) hold(c playout.Controls) (*videopage.Page, *video.Beneath) {
	controls := marked{c}
	s.mu.Lock()
	s.viewGen++
	if s.viewTimer != nil {
		s.viewTimer.Stop()
		s.viewTimer = nil
	}
	slog.Debug("the video page is held", "new", s.view == nil, "by", fmt.Sprintf("%T", c))
	if s.view == nil {
		ctx, stop := context.WithCancel(context.Background())
		s.view, s.viewStop, s.under = s.options.NewPage(controls), stop, s.options.NewUnder()
		view := s.view
		s.under.Stalled = func() { view.SetLoading(true) }
		s.under.Resumed = func() { view.SetLoading(false) }
		done := make(chan struct{})
		s.viewDone = done
		go func() { defer close(done); view.Run(ctx) }()
	} else {
		s.view.Follow(controls)
		s.view.SetLoading(true)
	}
	view, under := s.view, s.under
	s.mu.Unlock()
	return view, under
}

func (s *Screen) retire(gen int) {
	s.mu.Lock()
	if s.viewGen != gen || s.view == nil {
		s.mu.Unlock()
		return
	}
	stop, done, under := s.viewStop, s.viewDone, s.under
	s.view, s.viewStop, s.viewDone, s.under, s.viewTimer = nil, nil, nil, nil, nil
	s.mu.Unlock()
	stop()
	<-done
	under.Clear()
}

func (s *Screen) once(
	ctx context.Context,
	pics playback.Pictures,
	f playout.Feed,
	play func(context.Context, video.Stream) (video.Report, error),
	view *videopage.Page,
) error {
	actx, cancel := context.WithCancel(ctx)
	defer cancel()
	var began, late atomic.Bool
	watch := time.AfterFunc(pictureWait, func() {
		if !began.Load() {
			late.Store(true)
			cancel()
		}
	})
	defer watch.Stop()

	st, closer, err := streamOf(actx, pics)
	if err != nil {
		return err
	}
	defer closer.Close()
	st.Clock = f.Clock
	st.Started = func() {
		began.Store(true)
		f.Started()
		if view != nil {
			view.SetFrame(int(st.Width), int(st.Height))
			view.SetPicture(true)
		}
	}
	st.Waiting = func(w bool) {
		f.Waiting(w)
		if view != nil {
			view.SetLoading(w)
		}
	}
	rep, err := play(actx, st)
	slog.Info(
		"the video is over",
		"shown",
		rep.Shown,
		"dropped",
		rep.Dropped,
		"size",
		fmt.Sprintf("%dx%d", st.Width, st.Height),
		"err",
		err,
	)
	if late.Load() {
		return errNoPicture
	}
	return err
}

func streamOf(ctx context.Context, pics playback.Pictures) (video.Stream, io.Closer, error) {
	if l, ok := pics.(playback.Live); ok {
		p, err := l.Live(ctx)
		if err != nil {
			return video.Stream{}, nil, err
		}
		if p.Sealed != nil {
			if !p.H264 {
				return video.Stream{}, nil, errNoPicture
			}
			st := video.Stream{
				Codec:   video.H264,
				Width:   uint32(p.Width),
				Height:  uint32(p.Height),
				Session: p.Session,
				Source:  sealedFrames(p.Sealed),
			}
			return st, io.NopCloser(nil), nil
		}
		if p.Next != nil {
			if !p.H264 {
				return video.Stream{}, nil, errNoPicture
			}
			st := video.Stream{
				Codec:  video.H264,
				Width:  uint32(p.Width),
				Height: uint32(p.Height),
				Source: liveFrames(p.Next),
				Sparse: p.Sparse,
			}
			return st, io.NopCloser(nil), nil
		}
	}
	stream, from, err := pics.Pictures(ctx)
	if err != nil {
		return video.Stream{}, nil, err
	}
	if stream == nil {
		return video.Stream{}, nil, errNoPicture
	}
	st, err := video.WebM(stream, from)
	if err != nil {
		stream.Close()
		return video.Stream{}, nil, err
	}
	return st, stream, nil
}

type liveFrames func(context.Context) ([]byte, time.Duration, error)

func (f liveFrames) Next(ctx context.Context) (video.Frame, error) {
	data, at, err := f(ctx)
	return video.Frame{Data: data, At: at}, err
}

type sealedFrames func(context.Context) ([]byte, time.Duration, *cenc.Crypt, error)

func (f sealedFrames) Next(ctx context.Context) (video.Frame, error) {
	data, at, k, err := f(ctx)
	return video.Frame{Data: data, At: at, Crypt: k}, err
}

type marked struct{ playout.Controls }

func (m marked) Wakes() bool {
	w, ok := m.Controls.(interface{ Wakes() bool })
	return ok && w.Wakes()
}

func (m marked) Marks() []videopage.Mark {
	var out []videopage.Mark
	for _, k := range m.Controls.Marks() {
		out = append(out, videopage.Mark{From: k.From, To: k.To,
			Color: theme.Color{R: byte(k.RGB >> 16), G: byte(k.RGB >> 8), B: byte(k.RGB)}})
	}
	return out
}
