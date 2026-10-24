package playout

import (
	"fmt"

	"github.com/ygelfand/libcountertop/pkg/media/nowplaying"
	"github.com/ygelfand/libcountertop/pkg/media/ownership"
	"github.com/ygelfand/libcountertop/pkg/media/pcm"
)

type fakePlayer struct {
	owner     *ownership.Owner[nowplaying.Source]
	presented []nowplaying.Source
}

var testPlayer *fakePlayer

func newOutput() *Output {
	p := &fakePlayer{
		owner: ownership.New[nowplaying.Source](
			func(s nowplaying.Source) uint8 { return uint8(s.Kind()) },
		),
	}
	testPlayer = p
	return New(
		Options{
			Queue: &fakeQueue{},
			Media: func() Media { return p },
			Took:  func(pcm.Producer) {},
			Gave:  func(pcm.Producer) {},
		},
	)
}
func (p *fakePlayer) Began(s nowplaying.Source)   { p.owner.Begin(s) }
func (p *fakePlayer) Present(s nowplaying.Source) { p.presented = append(p.presented, s) }
func (p *fakePlayer) Ended(s nowplaying.Source)   { p.owner.End(s) }
func (p *fakePlayer) Release(s nowplaying.Source) { p.owner.Release(s) }
func (p *fakePlayer) External(s nowplaying.Source) {
	if s == nil {
		p.owner.Clear()
	} else {
		p.owner.Set(s)
	}
}
func (p *fakePlayer) Changed() {}
func (p *fakePlayer) Now() nowplaying.Now {
	s, _ := p.owner.Get()
	if s == nil {
		return nowplaying.Now{}
	}
	return s.Now()
}
func (p *fakePlayer) Sourced() (string, bool) {
	s, ok := p.owner.Get()
	return fmt.Sprintf("%T", s), ok
}
