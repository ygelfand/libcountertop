package playout

import (
	"testing"

	"github.com/ygelfand/libcountertop/pkg/audio/background"
	"github.com/ygelfand/libcountertop/pkg/media/pcm"
)

type noise struct{ down bool }

func (n *noise) Stand(down bool) { n.down = down }
func (n *noise) Duck(bool)       {}
func (n *noise) Requeue()        {}

func TestResumingTakesTheSpeakerBackFromWhatTookItSince(t *testing.T) {
	var a background.Arbiter
	o := newOutput()
	o.q = &fakeQueue{}
	o.options.Took = func(p pcm.Producer) { a.Took(p) }
	o.options.Gave = func(p pcm.Producer) { a.Gave(p) }

	src := &endless{}
	o.Play(src)
	t.Cleanup(func() { o.Stop(src) })

	n := &noise{}
	a.Took(n)
	if a.Playing() != n || !o.standing() {
		t.Fatal("the noise did not take the speaker from the cast")
	}

	o.Pause(src)
	o.Resume(src)
	if a.Playing() != pcm.Producer(o) {
		t.Fatalf("resuming left %T on the speaker", a.Playing())
	}
	if !n.down || o.standing() {
		t.Error("resuming did not stand the noise down and the cast up")
	}
}
