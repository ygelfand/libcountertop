package urlplayer

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ygelfand/libcountertop/pkg/media/cast"
	"github.com/ygelfand/libcountertop/pkg/media/cenc"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
)

const (
	outRate     = 48000
	outChannels = 2
	buffered    = 64 << 10
	offsetStart = 2 * time.Second
	liveSlack   = 5 * time.Second
)

type track struct {
	player   *Player
	media    cast.Media
	feed     *Feed
	at       time.Duration
	client   *http.Client
	resample func(from int) func([]int16) []int16
	screen   bool
	target   func() playback.Target
	ctx      context.Context
	cancel   context.CancelFunc

	mu       sync.Mutex
	started  bool
	failed   error
	seekable bool
	dec      decoder
	convert  func([]int16) []int16
	left     []int16
	drop     int64
	picture  playback.Picture
	length   time.Duration
	format   string
	handed   atomic.Int64
	waiting  atomic.Bool
	skipping atomic.Int64
	skipped  atomic.Int64

	frameMu   sync.Mutex
	queued    []time.Duration
	presented int64

	bodyMu sync.Mutex
	body   io.Closer
	keys   Keys
	closed bool
}

func (t *track) ensure() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ensureLocked()
}

func (t *track) ensureLocked() error {
	if !t.started {
		t.failed, t.started = t.begin(t.ctx), true
	}
	return t.failed
}

func (t *track) Read(ctx context.Context, pcm []int16) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	stop := context.AfterFunc(ctx, t.close)
	defer stop()

	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.ensureLocked(); err != nil {
		return 0, err
	}
	t.drop += t.skipping.Swap(0)
	n := 0
	for n < len(pcm) {
		if len(t.left) > 0 {
			c := copy(pcm[n:], t.left)
			t.left = t.left[c:]
			n += c
			continue
		}
		out, err := t.dec.next()
		if err != nil {
			if n > 0 && errors.Is(err, io.EOF) {
				return n, nil
			}
			return n, err
		}
		if len(out) == 0 {
			return n, nil
		}
		out = t.convert(out)
		if t.drop > 0 {
			d := min(t.drop*outChannels, int64(len(out)))
			out, t.drop = out[d:], t.drop-d/outChannels
		}
		t.left = out
	}
	return n, nil
}

func (t *track) begin(ctx context.Context) error {
	if t.feed != nil {
		return t.openFeed(ctx)
	}
	s, err := open(ctx, t.client, t.media.URL(), 0)
	if err != nil {
		return err
	}
	if !t.hold(s.body) {
		s.body.Close()
		return io.ErrClosedPipe
	}
	t.seekable = s.ranged != nil
	contentType := s.contentType
	if contentType == "" {
		contentType = t.media.ContentType
	}
	br := bufio.NewReaderSize(s.body, buffered)
	head, _ := br.Peek(16)
	format := sniff(head, contentType)
	t.format = format
	switch format {
	case HLS:
		s.body.Close()
		return t.openHLS(ctx)
	case MP4:
		return t.openMP4(ctx, s)
	}
	var tag, pace int64
	if format == MP3 {
		tag, pace = mp3Pace(br)
	}
	dec, err := newDecoder(ctx, format, br)
	if err != nil {
		return err
	}
	if t.at > 0 {
		dec, err = t.jump(format, s, br, dec, tag, pace)
		if err != nil {
			return err
		}
	}
	if m, ok := dec.(*matroska); ok && t.screen {
		if video, ok := m.demux.Paired(); ok {
			pic, err := matroskaPicture(video.Codec, video.Private, video.Width, video.Height)
			if err == nil {
				x := startMuxed(
					ctx,
					m.paired(video.Number),
					m.frame,
					m.hz,
					time.Duration(m.from),
					pic,
				)
				if first := x.begins(); !t.seekable && first > t.at+offsetStart {
					t.at = first
				}
				dec = x
				t.picture = pic.playback()
			}
		}
	}
	return t.use(dec)
}

