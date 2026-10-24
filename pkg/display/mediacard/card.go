package mediacard

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/media/nowplaying"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

type Source = nowplaying.Source
type Opener = nowplaying.Opener
type Presenter interface{ Presents() bool }
type Player interface {
	Now() nowplaying.Now
	Holder() nowplaying.Source
	ResetSession()
}
type Dependencies struct {
	Player Player
	Idle   storage.Section[config.Idle]
	Shell  *shell.Shell
}

func New(o Dependencies) *Card { return &Card{options: o, page: &Screen{}} }
func (c *Card) Page() *Screen  { return c.page }
func (c *Card) Began(s Source) { c.mu.Lock(); c.begun = s; c.mu.Unlock() }

func (c *Card) Close() { c.mu.Lock(); h := c.held; c.held = nil; c.mu.Unlock(); h.Release() }

type Screen struct {
	mu    sync.Mutex
	shown time.Duration
}

func (v *Screen) Covers() bool               { return true }
func (v *Screen) Docks() bool                { return true }
func (v *Screen) Shows(s config.Stream) bool { return s == config.StreamMedia }
func (v *Screen) Timeout() time.Duration     { return shell.SettingsTimeout }
func (v *Screen) Moved(elapsed time.Duration) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	second := elapsed / time.Second
	if second == v.shown {
		return false
	}
	v.shown = second
	return true
}

const (
	look = 250 * time.Millisecond
	gap  = 5 * time.Second
)

type Card struct {
	options Dependencies
	page    *Screen
	mu      sync.Mutex
	held    *shell.Hold
	sounded time.Time
	stalled time.Time
	begun   Source
}

func (c *Card) Name() string { return "player" }

func (c *Card) Run(ctx context.Context) error {
	defer c.Close()
	tick := time.NewTicker(look)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		c.Follow()
		c.Tick()
	}
}

func (c *Card) Tick() {
	if c.options.Shell.Top() != c.page {
		return
	}
	now := c.options.Player.Now()
	if !now.Playing || now.Length <= 0 {
		return
	}
	if c.page.Moved(now.Elapsed) {
		c.options.Shell.Redraw()
	}
}

func (c *Card) Follow() {
	now := c.options.Player.Now()
	playing := now.Playing || now.Hold
	src := c.options.Player.Holder()

	idle := !now.Playing && (now.Paused || now.Hold)
	c.mu.Lock()
	begun := c.begun
	c.begun = nil
	if playing {
		c.sounded = time.Now()
	}
	switch {
	case !idle:
		c.stalled = time.Time{}
	case c.stalled.IsZero():
		c.stalled = time.Now()
	}
	stalled := time.Since(c.stalled)
	held, quiet := c.held, time.Since(c.sounded)
	if !playing && quiet > gap && begun == nil {
		c.options.Player.ResetSession()
	}
	c.mu.Unlock()

	switch {
	case begun != nil && begun == src:
		slog.Info("the player opened", "by", fmt.Sprintf("%T", src))
		c.open()

	case !playing && held.Held() && quiet > gap:
		slog.Info("the player is done", "quiet", quiet.Round(time.Second))
		held.Keep(false)
	}

	if limit := c.options.Idle.Read().Media.After(); idle && src != nil && limit > 0 &&
		stalled > limit {
		slog.Info(
			"the player was left idle",
			"for",
			stalled.Round(time.Second),
			"by",
			fmt.Sprintf("%T", src),
		)
		c.mu.Lock()
		c.stalled = time.Time{}
		c.mu.Unlock()
		src.Stop()
	}
}

func (c *Card) Open() { c.open() }

func (c *Card) open() {
	if o, ok := c.options.Player.Holder().(Opener); ok && o.Open() {
		c.mu.Lock()
		held := c.held
		c.held = nil
		c.mu.Unlock()
		held.Release()
		return
	}
	if p, ok := c.options.Shell.Top().(Presenter); ok && p.Presents() {
		return
	}
	c.mu.Lock()
	held := c.held
	c.mu.Unlock()
	if held.Held() {
		return
	}
	hold := c.options.Shell.Hold(c.page)
	c.mu.Lock()
	c.held = hold
	c.mu.Unlock()
}
