package playout

import (
	"bytes"
	"context"
	"errors"
	"hash/maphash"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/audio/analysis"
	"github.com/ygelfand/libcountertop/pkg/audio/ducking"
	"github.com/ygelfand/libcountertop/pkg/fetch"
	"github.com/ygelfand/libcountertop/pkg/media/nowplaying"
	"github.com/ygelfand/libcountertop/pkg/media/pcm"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
)

const Queued = time.Minute

var _ playback.Output = (*Output)(nil)
var _ pcm.Producer = (*Output)(nil)

type Output struct {
	control sync.Mutex
	options Options
	q       Queue

	handing bool
	handed  *card

	feed sync.Mutex

	mu       sync.Mutex
	ended    func()
	cur      *playing
	lastSrc  playback.Source
	lastAt   time.Duration
	gain     float32
	down     bool
	sess     *playback.Session
	note     string
	sessArt  picture
	backdrop *image.RGBA
	standby  *standby
}

type picture struct {
	raw []byte
	id  uint64
}

var pictureSeed = maphash.MakeSeed()

func pictureOf(raw []byte) picture {
	if len(raw) == 0 {
		return picture{}
	}
	return picture{raw: raw, id: maphash.Bytes(pictureSeed, raw)}
}

func (p picture) decodes() bool {
	c, _, err := image.DecodeConfig(bytes.NewReader(p.raw))
	return err == nil && c.Width > 0 && c.Height > 0
}

type playing struct {
	workers sync.WaitGroup
	src     playback.Source
	card    *card
	stop    context.CancelFunc

	kept    []int16
	paused  bool
	waiting bool
	fed     int64

	art     picture
	artURL  string
	mark    picture
	markURL string
	thumbs  map[string]picture

	seen     chan struct{}
	seenOnce sync.Once
}

func (p *playing) shown() { p.seenOnce.Do(func() { close(p.seen) }) }

func New(options Options) *Output {
	if options.Rate == 0 {
		options.Rate = 48000
	}
	if options.Channels == 0 {
		options.Channels = 2
	}
	if options.DuckDB == nil {
		options.DuckDB = func() float64 { return ducking.DefaultDB }
	}
	o := &Output{options: options, q: options.Queue, gain: 1}
	o.standby = &standby{o: o}
	return o
}

func (o *Output) Play(src playback.Source) {
	o.control.Lock()
	defer o.control.Unlock()
	o.releaseLocked(nil)

	ctx, stop := context.WithCancel(context.Background())
	p := &playing{src: src, stop: stop}
	p.card = &card{o: o, p: p}
	screen := o.options.Screen
	pics, pictured := src.(playback.Pictures)
	pictured = pictured && pics.Pictured() && screen != nil
	if pictured {
		p.seen = make(chan struct{})
	}

	art := src.Showing().Art
	o.mu.Lock()
	o.cur, o.note = p, ""
	if h := o.handed; h != nil && art != "" && h.p.artURL == art {
		p.art, p.artURL = h.p.art, art
	}
	o.mu.Unlock()
	replaced := screen != nil && !pictured && screen.Held()
	if screen != nil {
		screen.Say("")
		if !pictured {
			screen.Leave()
		}
	}

	o.q.Drain()
	o.options.Media().Began(p.card)
	if replaced {
		o.options.Media().Present(p.card)
	}
	o.mu.Lock()
	o.handed = nil
	o.mu.Unlock()
	o.options.Took(o)
	o.Changed(src)

	if pictured {
		screen.Hold(p.card)
		if b, ok := src.(playback.Branded); ok && b.Logo() != "" {
			url := b.Logo()
			safe.Go("playout logo", func() {
				if pic := o.logoFor(url); pic.raw != nil {
					screen.Logo(pic.raw, pic.id)
				}
			})
		}
	}
	o.worker(p, "playout sound", func() { o.pump(ctx, p) })
	if pictured {
		o.worker(p, "playout video", func() { o.picture(ctx, p, pics) })
	}
}

