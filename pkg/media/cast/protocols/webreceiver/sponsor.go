package webreceiver

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/media/sponsorblock"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
)

const (
	skipCheck   = 250 * time.Millisecond
	skipEarly   = 100 * time.Millisecond
	lengthSlack = 2 * time.Second
)

type sponsor struct {
	r *Receiver

	mu    sync.Mutex
	video string
	stop  context.CancelFunc
}

func (s *sponsor) page(address string) {
	id := videoID(address)
	s.mu.Lock()
	if id == s.video {
		s.mu.Unlock()
		return
	}
	stop := s.stop
	s.video, s.stop = id, nil
	var ctx context.Context
	if id != "" {
		ctx, s.stop = context.WithCancel(context.Background())
	}
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
	if s.r.player == nil {
		return
	}
	s.r.player.Mark(nil)
	if id != "" {
		safe.Go("cast sponsorblock", func() { s.follow(ctx, id) })
	}
}

func (s *sponsor) follow(ctx context.Context, id string) {
	skip := s.r.env.Preferences.Settings().YouTube.Skip
	segs, err := sponsorblock.Segments(ctx, s.r.env.HTTP, sponsorblock.URL, id, skip)
	if err != nil {
		slog.Warn("youtube sponsorblock", "video", id, "err", err)
		return
	}
	if len(segs) == 0 {
		return
	}
	slog.Debug("youtube sponsorblock", "video", id, "segments", len(segs))
	p := s.r.player
	p.Mark(sponsorblock.Marks(segs))
	tick := time.NewTicker(skipCheck)
	defer tick.Stop()
	skipped := time.Duration(-1)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		shown, ok := p.Shown()
		if !ok || !lengthMatches(shown.Length, segs) {
			continue
		}
		at := p.Elapsed() + skipEarly
		seg, inside := sponsorblock.Inside(segs, p.Begins(), at)
		switch {
		case inside && seg.From != skipped:
			skipped = seg.From
			slog.Info("youtube sponsorblock skip", "video", id, "from", seg.From, "to", seg.To)
			_ = remote{s.r}.Seek(seg.To)
		case !inside && at < skipped:
			skipped = -1
		}
	}
}

func lengthMatches(playing time.Duration, segs []sponsorblock.Segment) bool {
	want := segs[0].Length
	if want <= 0 {
		return playing > 0
	}
	d := playing - want
	return d > -lengthSlack && d < lengthSlack
}

func videoID(address string) string {
	u, err := url.Parse(address)
	if err != nil ||
		(u.Hostname() != "youtube.com" && !strings.HasSuffix(u.Hostname(), ".youtube.com")) {
		return ""
	}
	if v := u.Query().Get("v"); v != "" {
		return v
	}
	_, query, ok := strings.Cut(u.Fragment, "?")
	if !ok {
		return ""
	}
	q, err := url.ParseQuery(query)
	if err != nil {
		return ""
	}
	return q.Get("v")
}
