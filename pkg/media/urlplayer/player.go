package urlplayer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/fetch"
	"github.com/ygelfand/libcountertop/pkg/media/cast"
	"github.com/ygelfand/libcountertop/pkg/media/nowplaying"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
)

var errIdle = errors.New("urlplayer: nothing is loaded")

const breakRGB = 0xffcc00

type Options struct {
	HTTP     *http.Client
	Output   playback.Output
	Resample func(from int) func(stereo []int16) []int16
	Screen   bool
	Target   func() playback.Target
	Remote   Remote
	Ready    func()
}

type Remote interface {
	cast.Transport
	Seek(to time.Duration) error
	Failed(err error)
}

type Player struct {
	o Options

	mu       sync.Mutex
	cur      *track
	shown    *track
	paused   bool
	metadata *cast.Metadata
	marks    []playback.Mark
	counted  int64
	status   *followed
	live     *window
	program  *programmed
}

type Status struct {
	At      time.Duration
	Playing bool
	Length  time.Duration
	Live    bool
	Break   *nowplaying.Break
	Breaks  []time.Duration
}

type followed struct {
	Status
	seen time.Time
}

type window struct {
	start, furthest time.Duration
	reached         time.Time
}

type Program struct {
	At       time.Duration
	Length   time.Duration
	Earliest time.Duration
	Edge     time.Duration
}

type programmed struct {
	Program
	offset time.Duration
	seen   time.Time
}

func New(o Options) *Player {
	if o.HTTP == nil {
		o.HTTP = fetch.Client(0)
	}
	return &Player{o: o}
}

func (p *Player) Load(m cast.Media, at time.Duration, play bool) error {
	u, err := url.Parse(m.URL())
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("urlplayer: %q is not an http URL", m.URL())
	}
	if p.o.Output == nil {
		return errors.New("urlplayer: no output")
	}
	p.fresh()
	p.swap(p.track(m, nil, at), !play)
	return nil
}

func (p *Player) LoadFeed(f *Feed, at time.Duration, play bool) error {
	if p.o.Output == nil {
		return errors.New("urlplayer: no output")
	}
	p.fresh()
	p.swap(p.track(cast.Media{}, f, at), !play)
	return nil
}

func (p *Player) fresh() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.counted = 0
	p.live = nil
	p.program = nil
	slog.Debug("the url player forgot the live window")
}

func (p *Player) Follow(s *Status) {
	p.mu.Lock()
	p.status = nil
	now := time.Now()
	switch {
	case s == nil || !s.Live:
		p.live = nil
		p.program = nil
	case p.live == nil:
		p.live = &window{start: s.At, furthest: s.At, reached: now}
	case s.At > p.live.furthest+now.Sub(p.live.reached):
		p.live.furthest, p.live.reached = s.At, now
	}
	if s != nil {
		p.status = &followed{Status: *s, seen: now}
	}
	if s != nil && p.live != nil {
		slog.Debug("the url player follows", "at", s.At, "playing", s.Playing, "live", s.Live,
			"start", p.live.start, "furthest", p.live.furthest, "reached", p.live.reached)
	} else if s != nil {
		slog.Debug("the url player follows", "at", s.At, "playing", s.Playing, "live", s.Live)
	}
	shown := p.shown
	p.mu.Unlock()
	if shown != nil {
		p.o.Output.Changed(shown)
	}
}

func (p *Player) followed() (Status, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.status == nil {
		return Status{}, false
	}
	s := p.status.Status
	switch {
	case !s.Playing:
	case s.Break != nil:
		b := *s.Break
		b.At = min(b.At+time.Since(p.status.seen), b.Length)
		s.Break = &b
	default:
		s.At += time.Since(p.status.seen)
	}
	return s, true
}

func (p *Player) Program(pr Program) {
	at, ok := p.followed()
	if !ok || !at.Live || pr.Length <= 0 {
		return
	}
	p.mu.Lock()
	p.program = &programmed{Program: pr, offset: at.At - pr.At, seen: time.Now()}
	slog.Debug("the url player follows a program", "at", pr.At, "length", pr.Length,
		"earliest", pr.Earliest, "edge", pr.Edge, "offset", p.program.offset)
	shown := p.shown
	p.mu.Unlock()
	if shown != nil {
		p.o.Output.Changed(shown)
	}
}

