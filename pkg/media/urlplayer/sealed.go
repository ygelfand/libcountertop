package urlplayer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/ygelfand/libcountertop/pkg/media/cenc"
)

const sealedRead = 4096

var errNoKeys = errors.New("urlplayer: a protected media source with no keys")

func (t *track) openSealed(ctx context.Context, soundLane, videoLane int) error {
	open := t.feed.opener()
	if open == nil {
		return errNoKeys
	}
	sound := t.feed.lane(soundLane)
	if sound.cenc == nil {
		return fmt.Errorf("%w: protected video beside sound it cannot decrypt", errUnplayable)
	}
	k, err := open(ctx)
	if err != nil {
		return err
	}
	if !t.protect(k) {
		k.Close()
		return context.Canceled
	}
	if err := k.OpenAudio(*sound.cenc); err != nil {
		return err
	}
	k.Begin(t.at)
	var lanes sync.WaitGroup
	if v := t.feed.lane(videoLane); videoLane >= 0 && v.cenc != nil && v.video && t.screen {
		t.picture = k.Video(v.width, v.height)
		lanes.Go(func() {
			t.follow(ctx, k, videoLane, *v.cenc, func(tr cenc.Track, s cenc.Sample) error {
				return k.Picture(ctx, tr, s)
			})
		})
	}
	lanes.Go(func() {
		t.follow(ctx, k, soundLane, *sound.cenc, k.Sound)
	})
	go func() {
		lanes.Wait()
		k.Fail(io.EOF)
	}()
	t.seekable = true
	t.length = t.feed.Duration()
	t.dec = &sealedSound{k: k, buf: make([]int16, sealedRead*outChannels)}
	t.convert = func(p []int16) []int16 { return p }
	return nil
}

func (t *track) follow(
	ctx context.Context,
	k Keys,
	id int,
	opened cenc.Track,
	take func(cenc.Track, cenc.Sample) error,
) {
	next := t.feed.reader(ctx, t.at, id)
	ready := map[[16]byte]bool{}
	for {
		s, err := next()
		if err == nil {
			cs, tr := cenc.Sample{Data: s.data, At: s.at, Key: s.key}, opened
			if s.sealed != nil {
				cs = *s.sealed
			}
			if s.track != nil {
				tr = *s.track
			}
			if cs.Encrypted && !ready[cs.KeyID] {
				err = k.Wait(ctx, cs.KeyID)
				ready[cs.KeyID] = err == nil
			}
			if err == nil {
				err = take(tr, cs)
			}
		}
		if err != nil {
			slog.Debug("the url player's protected feed stopped", "lane", id, "err", err)
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				k.Fail(err)
			}
			return
		}
	}
}

type sealedSound struct {
	k   Keys
	buf []int16
}

func (s *sealedSound) rate() int { return outRate }

func (s *sealedSound) next() ([]int16, error) {
	n, err := s.k.Read(s.buf)
	if n > 0 {
		return append([]int16(nil), s.buf[:n]...), nil
	}
	return nil, err
}