func (t *track) use(dec decoder) error {
	t.dec = dec
	t.convert = func(p []int16) []int16 { return p }
	if r := dec.rate(); r != outRate {
		if t.resample == nil {
			return errors.New("urlplayer: no resampler for a non-48 kHz stream")
		}
		t.convert = t.resample(r)
	}
	return nil
}

func (t *track) jump(
	format string,
	s stream,
	br *bufio.Reader,
	dec decoder,
	tag, pace int64,
) (decoder, error) {
	skip := func() (decoder, error) {
		t.drop = int64(t.at.Seconds() * outRate)
		return dec, nil
	}
	if s.ranged == nil {
		return skip()
	}
	switch d := dec.(type) {
	case *wav:
		frame := int64(2 * d.channels)
		off := int64(t.at.Seconds()*float64(d.hz)) * frame
		s.ranged.From(d.data + off)
		br.Reset(s.ranged)
		d.held = 0
		return d, nil
	case *matroska:
		d.from = int64(t.at)
		if err := d.demux.Index(); err != nil {
			return skip()
		}
		if off, _, ok := d.demux.Cue(int64(t.at)); ok {
			s.ranged.From(off)
			d.demux.Restart(s.ranged, off)
		}
		return d, nil
	}
	off := int64(-1)
	switch {
	case (format == MP3 || format == AAC) && s.size > 0 && t.media.Duration > 0:
		off = int64(float64(s.size) * t.at.Seconds() / t.media.Duration)
	case format == MP3 && pace > 0:
		off = tag + int64(t.at.Seconds()*float64(pace))
	}
	if off >= 0 && (s.size <= 0 || off < s.size) {
		s.ranged.From(off)
		br.Reset(s.ranged)
		synced := format != MP3 || mp3Sync(br) == nil
		if mid, err := newDecoder(t.ctx, format, br); synced && err == nil {
			return mid, nil
		}
		s.ranged.From(0)
		br.Reset(s.ranged)
		fresh, err := newDecoder(t.ctx, format, br)
		if err != nil {
			return nil, err
		}
		dec = fresh
	}
	return skip()
}

func (t *track) hold(body io.Closer) bool {
	t.bodyMu.Lock()
	defer t.bodyMu.Unlock()
	if t.closed {
		return false
	}
	t.body = body
	return true
}

func (t *track) protect(k Keys) bool {
	t.bodyMu.Lock()
	defer t.bodyMu.Unlock()
	if t.closed {
		return false
	}
	t.keys = k
	return true
}

func (t *track) close() {
	t.cancel()
	t.bodyMu.Lock()
	defer t.bodyMu.Unlock()
	if t.closed {
		return
	}
	t.closed = true
	if t.body != nil {
		_ = t.body.Close()
	}
	if t.keys != nil {
		t.keys.Close()
	}
}

func (t *track) Pictured() bool { return t.picture.Next != nil || t.picture.Sealed != nil }

func (t *track) Waiting(w bool) {
	t.waiting.Store(w)
	t.player.ready(t)
}

func (t *track) Pictures(context.Context) (io.ReadCloser, time.Duration, error) {
	return nil, 0, nil
}

func (t *track) Live(context.Context) (playback.Picture, error) {
	if t.picture.Sealed != nil {
		p, next := t.picture, t.picture.Sealed
		p.Sealed = func(ctx context.Context) ([]byte, time.Duration, *cenc.Crypt, error) {
			data, at, crypt, err := next(ctx)
			if err == nil {
				t.handed.Add(1)
				at += t.origin()
				t.queue(at)
			}
			return data, at, crypt, err
		}
		return p, nil
	}
	if t.picture.Next == nil {
		return playback.Picture{}, errors.New("urlplayer: no picture")
	}
	p, next := t.picture, t.picture.Next
	p.Next = func(ctx context.Context) ([]byte, time.Duration, error) {
		data, at, err := next(ctx)
		if err == nil {
			t.handed.Add(1)
			t.queue(at)
		}
		return data, at, err
	}
	return p, nil
}

