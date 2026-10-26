package callvideo

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	display "github.com/ygelfand/libcountertop/pkg/display/geometry"
	"github.com/ygelfand/libcountertop/pkg/display/surface"
	"github.com/ygelfand/libcountertop/pkg/media/call"
	"github.com/ygelfand/libcountertop/pkg/media/rtc"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
)

const (
	remoteLayer = 920
	selfLayer   = 921
	remoteZ     = -2
	selfZ       = -1
	videoLag    = 200 * time.Millisecond
	feedRetry   = 20 * time.Millisecond
	feedGiveUp  = 200 * time.Millisecond
	rejoinWait  = 500 * time.Millisecond
	selfShare   = 0.28
	selfMargin  = 0.03
	sideShare   = 0.25
	belowShare  = 0.3
	turnCheck   = 250 * time.Millisecond
)

type Size = call.Size

type Display interface {
	Helper() *surface.Client
	Orientation() display.Orientation
	Native() (int, int)
}

type Dependencies struct {
	Display Display
	Camera  *Camera
}

type Video struct{ o Dependencies }

func New(o Dependencies) *Video { return &Video{o: o} }

type Layout struct {
	Remote  display.Rect
	Picture display.Rect
	Self    display.Rect
	Panel   display.Rect
	Side    bool
}

func Arrange(vw, vh int, remote, own Size) Layout {
	full := fullScreen(vw, vh)
	m := int(float64(min(vw, vh)) * selfMargin)
	if remote.Width > 0 && remote.Height > 0 {
		r := fit(full, remote.Width, remote.Height)
		switch {
		case vh > vw && vh-r.H >= int(float64(vh)*belowShare):
			pic := display.Rect{W: vw, H: r.H}
			panel := display.Rect{Y: r.H, W: vw, H: vh - r.H}
			self := display.Rect{X: panel.W / 2, Y: panel.Y + m, W: panel.W/2 - m, H: panel.H / 2}
			return Layout{
				Remote:  pic,
				Picture: pic,
				Panel:   panel,
				Side:    true,
				Self:    fitOwn(self, own, true),
			}
		case vw > vh && vw-r.W >= int(float64(vw)*sideShare):
			pic := display.Rect{W: r.W, H: vh}
			panel := display.Rect{X: r.W, W: vw - r.W, H: vh}
			self := display.Rect{
				X: panel.X + m,
				Y: panel.H / 5,
				W: panel.W - 2*m,
				H: panel.H * 2 / 5,
			}
			return Layout{
				Remote:  pic,
				Picture: pic,
				Panel:   panel,
				Side:    true,
				Self:    fitOwn(self, own, false),
			}
		}
		return Layout{Remote: full, Picture: r, Panel: full, Self: corner(vw, vh)}
	}
	return Layout{Remote: full, Picture: full, Panel: full, Self: corner(vw, vh)}
}

func fitOwn(box display.Rect, own Size, right bool) display.Rect {
	if own.Width == 0 || own.Height == 0 {
		return box
	}
	r := fit(box, own.Width, own.Height)
	if right {
		r.X = box.X + box.W - r.W
	}
	r.Y = box.Y
	return r
}

type layer struct {
	display Display
	codec   uint32
	id      uint32
	z       int
	size    Size
	box     func(vw, vh int) display.Rect
	broken  func()

	mu      sync.Mutex
	open    bool
	shown   bool
	hidden  bool
	closed  bool
	frames  uint64
	decoded uint64
	failed  error
}

func (l *layer) stats() call.LayerStats {
	if l == nil {
		return call.LayerStats{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return call.LayerStats{
		Open:   l.open,
		Width:  l.size.Width,
		Height: l.size.Height,
		Frames: l.frames,
		Shown:  l.decoded,
		Failed: l.failed,
	}
}

func (l *layer) Show(f rtc.Frame) {
	c := l.display.Helper()
	if c == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	if !l.open {
		if !f.Key || l.size.Width == 0 {
			return
		}
		_ = c.VideoClose(l.id)
		if err := c.VideoOpen(
			l.id,
			l.codec,
			l.size.Width,
			l.size.Height,
			l.z,
			0,
			"",
		); err != nil {
			slog.Warn(
				"call video open",
				"layer",
				l.id,
				"width",
				l.size.Width,
				"height",
				l.size.Height,
				"err",
				err,
			)
			l.failed = err
			return
		}
		slog.Info(
			"call video opened",
			"layer",
			l.id,
			"width",
			l.size.Width,
			"height",
			l.size.Height,
		)
		l.open, l.failed = true, nil
	}
	played, err := feed(c, l.id, f)
	if err != nil {
		l.failed = err
		if errors.Is(err, surface.Broken) {
			slog.Warn("call video decoder broke, reopening", "layer", l.id)
			_ = c.VideoClose(l.id)
			l.open, l.shown = false, false
			if l.broken != nil {
				go l.broken()
			}
			return
		}
		slog.Debug("call video feed", "layer", l.id, "err", err)
		return
	}
	l.frames++
	l.decoded = uint64(played.Shown)
	if err := c.VideoClock(l.id, f.PTS-videoLag, true); err != nil {
		slog.Debug("call video clock", "layer", l.id, "err", err)
	}
	if !l.shown {
		l.shown = true
		l.place(c)
	}
}

func (l *layer) update(sz Size, hidden bool) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	c := l.display.Helper()
	if sz != l.size {
		l.size = sz
		if l.open {
			if c != nil {
				_ = c.VideoClose(l.id)
			}
			l.open, l.shown = false, false
		}
	}
	l.hidden = hidden
	if c != nil && l.shown {
		l.place(c)
	}
}

func (l *layer) place(c *surface.Client) {
	o := l.display.Orientation()
	fw, fh := l.display.Native()
	vw, vh := o.Size(fw, fh)
	at := fit(l.box(vw, vh), l.size.Width, l.size.Height)
	x, y, m, _ := o.Place(fw, fh, at, l.size.Width, l.size.Height)
	if err := c.VideoPlace(l.id, x, y, m, l.z, !l.hidden); err != nil {
		slog.Debug("call video place", "layer", l.id, "err", err)
	}
}

func (l *layer) reorient() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if c := l.display.Helper(); c != nil && l.shown {
		l.place(c)
	}
}