func (o *Output) picture(ctx context.Context, p *playing, pics playback.Pictures) {
	waiter, _ := p.src.(playback.Waiter)
	err := o.options.Screen.Play(ctx, p.card, pics, Feed{
		Clock: func() (time.Duration, bool) { return o.clock(p) },
		Started: func() {
			p.shown()
			if waiter != nil {
				waiter.Waiting(false)
			}
		},
		Waiting: func(w bool) {
			o.waiting(p, w)
			if waiter != nil {
				waiter.Waiting(w)
			}
		},
		Handing: func() bool {
			o.mu.Lock()
			defer o.mu.Unlock()
			return o.handing
		},
	})
	if err == nil || ctx.Err() != nil {
		return
	}
	slog.Warn("the playout video stopped", "err", err)
	o.completed(p, err, true)
}

func (o *Output) Stop(src playback.Source) {
	o.control.Lock()
	defer o.control.Unlock()
	o.mu.Lock()
	o.handing = false
	o.mu.Unlock()
	released := o.releaseLocked(src)
	o.mu.Lock()
	c := o.handed
	o.handed = nil
	attended := c != nil && o.sess != nil && o.cur == nil
	over := released && o.sess == nil && o.cur == nil
	o.mu.Unlock()
	switch {
	case attended:
		o.standBy()
	case c != nil:
		o.options.Media().Release(c)
	}
	if screen := o.options.Screen; screen != nil && over {
		screen.Leave()
	}
}

func (o *Output) Handoff(src playback.Source) {
	o.control.Lock()
	defer o.control.Unlock()
	o.mu.Lock()
	o.handing = true
	o.mu.Unlock()
	o.releaseLocked(src)
}

func (o *Output) releaseLocked(src playback.Source) bool {
	o.mu.Lock()
	p := o.cur
	if p == nil || (src != nil && p.src != src) {
		o.mu.Unlock()
		return false
	}
	o.cur = nil
	attended := src != nil && o.sess != nil && !o.handing
	handing := o.handing
	if handing {
		o.handed = p.card
	}
	o.mu.Unlock()

	p.stop()
	p.workers.Wait()
	o.options.Gave(o)
	switch {
	case attended:
		o.standBy()
	case !handing:
		o.options.Media().Release(p.card)
	}
	o.q.Drain()
	return true
}

func (o *Output) dropHanded() {
	o.mu.Lock()
	c := o.handed
	o.handed = nil
	o.mu.Unlock()
	if c != nil {
		o.options.Media().Release(c)
	}
}

func (o *Output) Attend(s *playback.Session) {
	o.mu.Lock()
	if s != o.sess {
		o.backdrop = nil
	}
	o.sess = s
	if s == nil {
		o.note, o.sessArt = "", picture{}
		o.handing = false
	}
	idle := o.cur == nil
	o.mu.Unlock()
	if s == nil {
		o.dropHanded()
		o.options.Media().Ended(o.standby)
	}
	switch {
	case !idle:
	case s != nil:
		o.standBy()
		o.options.Media().Began(o.standby)
	default:
		o.options.Media().Release(o.standby)
		if screen := o.options.Screen; screen != nil {
			screen.Leave()
		}
	}
}

func (o *Output) Failed(err error) { o.fail(err, false) }

func (o *Output) end() {
	if o.ended != nil {
		safe.Go("playout end", o.ended)
	}
}

func (o *Output) fail(err error, unseen bool) {
	o.dropHanded()
	o.mu.Lock()
	o.note = err.Error()
	idle := o.cur == nil
	o.mu.Unlock()
	if screen := o.options.Screen; screen != nil && (idle || unseen) {
		screen.Fail(err.Error())
	}
	o.options.Media().Changed()
}

func (o *Output) Backdrop(img *image.RGBA) {
	o.mu.Lock()
	if o.sess == nil || !o.sess.Pictured {
		o.mu.Unlock()
		return
	}
	o.backdrop = img
	idle := o.cur == nil && o.note == ""
	o.mu.Unlock()
	if screen := o.options.Screen; screen != nil && idle {
		screen.Backdrop(img)
	}
}