func (t *track) queue(at time.Duration) {
	t.frameMu.Lock()
	t.queued = append(t.queued, at)
	t.frameMu.Unlock()
}

func (t *track) shownBy(at time.Duration) int64 {
	t.frameMu.Lock()
	defer t.frameMu.Unlock()
	n := 0
	for n < len(t.queued) && t.queued[n] <= at {
		n++
	}
	t.presented += int64(n)
	t.queued = t.queued[n:]
	return t.presented
}

func (t *track) Start() time.Duration {
	return t.origin() + time.Duration(t.skipped.Load())
}

func (t *track) skip(by time.Duration) {
	t.skipped.Add(int64(by))
	t.skipping.Add(int64(by.Seconds() * outRate))
}

func (t *track) origin() time.Duration {
	t.bodyMu.Lock()
	k := t.keys
	t.bodyMu.Unlock()
	if k != nil {
		if at, ok := k.Origin(); ok {
			return at
		}
	}
	return t.at
}

func (t *track) Showing() playback.Showing {
	md := t.media.Metadata
	if d, ok := t.player.described(); ok {
		md = d
	}
	s := playback.Showing{
		Title:  md.Title,
		Artist: firstOf(md.Artist, md.AlbumArtist, md.Subtitle),
		Album:  md.AlbumName,
	}
	st, followed := t.player.followed()
	pr, _, programmed := t.player.programmed()
	switch {
	case followed && programmed:
		s.Length, s.Earliest, s.Edge = pr.Length, pr.Earliest, pr.Edge
		s.LiveWithin = liveSlack
	case followed && st.Live:
		if start, edge, ok := t.player.window(st.At); ok {
			s.Length = edge - start
		}
		s.LiveWithin = liveSlack
	case followed && st.Length > 0:
		s.Length = st.Length
	case t.feed != nil:
		s.Length = t.feed.Duration()
	case !t.media.Streaming():
		s.Length = t.media.Length()
	}
	if followed {
		s.Break = st.Break
	}
	if len(md.Images) > 0 {
		s.Art = md.Images[0].URL
	}
	return s
}

func (t *track) Place() (time.Duration, bool) {
	st, ok := t.player.followed()
	if _, offset, programmed := t.player.programmed(); programmed {
		return max(st.At-offset, 0), ok
	}
	if start, _, live := t.player.window(st.At); live {
		return max(st.At-start, 0), ok
	}
	return st.At, ok
}

func (t *track) Label() string { return "" }

func (t *track) Marks() []playback.Mark { return t.player.marked() }

func (t *track) Play() {
	if r := t.player.o.Remote; r != nil {
		_ = r.Play()
		return
	}
	_ = t.player.resume(t)
}

func (t *track) Pause() {
	if r := t.player.o.Remote; r != nil {
		_ = r.Pause()
		return
	}
	_ = t.player.pause(t)
}

func (t *track) Stop() {
	if r := t.player.o.Remote; r != nil {
		_ = r.Stop()
		return
	}
	t.player.stop(t, t.player.o.Output.Stop)
}

func (t *track) Fail(err error) {
	if !t.player.stop(t, t.player.o.Output.Stop) {
		return
	}
	if r := t.player.o.Remote; r != nil {
		r.Failed(err)
	}
}

func (t *track) Next()     {}
func (t *track) Previous() {}

func (t *track) Seek(to time.Duration) {
	if r := t.player.o.Remote; r != nil {
		if pr, offset, programmed := t.player.programmed(); programmed {
			to = min(max(to, pr.Earliest), pr.Edge) + offset
		} else if start, _, live := t.player.window(0); live {
			to += start
		}
		_ = r.Seek(to)
		return
	}
	safe.Go("urlplayer seek", func() { _ = t.player.seek(t, to) })
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