func (p *Player) programmed() (pr Program, offset time.Duration, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.program == nil {
		return Program{}, 0, false
	}
	pr = p.program.Program
	pr.Edge = min(pr.Edge+time.Since(p.program.seen), pr.Length)
	return pr, p.program.offset, true
}

func (p *Player) window(at time.Duration) (start, edge time.Duration, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.live == nil {
		return 0, 0, false
	}
	edge = max(p.live.furthest+time.Since(p.live.reached), at)
	return p.live.start, edge, true
}

func (p *Player) Frames() int64 {
	at := p.Elapsed()
	p.mu.Lock()
	t, counted := p.cur, p.counted
	p.mu.Unlock()
	if t == nil {
		return counted
	}
	return counted + t.shownBy(at)
}

func (p *Player) recount(n int64) {
	p.mu.Lock()
	p.counted = n
	p.mu.Unlock()
}

func (p *Player) Describe(md *cast.Metadata) {
	p.mu.Lock()
	p.metadata = md
	shown := p.shown
	p.mu.Unlock()
	if shown != nil {
		p.o.Output.Changed(shown)
	}
}

func (p *Player) Begins() time.Duration {
	p.mu.Lock()
	t := p.cur
	p.mu.Unlock()
	if t == nil {
		return 0
	}
	return t.Start()
}

func (p *Player) Mark(marks []playback.Mark) {
	p.mu.Lock()
	p.marks = marks
	shown := p.shown
	p.mu.Unlock()
	if shown != nil {
		p.o.Output.Changed(shown)
	}
}

func (p *Player) marked() []playback.Mark {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.status == nil || len(p.status.Breaks) == 0 {
		return p.marks
	}
	marks := slices.Clone(p.marks)
	for _, at := range p.status.Breaks {
		marks = append(marks, playback.Mark{From: at, To: at, RGB: breakRGB})
	}
	return marks
}

func (p *Player) described() (cast.Metadata, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.metadata == nil {
		return cast.Metadata{}, false
	}
	return *p.metadata, true
}

func (p *Player) Play() error { return p.resume(nil) }

func (p *Player) Pause() error { return p.pause(nil) }

func (p *Player) Stop() error {
	if !p.stop(nil, p.o.Output.Stop) {
		p.o.Output.Stop(nil)
	}
	return nil
}

func (p *Player) Release() { p.stop(nil, p.o.Output.Handoff) }

func (p *Player) resume(only *track) error {
	t := p.current(only, false)
	if t == nil {
		return errIdle
	}
	p.o.Output.Resume(t)
	return nil
}

func (p *Player) pause(only *track) error {
	t := p.current(only, true)
	if t == nil {
		return errIdle
	}
	p.o.Output.Pause(t)
	return nil
}

func (p *Player) stop(only *track, give func(playback.Source)) bool {
	p.mu.Lock()
	t := p.cur
	if only != nil && only != t {
		p.mu.Unlock()
		return true
	}
	shown := p.shown
	p.cur, p.shown = nil, nil
	p.mu.Unlock()
	if shown != nil {
		give(shown)
		shown.close()
	}
	if t != nil {
		t.close()
	}
	return shown != nil
}

func (p *Player) Seek(to time.Duration) error { return p.seek(nil, to) }

func (p *Player) seek(only *track, to time.Duration) error {
	p.mu.Lock()
	t, paused := p.cur, p.paused
	p.mu.Unlock()
	if t == nil {
		return errIdle
	}
	if only != nil && only != t {
		return nil
	}
	refused := false
	if t.feed == nil {
		t.mu.Lock()
		refused = t.started && !t.seekable
		t.mu.Unlock()
	}
	at := p.Elapsed()
	d := to - at
	if refused && d > -offsetStart && d < offsetStart {
		return nil
	}
	if t.feed != nil && d >= 0 && p.showing() == t {
		if k, ok := t.feed.keyframe(to); ok && k <= at {
			t.skip(d)
			return nil
		}
	}
	if refused || t.media.Streaming() {
		return fmt.Errorf("urlplayer: %s cannot seek", t.media.URL())
	}
	p.recount(p.Frames())
	p.swap(p.track(t.media, t.feed, max(to, 0)), paused)
	return nil
}

