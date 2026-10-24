package youtube

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/opus"

	"github.com/ygelfand/libcountertop/pkg/media/hls"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
	"github.com/ygelfand/libcountertop/pkg/media/sponsorblock"
	"github.com/ygelfand/libcountertop/pkg/media/webm"
)

const (
	rate     = 48000
	channels = 2
)

// One Opus packet carries at most 120 ms per channel: 5760 samples at 48 kHz.
const maxFrame = 5760

type track struct {
	info   Track
	stream io.ReadCloser
	demux  *webm.Reader
	dec    opus.Decoder

	station *station
	stalled atomic.Bool

	mu       sync.Mutex
	ended    bool
	start    time.Duration
	produced int64
	skip     int
	left     []int16
	frame    []int16

	skips   []sponsorblock.Segment
	jumping bool

	live *hls.Stream
	base time.Time
}

var errJumping = errors.New("youtube: jumping past a segment")

var (
	_ playback.Source = (*track)(nil)
	_ playback.Seeker = (*track)(nil)
	_ playback.Marked = (*track)(nil)
)

func openTrack(
	ctx context.Context,
	r *Resolver,
	st *station,
	id string,
	at time.Duration,
) (*track, error) {
	skips := make(chan []sponsorblock.Segment, 1)
	go func() { skips <- st.segments(ctx, r, id) }()

	info, stream, err := r.Audio(ctx, id)
	if l, ok := errors.AsType[*Live](err); ok {
		return openLive(ctx, r, st, l, at)
	}
	if err != nil {
		return nil, err
	}
	t, err := newTrack(info, stream, st, at)
	if err != nil {
		stream.Close()
		return nil, err
	}
	t.skips = <-skips
	return t, nil
}

