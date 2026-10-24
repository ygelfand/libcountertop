package youtube

import (
	"context"
	"log/slog"

	"github.com/ygelfand/libcountertop/pkg/media/sponsorblock"
)

func (s *station) segments(ctx context.Context, r *Resolver, id string) []sponsorblock.Segment {
	if s == nil || s.theme != ThemeYouTube {
		return nil
	}
	s.mu.Lock()
	if s.skipsFor == id {
		defer s.mu.Unlock()
		return s.skips
	}
	s.mu.Unlock()

	segs, err := sponsorblock.Segments(
		ctx,
		r.http,
		sponsorblock.URL,
		id,
		r.Preferences.Settings().YouTube.Skip,
	)
	if err != nil {
		slog.Warn("youtube sponsorblock", "video", id, "err", err)
		return nil
	}
	if len(segs) > 0 {
		slog.Debug("youtube sponsorblock", "video", id, "segments", len(segs))
	}
	s.mu.Lock()
	s.skipsFor, s.skips = id, segs
	s.mu.Unlock()
	return segs
}
