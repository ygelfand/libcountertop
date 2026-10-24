package videopage

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ygelfand/libcountertop/pkg/audio/volume"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/media/nowplaying"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	spinDots  = 12
	spinEvery = 100 * time.Millisecond
	spinAfter = 300 * time.Millisecond
)

type Page struct {
	options Dependencies
	c       Controls

	mu       sync.Mutex
	picture  bool
	loading  bool
	began    time.Time
	note     string
	logo     *ui.Image
	backdrop *ui.Image
	frameW   int
	frameH   int
	shown    bool
	paused   bool
	until    time.Time
	drawn    string
	hold     *shell.Hold
}

func (p *Page) Shows(s config.Stream) bool { return s == config.StreamMedia }

func (p *Page) reveal() {
	p.mu.Lock()
	p.shown, p.until = true, time.Now().Add(linger)
	p.mu.Unlock()
	p.redraw()
}

type Look struct {
	Controls Controls
	Picture  bool
	Spinning bool
	Shown    bool
	Began    time.Time
	Note     string
	Logo     *ui.Image
	Backdrop *ui.Image
	FrameW   int
	FrameH   int
}

func (p *Page) Look() Look {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Look{
		Controls: p.c,
		Picture:  p.picture,
		Spinning: p.loading && time.Since(p.began) > spinAfter,
		Shown:    p.shown,
		Began:    p.began,
		Note:     p.note,
		Logo:     p.logo,
		Backdrop: p.backdrop,
		FrameW:   p.frameW,
		FrameH:   p.frameH,
	}
}

func (p *Page) Reveal() { p.reveal() }

func (p *Page) Conceal() {
	p.mu.Lock()
	p.shown = false
	p.mu.Unlock()
	p.redraw()
}

func (p *Page) Linger() {
	p.mu.Lock()
	p.until = time.Now().Add(linger)
	p.mu.Unlock()
}

func New(c Controls, o Dependencies) *Page {
	return &Page{c: c, options: o, loading: true, began: time.Now()}
}

func (p *Page) Covers() bool { return true }

func (p *Page) ControlsBelow() bool { return true }

func (p *Page) Docks() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.shown || !p.picture
}

func (p *Page) Follow(c Controls) {
	p.mu.Lock()
	p.c = c
	p.mu.Unlock()
}

func (p *Page) controls() Controls {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.c
}

func (p *Page) SetPicture(on bool) {
	p.mu.Lock()
	p.picture = on
	if on {
		p.loading = false
	}
	up := p.hold.Held()
	p.mu.Unlock()
	p.redraw()
	if on && !up {
		p.Show()
	}
}

func (p *Page) Wakes() bool {
	p.mu.Lock()
	picture, c := p.picture, p.c
	p.mu.Unlock()
	if picture {
		return true
	}
	w, ok := c.(shell.Waker)
	return ok && w.Wakes()
}

func (p *Page) SetLoading(on bool) {
	p.mu.Lock()
	changed := on != p.loading
	if on && changed {
		p.began = time.Now()
	}
	p.loading = on
	p.mu.Unlock()
	if changed {
		p.redraw()
	}
}

func (p *Page) SetNote(note string) {
	p.mu.Lock()
	p.note = note
	if note != "" {
		p.loading = false
	}
	p.mu.Unlock()
	p.redraw()
}

func (p *Page) SetFrame(w, h int) {
	p.mu.Lock()
	p.frameW, p.frameH = w, h
	p.mu.Unlock()
	p.redraw()
}

func (p *Page) SetLogo(img *ui.Image) {
	p.mu.Lock()
	p.logo = img
	p.mu.Unlock()
	p.redraw()
}

func (p *Page) SetBackdrop(img *ui.Image) {
	p.mu.Lock()
	p.backdrop = img
	p.mu.Unlock()
	p.redraw()
}

func (p *Page) redraw() {
	if p.options.Shell.Top() == p {
		p.options.Shell.Redraw()
	}
}

func (p *Page) Run(ctx context.Context) {
	p.Show()
	quiet := p.options.Volume.Watch(func(int) {
		if p.options.Shell.Top() == p {
			p.reveal()
		}
	})
	defer quiet()
	defer func() {
		p.mu.Lock()
		hold := p.hold
		p.hold = nil
		p.mu.Unlock()
		hold.Release()
	}()
	t := time.NewTicker(spinEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.tick()
		}
	}
}

func (p *Page) Show() {
	p.mu.Lock()
	held := p.hold.Held()
	p.mu.Unlock()
	if held {
		return
	}
	hold := p.options.Shell.Hold(p)
	p.mu.Lock()
	p.hold = hold
	p.mu.Unlock()
}

var watched atomic.Int64

func Watched() time.Time {
	at := watched.Load()
	if at == 0 {
		return time.Time{}
	}
	return time.Unix(0, at)
}

func breakKey(b *nowplaying.Break) string {
	if b == nil {
		return ""
	}
	return fmt.Sprintf("%d/%d@%d", b.Clip, b.Clips, b.At/time.Second)
}

func (p *Page) tick() {
	now := p.controls().Now()
	p.mu.Lock()
	if p.picture && now.Playing && !now.Paused {
		watched.Store(time.Now().UnixNano())
	}
	if p.shown && now.Playing && !now.Paused && time.Now().After(p.until) {
		p.shown = false
	}
	if !p.shown && now.Paused && !p.paused {
		p.shown, p.until = true, time.Now().Add(linger)
	}
	p.paused = now.Paused
	spinning := p.loading && time.Since(p.began) > spinAfter
	key := fmt.Sprintf(
		"%v|%v|%d|%v|%v|%s|%s|%s|%d|%d|%d|%d|%s",
		p.shown,
		p.picture,
		now.Elapsed/time.Second,
		now.Paused,
		now.Playing,
		now.Title,
		now.Artist,
		now.Album,
		now.ArtID,
		now.MarkID,
		now.Can,
		p.options.Volume.Level(),
		breakKey(now.Break),
	)
	changed := key != p.drawn
	p.drawn = key
	p.mu.Unlock()
	if spinning || changed {
		p.redraw()
	}
}

type Dependencies struct {
	Shell  *shell.Shell
	Volume volume.Channel
}
