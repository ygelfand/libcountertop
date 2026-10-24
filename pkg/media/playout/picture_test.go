package playout

import (
	"context"
	"errors"
	"image"
	"io"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

type fakeScreen struct {
	play func(ctx context.Context, f Feed) error
}

func (s *fakeScreen) Hold(Controls)                            {}
func (s *fakeScreen) Open(Controls)                            {}
func (s *fakeScreen) Held() bool                               { return false }
func (s *fakeScreen) Release(bool)                             {}
func (s *fakeScreen) Leave()                                   {}
func (s *fakeScreen) Say(string)                               {}
func (s *fakeScreen) Fail(string)                              {}
func (s *fakeScreen) Logo([]byte, uint64)                      {}
func (s *fakeScreen) Backdrop(*image.RGBA)                     {}
func (s *fakeScreen) Standby(Controls, string, []byte, uint64) {}

func (s *fakeScreen) Play(ctx context.Context, _ Controls, _ playback.Pictures, f Feed) error {
	return s.play(ctx, f)
}

func screened(play func(ctx context.Context, f Feed) error) *Output {
	o := newOutput()
	o.q = &fakeQueue{}
	o.options.Screen = &fakeScreen{play: play}
	return o
}

type pictured struct {
	ending
	shows bool
	reads int
}

func (p *pictured) Read(ctx context.Context, _ []int16) (int, error) {
	p.mu.Lock()
	p.reads++
	p.mu.Unlock()
	return 0, nil
}

func (p *pictured) readCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reads
}

func (p *pictured) Pictured() bool { return p.shows }

func (p *pictured) Pictures(context.Context) (io.ReadCloser, time.Duration, error) {
	return nil, 0, nil
}

func TestTheSoundWaitsForThePicture(t *testing.T) {
	start := make(chan func(), 1)
	o := screened(func(ctx context.Context, f Feed) error {
		start <- f.Started
		<-ctx.Done()
		return nil
	})
	src := &pictured{shows: true}
	o.Play(src)
	t.Cleanup(func() { o.Stop(src) })

	started := <-start
	time.Sleep(150 * time.Millisecond)
	if n := src.readCount(); n != 0 {
		t.Fatalf("the sound was read %d times before any picture", n)
	}
	started()
	deadline := time.Now().Add(2 * time.Second)
	for src.readCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if src.readCount() == 0 {
		t.Error("the sound never started after the first picture")
	}
}

func TestAPicturedSourcePlaysAgainstWhatHasBeenHeard(t *testing.T) {
	got := make(chan Feed, 1)
	o := screened(func(_ context.Context, f Feed) error {
		got <- f
		return nil
	})
	src := &pictured{ending: ending{start: 10 * time.Second}, shows: true}
	o.Play(src)
	t.Cleanup(func() { o.Stop(src) })

	var f Feed
	select {
	case f = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("the pictures never played")
	}
	if at, running := f.Clock(); at != 10*time.Second || !running {
		t.Errorf("clock %v running %v, want 10s: where it starts, nothing heard yet", at, running)
	}
	o.Pause(src)
	if _, running := f.Clock(); running {
		t.Error("the clock runs while paused")
	}
	o.Resume(src)
	f.Waiting(true)
	if _, running := f.Clock(); running {
		t.Error("the clock runs while the picture is waiting for frames")
	}
	f.Waiting(false)
	if _, running := f.Clock(); !running {
		t.Error("the clock stays stopped once the picture has frames again")
	}
}

func TestAPictureThatFailsStopsTheSoundToo(t *testing.T) {
	o := screened(func(context.Context, Feed) error { return errors.New("refused") })
	src := &pictured{shows: true}
	o.Play(src)
	t.Cleanup(func() { o.Stop(src) })

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		o.mu.Lock()
		cur := o.cur
		o.mu.Unlock()
		if cur == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("the sound is still playing after its picture failed")
}

func TestWaitingForFramesHoldsTheSound(t *testing.T) {
	got := make(chan Feed, 1)
	o := screened(func(ctx context.Context, f Feed) error {
		f.Started()
		got <- f
		<-ctx.Done()
		return nil
	})
	q := o.q.(*fakeQueue)
	src := &loud{}
	o.Play(src)
	t.Cleanup(func() { o.Stop(src) })
	f := <-got
	filled(t, q)

	f.Waiting(true)
	time.Sleep(200 * time.Millisecond)
	if n := q.Queued(); n != 0 {
		t.Errorf("%d frames queued for the speaker while the picture waits", n)
	}
	f.Waiting(false)
	if q.Queued() == 0 {
		t.Error("the sound did not come back with the picture")
	}
}

type loud struct{ pictured }

func (l *loud) Read(ctx context.Context, pcm []int16) (int, error) { return len(pcm), nil }
func (l *loud) Pictured() bool                                     { return true }

func TestASourceWithoutPicturesPlaysNone(t *testing.T) {
	played := make(chan struct{}, 1)
	o := screened(func(context.Context, Feed) error {
		played <- struct{}{}
		return nil
	})
	src := &pictured{}
	o.Play(src)
	t.Cleanup(func() { o.Stop(src) })
	select {
	case <-played:
		t.Error("a source with no picture stream played one")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestWithoutAScreenAPicturedSourceIsHeardAtOnce(t *testing.T) {
	o := newOutput()
	o.q = &fakeQueue{}
	src := &pictured{shows: true}
	o.Play(src)
	t.Cleanup(func() { o.Stop(src) })
	deadline := time.Now().Add(2 * time.Second)
	for src.readCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if src.readCount() == 0 {
		t.Error("the sound waited for a picture nothing will show")
	}
}