func (o *Output) standBy() {
	o.mu.Lock()
	s := o.sess
	o.mu.Unlock()
	if s == nil {
		return
	}
	o.options.Media().External(o.standby)
	o.standby.Open()
	if s.Logo == "" {
		return
	}
	safe.Go("playout logo", func() {
		pic := o.logoFor(s.Logo)
		if pic.raw == nil {
			return
		}
		o.mu.Lock()
		if o.sess == s {
			o.sessArt = pic
		}
		idle := o.cur == nil
		o.mu.Unlock()
		if screen := o.options.Screen; screen != nil && idle {
			screen.Logo(pic.raw, pic.id)
		}
		o.options.Media().Changed()
	})
}

func (o *Output) Changed(src playback.Source) {
	o.mu.Lock()
	p := o.cur
	o.mu.Unlock()
	if p == nil || p.src != src {
		return
	}

	shown := src.Showing()

	o.mu.Lock()
	fetchArt := shown.Art != "" && shown.Art != p.artURL
	if fetchArt {
		p.artURL = shown.Art
	}
	fetchMark := shown.Mark != "" && shown.Mark != p.markURL
	if fetchMark {
		p.markURL = shown.Mark
	}
	o.mu.Unlock()

	if fetchArt {
		safe.Go("playout art", func() { o.fetchArt(p, shown.Art) })
	}
	if fetchMark {
		safe.Go("playout mark", func() {
			pic := o.logoFor(shown.Mark)
			o.mu.Lock()
			if pic.raw != nil && o.cur == p && p.markURL == shown.Mark {
				p.mark = pic
			}
			o.mu.Unlock()
			o.options.Media().Changed()
		})
	}
	o.options.Media().Changed()
}

func (o *Output) Pause(src playback.Source) {
	o.feed.Lock()
	o.mu.Lock()
	p := o.cur
	if p == nil || p.src != src || p.paused {
		o.mu.Unlock()
		o.feed.Unlock()
		return
	}
	p.paused = true
	o.mu.Unlock()
	o.aside()
	o.feed.Unlock()
	o.options.Media().Changed()
}

func (o *Output) Resume(src playback.Source) {
	o.feed.Lock()
	o.mu.Lock()
	p := o.cur
	if p == nil || p.src != src || !p.paused {
		o.mu.Unlock()
		o.feed.Unlock()
		return
	}
	p.paused = false
	down, waiting := o.down, p.waiting
	o.mu.Unlock()
	if !down && !waiting {
		o.resume()
	}
	o.feed.Unlock()
	if down {
		o.options.Took(o)
	}
	o.options.Media().Changed()
}

func (o *Output) waiting(p *playing, w bool) {
	o.feed.Lock()
	defer o.feed.Unlock()
	o.mu.Lock()
	if o.cur != p || p.waiting == w {
		o.mu.Unlock()
		return
	}
	p.waiting = w
	hold := o.down || p.paused
	o.mu.Unlock()
	switch {
	case w:
		o.aside()
	case !hold:
		o.resume()
	}
}

func (o *Output) clock(p *playing) (time.Duration, bool) {
	at, paused := o.Position(p.src)
	o.mu.Lock()
	waiting := p.waiting
	o.mu.Unlock()
	return at, !paused && !waiting
}

func (o *Output) Position(src playback.Source) (time.Duration, bool) {
	o.mu.Lock()
	p := o.cur
	if p == nil || p.src != src {
		defer o.mu.Unlock()
		if src == o.lastSrc {
			return o.lastAt, false
		}
		return src.Start(), false
	}
	fed, kept, paused := p.fed, int64(len(p.kept)/o.options.Channels), p.paused
	o.mu.Unlock()
	heard := max(fed-kept-int64(o.q.Queued()), 0)
	at := src.Start() + time.Duration(heard)*time.Second/time.Duration(o.options.Rate)
	o.mu.Lock()
	o.lastSrc, o.lastAt = src, at
	o.mu.Unlock()
	return at, paused
}