func openLive(
	ctx context.Context,
	r *Resolver,
	st *station,
	l *Live,
	at time.Duration,
) (*track, error) {
	var from time.Time
	if WallClock(at) {
		from = time.Unix(0, int64(at))
	}
	s, err := hls.Open(
		ctx,
		l.HLS,
		hls.Options{
			HTTP:     r.http,
			Ask:      ask,
			Lead:     liveLead,
			From:     from,
			Resample: r.Resample,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("youtube: %s: %w", l.Track.ID, err)
	}
	t := &track{info: l.Track, station: st, live: s}
	if oldest, _, ok := s.Window(); ok {
		t.base = oldest
	}
	slog.Debug(
		"youtube live",
		"video",
		l.Track.ID,
		"from",
		from,
		"base",
		t.base,
		"size",
		fmt.Sprintf("%dx%d", s.Width, s.Height),
	)
	return t, nil
}

const (
	wallFloor   = 20 * 365 * 24 * time.Hour
	rewindLeast = time.Minute
	liveSlack   = 15 * time.Second
	liveLead    = 10 * time.Second
)

func WallClock(at time.Duration) bool { return at > wallFloor }

func (t *track) window() (oldest, edge time.Time, ok bool) {
	if t.live == nil {
		return time.Time{}, time.Time{}, false
	}
	return t.live.Window()
}

func newTrack(info Track, stream io.ReadCloser, st *station, at time.Duration) (*track, error) {
	demux := webm.NewReader(stream)
	tr, err := demux.Track()
	if err != nil {
		return nil, fmt.Errorf("youtube: %s: %w", info.ID, err)
	}
	if tr.Codec != "A_OPUS" {
		return nil, fmt.Errorf("youtube: %s is %s, not opus", info.ID, tr.Codec)
	}
	if s, ok := stream.(interface{ From(int64) }); ok && at > 0 {
		if err := demux.Index(); err != nil {
			return nil, fmt.Errorf("youtube: %s: %w", info.ID, err)
		}
		off, cue, ok := demux.Cue(int64(at))
		slog.Debug(
			"youtube audio cue",
			"video",
			info.ID,
			"want",
			at,
			"cue",
			time.Duration(cue),
			"at",
			off,
			"found",
			ok,
		)
		if ok {
			s.From(off)
			demux.Restart(stream, off)
		}
	}
	dec, err := opus.NewDecoderWithOutput(rate, channels)
	if err != nil {
		return nil, err
	}

	t := &track{
		info: info, stream: stream, demux: demux, dec: dec, station: st,
		start: at, frame: make([]int16, maxFrame*channels),
	}
	if at == 0 && len(tr.Private) >= 12 {
		t.skip = int(binary.LittleEndian.Uint16(tr.Private[10:12])) * channels
	}
	return t, nil
}

func (t *track) Read(ctx context.Context, pcm []int16) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if t.stream != nil {
		done := make(chan struct{})
		stop := context.AfterFunc(ctx, func() { t.stream.Close(); close(done) })
		defer func() {
			if !stop() {
				<-done
			}
		}()
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.jumping {
		return 0, nil
	}
	if t.live != nil {
		return t.readLive(t.live.Read, pcm)
	}

	n := 0
	for n < len(pcm) {
		if len(t.left) == 0 {
			if err := t.decode(); err != nil {
				if errors.Is(err, errJumping) {
					break
				}
				if errors.Is(err, io.EOF) && !t.ended {
					t.ended = true
					if t.station != nil {
						go t.station.finished(t)
					}
				}
				return n, err
			}
			continue
		}
		c := copy(pcm[n:], t.left)
		t.left = t.left[c:]
		n += c
	}
	t.produced += int64(n / channels)
	return n, nil
}

func (t *track) readLive(read func([]int16) (int, error), pcm []int16) (int, error) {
	n, err := read(pcm)
	t.produced += int64(n / channels)
	if errors.Is(err, io.EOF) && !t.ended {
		t.ended = true
		if t.station != nil {
			go t.station.finished(t)
		}
	}
	return n, err
}

func (t *track) decode() error {
	for {
		f, err := t.demux.Next()
		if err != nil {
			return err
		}
		at := time.Duration(f.Time)
		if at < t.start {
			continue
		}
		if s, ok := sponsorblock.Inside(t.skips, t.start, at); ok {
			t.jumping = true
			if t.station != nil {
				go t.station.skipWhenHeard(t, s)
			}
			return errJumping
		}
		got, err := t.dec.DecodeToInt16(f.Data, t.frame)
		if err != nil {
			continue
		}
		samples := t.frame[:got*channels]
		if t.skip > 0 {
			drop := min(t.skip, len(samples))
			samples, t.skip = samples[drop:], t.skip-drop
		}
		if len(samples) > 0 {
			t.left = samples
			return nil
		}
	}
}

func (t *track) elapsed() time.Duration {
	start := t.Start()
	t.mu.Lock()
	defer t.mu.Unlock()
	return start + time.Duration(t.produced)*time.Second/rate
}

func (t *track) Logo() string {
	if t.station == nil {
		return ""
	}
	return t.station.logo()
}

func (t *track) Waiting(w bool) {
	if t.station == nil {
		return
	}
	t.stalled.Store(w)
	go t.tell()
}

func (t *track) tell() {
	if t.station.current() != t {
		return
	}
	if t.stalled.Load() {
		t.station.report(stateLoading)
		return
	}
	t.station.report(t.station.state())
}

func (t *track) Marks() []playback.Mark { return sponsorblock.Marks(t.skips) }

func (t *track) Seek(to time.Duration) {
	if t.station == nil {
		return
	}
	if t.live == nil {
		go t.station.seek(max(to, 0))
		return
	}
	if !t.base.IsZero() {
		go t.station.seek(time.Duration(t.base.Add(max(to, 0)).UnixNano()))
	}
}

func (t *track) Start() time.Duration {
	if t.live == nil || t.base.IsZero() {
		return t.start
	}
	began := t.live.Began()
	if began.IsZero() {
		return 0
	}
	return max(began.Sub(t.base), 0)
}

func (t *track) close() {
	if t.live != nil {
		t.live.Close()
		return
	}
	t.stream.Close()
}

func (t *track) Showing() playback.Showing {
	s := playback.Showing{
		Title:  t.info.Title,
		Artist: t.info.Author,
		Art:    t.info.Thumbnail,
		Length: t.info.Duration,
	}
	if oldest, edge, ok := t.window(); ok && edge.Sub(oldest) >= rewindLeast && !t.base.IsZero() {
		s.Length = edge.Sub(t.base)
		s.LiveWithin = liveLead + liveSlack
	}
	if t.station != nil {
		s.HasPrevious, s.HasNext = t.station.around()
		s.Upcoming = t.station.coming()
	}
	return s
}

func (t *track) Label() string {
	if t.station == nil {
		return ""
	}
	return t.station.label()
}

func (t *track) Play()     { t.station.play() }
func (t *track) Pause()    { t.station.pause() }
func (t *track) Stop()     { t.station.stop() }
func (t *track) Next()     { t.station.step(1) }
func (t *track) Previous() { t.station.step(-1) }
