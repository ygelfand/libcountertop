package compositor

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/ygelfand/libcountertop/pkg/display/compositor/internal/wayland"
	"github.com/ygelfand/libcountertop/pkg/display/compositor/internal/wire"
	"github.com/ygelfand/libcountertop/pkg/hook"
)

type Options struct {
	Path          string
	Width, Height int
}

type Compositor struct {
	o Options
	l *net.UnixListener

	mu      sync.Mutex
	windows []*Window

	Opened hook.Hook[*Window]
}

func New(o Options) *Compositor { return &Compositor{o: o} }

func (c *Compositor) Listen() error {
	if err := os.MkdirAll(filepath.Dir(c.o.Path), 0o700); err != nil {
		return fmt.Errorf("compositor: %w", err)
	}
	if err := os.Remove(c.o.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("compositor: %w", err)
	}
	l, err := wire.ListenPath(c.o.Path)
	if err != nil {
		return fmt.Errorf("compositor: %w", err)
	}
	c.l = l
	return nil
}

func (c *Compositor) Run(ctx context.Context) error {
	if c.l == nil {
		if err := c.Listen(); err != nil {
			return err
		}
	}
	srv := &wayland.Server{Listener: c.l, Handler: c.serve}
	return srv.Run(ctx)
}

func (c *Compositor) Windows() []*Window {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.windows)
}

func (c *Compositor) serve(ctx context.Context, conn *wayland.Client) {
	cl := newClient(c, conn)
	defer cl.close()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-conn.Events():
			if !ok {
				return
			}
			if err := ev(); err != nil {
				slog.Debug("a compositor client sent a bad request", "err", err)
				return
			}
		}
	}
}

func (c *Compositor) opened(w *Window) {
	c.mu.Lock()
	c.windows = append(c.windows, w)
	c.mu.Unlock()
	c.Opened.Emit(w)
}

func (c *Compositor) closed(w *Window) {
	c.mu.Lock()
	c.windows = slices.DeleteFunc(c.windows, func(x *Window) bool { return x == w })
	c.mu.Unlock()
	w.Closed.Emit(struct{}{})
}