func (o *Output) Behind(src playback.Source) time.Duration {
	o.mu.Lock()
	p := o.cur
	var kept int
	if p != nil {
		kept = len(p.kept) / o.options.Channels
	}
	o.mu.Unlock()
	if p == nil || p.src != src {
		return 0
	}
	frames := o.q.Queued() + kept
	return time.Duration(frames) * time.Second / time.Duration(o.options.Rate)
}

func (o *Output) pump(ctx context.Context, p *playing) {
	buf := make([]int16, o.options.Rate/50*o.options.Channels)
	most := Queued
	if r, ok := p.src.(playback.ReadAhead); ok {
		most = r.ReadAhead()
	}
	ahead := int(most * time.Duration(o.options.Rate) / time.Second)

	if p.seen != nil {
		select {
		case <-p.seen:
		case <-ctx.Done():
			return
		}
	}
	for ctx.Err() == nil {
		if o.standing() || o.q.Queued() > ahead {
			wait(ctx, 100*time.Millisecond)
			continue
		}

		n, err := p.src.Read(ctx, buf)
		if n > 0 {
			out := make([]int16, n)
			copy(out, buf[:n])
			analysis.Scale(out, o.level())
			o.feed.Lock()
			o.mu.Lock()
			p.fed += int64(n / o.options.Channels)
			held := o.cur != p || o.down || p.paused || p.waiting
			if held {
				p.kept = append(p.kept, out...)
			}
			o.mu.Unlock()
			if !held {
				o.q.Play(out)
			}
			o.feed.Unlock()
		}
		if err != nil && ctx.Err() != nil {
			return
		}
		if errors.Is(err, io.EOF) {
			o.finish(ctx, p)
			return
		}
		if err != nil {
			slog.Warn("the playout sound stopped", "err", err)
			o.completed(p, err, false)
			return
		}
		if n == 0 {
			wait(ctx, 20*time.Millisecond)
		}
	}
}

func (o *Output) finish(ctx context.Context, p *playing) {
	for ctx.Err() == nil && o.Behind(p.src) > 0 {
		wait(ctx, 100*time.Millisecond)
	}
	o.completed(p, nil, false)
}

func wait(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func (o *Output) standing() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.down || (o.cur != nil && (o.cur.paused || o.cur.waiting))
}

func (o *Output) level() float32 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.gain
}

func (o *Output) aside() {
	queued := o.q.Take()
	o.mu.Lock()
	if o.cur != nil {
		o.cur.kept = append(o.cur.kept, queued...)
	}
	o.mu.Unlock()
}

func (o *Output) resume() {
	o.mu.Lock()
	var kept []int16
	if o.cur != nil {
		kept, o.cur.kept = o.cur.kept, nil
	}
	o.mu.Unlock()
	if len(kept) > 0 {
		o.q.Play(kept)
	}
}

func (o *Output) Stand(down bool) {
	o.feed.Lock()
	defer o.feed.Unlock()
	o.mu.Lock()
	first := down && !o.down
	back := !down && o.down && (o.cur == nil || (!o.cur.paused && !o.cur.waiting))
	o.down = down
	o.mu.Unlock()

	switch {
	case first:
		o.aside()
	case back:
		o.resume()
	}
}

func (o *Output) Duck(on bool) {
	gain := float32(1)
	if on {
		gain = ducking.Gain(o.options.DuckDB())
	}
	o.mu.Lock()
	o.gain = gain
	o.mu.Unlock()
}

func (o *Output) Requeue() {
	gain := o.level()
	if gain == 1 {
		return
	}
	o.q.Adjust(func(samples []int16) { analysis.Scale(samples, gain) })
}

var (
	logoMu sync.Mutex
	logos  = map[string]picture{}
)

func (o *Output) logoFor(url string) picture {
	logoMu.Lock()
	pic, ok := logos[url]
	logoMu.Unlock()
	if ok {
		return pic
	}
	pic = fetchImage(url, "playout logo")
	if pic.raw != nil {
		logoMu.Lock()
		logos[url] = pic
		logoMu.Unlock()
	}
	return pic
}