func (p *Player) Elapsed() time.Duration {
	p.mu.Lock()
	t := p.cur
	p.mu.Unlock()
	if t == nil {
		return 0
	}
	at, _ := p.o.Output.Position(t)
	return at
}

type Shown struct {
	Width, Height int
	Length        time.Duration
}

func (p *Player) Shown() (Shown, bool) {
	t := p.showing()
	if t == nil {
		return Shown{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	s := Shown{Width: t.picture.Width, Height: t.picture.Height, Length: t.length}
	if s.Length == 0 && !t.media.Streaming() {
		s.Length = t.media.Length()
	}
	return s, true
}

func (p *Player) Ready() bool {
	p.mu.Lock()
	t, shown := p.cur, p.shown
	p.mu.Unlock()
	return t != nil && t == shown && !t.waiting.Load()
}

func (p *Player) ready(t *track) {
	p.mu.Lock()
	current := p.cur == t
	p.mu.Unlock()
	if current && !t.waiting.Load() && p.o.Ready != nil {
		p.o.Ready()
	}
}

func (p *Player) Report() string {
	p.mu.Lock()
	t, shown, paused := p.cur, p.shown, p.paused
	p.mu.Unlock()
	if t == nil {
		return "nothing is loaded"
	}
	t.mu.Lock()
	started, failed, format, seekable := t.started, t.failed, t.format, t.seekable
	w, h := t.picture.Width, t.picture.Height
	t.mu.Unlock()
	var b strings.Builder
	if t.feed != nil {
		b.WriteString("url: the page's media source\n")
	} else {
		fmt.Fprintf(&b, "url: %s\n", t.media.URL())
	}
	fmt.Fprintf(&b, "format: %s, seekable %v\n", firstOf(format, "not known yet"), seekable)
	switch {
	case !started:
		b.WriteString("state: opening\n")
	case failed != nil:
		fmt.Fprintf(&b, "state: failed: %v\n", failed)
	case shown != t:
		b.WriteString("state: opened, not shown yet\n")
	case paused:
		b.WriteString("state: paused\n")
	default:
		b.WriteString("state: playing\n")
	}
	if w > 0 {
		fmt.Fprintf(&b, "picture: %dx%d, %d frames handed to the screen\n", w, h, t.handed.Load())
	} else {
		b.WriteString("picture: none\n")
	}
	fmt.Fprintf(&b, "elapsed: %s", p.Elapsed().Round(100*time.Millisecond))
	return b.String()
}

func (p *Player) track(m cast.Media, f *Feed, at time.Duration) *track {
	ctx, cancel := context.WithCancel(context.Background())
	return &track{
		player: p, media: m, feed: f, at: at, client: p.o.HTTP, resample: p.o.Resample,
		screen: p.o.Screen, target: p.o.Target, ctx: ctx, cancel: cancel,
	}
}

func (p *Player) swap(t *track, paused bool) {
	p.mu.Lock()
	old := p.cur
	p.cur, p.paused = t, paused
	p.mu.Unlock()
	if old != nil && old != p.showing() {
		old.close()
	}
	safe.Go("urlplayer open", func() { p.show(t) })
}

func (p *Player) show(t *track) {
	err := t.ensure()
	p.mu.Lock()
	current, paused, old := p.cur == t, p.paused, p.shown
	if current && err == nil {
		p.shown = t
	}
	p.mu.Unlock()
	switch {
	case !current:
		t.close()
		return
	case err != nil:
		slog.Info("the url player could not open a stream", "err", err)
		p.o.Output.Failed(err)
		return
	}
	slog.Info("the url player opened a stream", "format", t.format, "seekable", t.seekable,
		"width", t.picture.Width, "height", t.picture.Height, "at", t.at)
	t.waiting.Store(t.screen && t.Pictured())
	if old != nil && old != t {
		p.o.Output.Handoff(old)
	}
	p.o.Output.Play(t)
	p.ready(t)
	if paused {
		p.o.Output.Pause(t)
	}
	if old != nil && old != t {
		old.close()
	}
}

func (p *Player) showing() *track {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.shown
}

func (p *Player) current(only *track, paused bool) *track {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur == nil || (only != nil && only != p.cur) {
		return nil
	}
	p.paused = paused
	return p.cur
}

var _ cast.Playing = (*Player)(nil)