func (l *layer) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	if !l.open {
		return
	}
	if c := l.display.Helper(); c != nil {
		_ = c.VideoClose(l.id)
	}
	l.open, l.shown = false, false
}

func feed(c *surface.Client, id uint32, f rtc.Frame) (surface.Played, error) {
	var flags uint32
	if f.Key {
		flags = surface.SampleKey
	}
	give := time.Now().Add(feedGiveUp)
	for {
		played, err := c.VideoSample(id, f.PTS, flags, f.Data)
		if !errors.Is(err, surface.Full) || time.Now().After(give) {
			return played, err
		}
		time.Sleep(feedRetry)
	}
}

func fit(box display.Rect, w, h int) display.Rect {
	dw, dh := box.W, box.W*h/max(1, w)
	if dh > box.H {
		dw, dh = box.H*w/max(1, h), box.H
	}
	return display.Rect{X: box.X + (box.W-dw)/2, Y: box.Y + (box.H-dh)/2, W: dw, H: dh}
}

func fullScreen(vw, vh int) display.Rect { return display.Rect{W: vw, H: vh} }

func corner(vw, vh int) display.Rect {
	w := int(float64(vw) * selfShare)
	h := int(float64(vh) * selfShare)
	m := int(float64(min(vw, vh)) * selfMargin)
	return display.Rect{X: vw - w - m, Y: vh - h - m, W: w, H: h}
}

type pictures struct {
	display Display
	workers sync.WaitGroup
	mu      sync.Mutex
	shown   call.Shown
	remote  *layer
	self    *layer
	stop    func()
}

func (v *Video) Start(f call.Feed) (call.Picture, rtc.Camera) {
	p := &pictures{display: v.o.Display, shown: f.Shown}
	p.remote = &layer{
		display: v.o.Display,
		codec:   surface.AVC,
		id:      remoteLayer,
		z:       remoteZ,
		size:    f.Shown.Remote,
		hidden:  !f.Shown.RemoteOn,
		box:     func(vw, vh int) display.Rect { return p.layout(vw, vh).Remote },
		broken:  f.Key,
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.stop = cancel
	var sending rtc.Camera
	if at, own, ok := v.o.Camera.Select(f.Stream); ok {
		f.Sized(own)
		p.mu.Lock()
		p.shown.Own = own
		p.mu.Unlock()
		p.see(ctx, camera{source: v.o.Camera, at: at, sized: f.Sized}, own, f.Shown.SelfOn)
		sending = camera{source: v.o.Camera, at: at}
	}
	p.workers.Add(1)
	safe.Go("call video turn", func() { defer p.workers.Done(); p.follow(ctx) })
	return p, sending
}

func (p *pictures) layout(vw, vh int) Layout {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Arrange(vw, vh, p.shown.Remote, p.shown.Own)
}

func (p *pictures) see(ctx context.Context, cam camera, own Size, visible bool) {
	p.self = &layer{
		display: p.display,
		codec:   surface.AVC,
		id:      selfLayer,
		z:       selfZ,
		size:    own,
		hidden:  !visible,
		box:     func(vw, vh int) display.Rect { return p.layout(vw, vh).Self },
	}
	self := p.self
	frames, release := cam.Frames()
	cancel := p.stop
	p.stop = func() { cancel(); release() }
	p.workers.Add(1)
	safe.Go("call self view", func() {
		defer p.workers.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case f, ok := <-frames:
				if !ok {
					return
				}
				self.Show(f)
			}
		}
	})
}

func (p *pictures) follow(ctx context.Context) {
	tick := time.NewTicker(turnCheck)
	defer tick.Stop()
	was := p.display.Orientation()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if now := p.display.Orientation(); now != was {
				was = now
				p.replace()
			}
		}
	}
}

func (p *pictures) replace() {
	p.remote.reorient()
	p.self.reorient()
}

func (p *pictures) Show(f rtc.Frame) { p.remote.Show(f) }

func (p *pictures) Update(sh call.Shown) {
	p.mu.Lock()
	p.shown = sh
	p.mu.Unlock()
	p.remote.update(sh.Remote, !sh.RemoteOn)
	p.self.update(sh.Own, !sh.SelfOn)
}

func (p *pictures) Stats() (remote, self call.LayerStats) {
	return p.remote.stats(), p.self.stats()
}

func (p *pictures) Close() {
	p.stop()
	p.workers.Wait()
	if p.self != nil {
		p.self.close()
	}
	if p.remote != nil {
		p.remote.close()
	}
}