func fetchImage(url, what string) picture {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return picture{}
	}
	resp, err := fetch.Client(15 * time.Second).Do(req)
	if err != nil {
		slog.Debug(what, "err", err)
		return picture{}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return picture{}
	}
	pic := pictureOf(body)
	if !pic.decodes() {
		return picture{}
	}
	return pic
}

func (o *Output) fetchThumb(p *playing, url string) {
	pic := fetchImage(url, "playout queue art")
	if pic.raw == nil {
		return
	}
	o.mu.Lock()
	if o.cur == p {
		p.thumbs[url] = pic
	}
	o.mu.Unlock()
	o.options.Media().Changed()
}

func (o *Output) fetchArt(p *playing, url string) {
	pic := fetchImage(url, "playout art")
	if pic.raw == nil {
		return
	}

	o.mu.Lock()
	if o.cur == p && p.artURL == url {
		p.art = pic
	}
	o.mu.Unlock()
	o.options.Media().Changed()
}

type card struct {
	o *Output
	p *playing
}

var _ nowplaying.Source = (*card)(nil)

func (c *card) Now() nowplaying.Now {
	s := c.p.src.Showing()
	elapsed, paused := c.o.Position(c.p.src)
	if pl, ok := c.p.src.(playback.Placer); ok {
		if at, placed := pl.Place(); placed {
			elapsed = at
		}
	}

	c.o.mu.Lock()
	art, mark := c.p.art, c.p.mark
	c.o.mu.Unlock()

	can := nowplaying.CanStop
	if !s.NoPause {
		can |= nowplaying.CanPause
	}
	if s.HasNext {
		can |= nowplaying.CanNext
	}
	if s.HasPrevious {
		can |= nowplaying.CanPrevious
	}
	queue := make([]nowplaying.Track, 0, len(s.Upcoming))
	var wanted []string
	c.o.mu.Lock()
	if c.p.thumbs == nil {
		c.p.thumbs = map[string]picture{}
	}
	for _, u := range s.Upcoming {
		pic, seen := c.p.thumbs[u.Art]
		if u.Art != "" && !seen {
			c.p.thumbs[u.Art] = picture{}
			wanted = append(wanted, u.Art)
		}
		queue = append(
			queue,
			nowplaying.Track{
				Title:  u.Title,
				Artist: u.Artist,
				Length: u.Length,
				Art:    pic.raw,
				ArtID:  pic.id,
				Play:   u.Play,
			},
		)
	}
	c.o.mu.Unlock()
	for _, url := range wanted {
		safe.Go("playout queue art", func() { c.o.fetchThumb(c.p, url) })
	}
	return nowplaying.Now{
		Queue:    queue,
		Playing:  !paused,
		Paused:   paused,
		Title:    s.Title,
		Artist:   s.Artist,
		Album:    s.Album,
		Art:      art.raw,
		ArtID:    art.id,
		Mark:     mark.raw,
		MarkID:   mark.id,
		Elapsed:  elapsed,
		Length:   s.Length,
		Earliest: s.Earliest,
		Edge:     s.Edge,

		LiveWithin: s.LiveWithin,
		Break:      s.Break,
		Can:        can,
	}
}

func (c *card) Open() bool {
	c.o.mu.Lock()
	cur := c.o.cur
	c.o.mu.Unlock()
	screen := c.o.options.Screen
	if cur != c.p || c.p.seen == nil || screen == nil {
		return false
	}
	screen.Open(c)
	return true
}

func (c *card) Playing() (playing, paused bool) {
	_, paused = c.o.Position(c.p.src)
	return !paused, paused
}

func (c *card) Kind() nowplaying.Kind { return nowplaying.FromCast }
func (c *card) Label() string         { return c.p.src.Label() }

func (c *card) Play()  { c.p.src.Play() }
func (c *card) Pause() { c.p.src.Pause() }

func (c *card) Stop() {
	c.p.src.Stop()
	c.o.end()
}
func (c *card) Next()     { c.p.src.Next() }
func (c *card) Previous() { c.p.src.Previous() }

