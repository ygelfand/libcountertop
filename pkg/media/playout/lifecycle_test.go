package playout

import (
	"context"
	"testing"
	"testing/synctest"
)

type exiting struct {
	ending
	reading, cancelled, finish chan struct{}
}

func (s *exiting) Read(ctx context.Context, _ []int16) (int, error) {
	close(s.reading)
	<-ctx.Done()
	close(s.cancelled)
	<-s.finish
	return 0, ctx.Err()
}

func TestStopJoinsTheAudioWorker(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		o := newOutput()
		s := &exiting{
			reading:   make(chan struct{}),
			cancelled: make(chan struct{}),
			finish:    make(chan struct{}),
		}
		o.Play(s)
		<-s.reading
		stopped := make(chan struct{})
		go func() { o.Stop(s); close(stopped) }()
		<-s.cancelled
		synctest.Wait()
		select {
		case <-stopped:
			t.Fatal("Stop returned while audio was still exiting")
		default:
		}
		close(s.finish)
		<-stopped
	})
}