func (c *card) Seek(to time.Duration) {
	if s, ok := c.p.src.(playback.Seeker); ok {
		s.Seek(to)
	}
}

func (c *card) Marks() []playback.Mark {
	if m, ok := c.p.src.(playback.Marked); ok {
		return m.Marks()
	}
	return nil
}

func (c *card) CanSeek() bool {
	_, ok := c.p.src.(playback.Seeker)
	s := c.p.src.Showing()
	return ok && s.Break == nil && (s.Length > 0 || s.LiveWithin > 0)
}

type standby struct{ o *Output }

var _ nowplaying.Source = (*standby)(nil)

func (b *standby) Now() nowplaying.Now {
	b.o.mu.Lock()
	defer b.o.mu.Unlock()
	return nowplaying.Now{
		Hold:  b.o.sess != nil,
		Can:   nowplaying.CanStop,
		Title: b.o.note,
		Art:   b.o.sessArt.raw,
		ArtID: b.o.sessArt.id,
	}
}

func (b *standby) Playing() (playing, paused bool) { return false, false }

func (b *standby) Kind() nowplaying.Kind { return nowplaying.FromCast }

func (b *standby) Open() bool {
	b.o.mu.Lock()
	s, note, art, back, playing := b.o.sess, b.o.note, b.o.sessArt, b.o.backdrop, b.o.cur != nil
	b.o.mu.Unlock()
	screen := b.o.options.Screen
	if s == nil || !s.Pictured || playing || screen == nil {
		return false
	}
	failed := note != ""
	if !failed && s.Logo == "" {
		note = say.F("cast.starting", map[string]any{"Name": s.Label})
	}
	screen.Standby(b, note, art.raw, art.id)
	if back != nil && !failed {
		screen.Backdrop(back)
	}
	return true
}

func (b *standby) Wakes() bool {
	b.o.mu.Lock()
	defer b.o.mu.Unlock()
	return b.o.sess != nil && b.o.sess.Pictured
}

func (b *standby) Seek(time.Duration)     {}
func (b *standby) CanSeek() bool          { return false }
func (b *standby) Marks() []playback.Mark { return nil }

func (b *standby) Label() string {
	b.o.mu.Lock()
	defer b.o.mu.Unlock()
	if b.o.sess == nil {
		return ""
	}
	return b.o.sess.Label
}

func (b *standby) Play()     {}
func (b *standby) Pause()    {}
func (b *standby) Stop()     { b.o.end() }
func (b *standby) Next()     {}
func (b *standby) Previous() {}

type Queue interface {
	Play([]int16)
	Take() []int16
	Queued() int
	Drain()
	Adjust(func([]int16))
}
type Media interface {
	Began(nowplaying.Source)
	Present(nowplaying.Source)
	Ended(nowplaying.Source)
	Release(nowplaying.Source)
	External(nowplaying.Source)
	Changed()
}
type Options struct {
	Queue          Queue
	Rate, Channels int
	DuckDB         func() float64
	Media          func() Media
	Took, Gave     func(pcm.Producer)
	Screen         Screen
}

func (o *Output) SetEnded(f func()) { o.mu.Lock(); o.ended = f; o.mu.Unlock() }

func (o *Output) worker(p *playing, name string, run func()) {
	p.workers.Add(1)
	safe.Go(name, func() { defer p.workers.Done(); run() })
}
func (o *Output) completed(p *playing, err error, unseen bool) {
	safe.Go("playout completion", func() {
		o.control.Lock()
		defer o.control.Unlock()
		o.mu.Lock()
		current := o.cur == p
		o.mu.Unlock()
		if !current {
			return
		}
		if err != nil {
			o.fail(err, unseen)
		}
		o.releaseLocked(p.src)
		switch f, fails := p.src.(playback.Failer); {
		case err == nil:
		case fails:
			safe.Go("playout failure", func() { f.Fail(err) })
		default:
			safe.Go("playout stop", p.src.Stop)
		}
	})
}
