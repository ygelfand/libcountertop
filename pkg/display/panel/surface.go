package panel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/surface"
)

const (
	uiLayer      = 1
	reconnect    = 3 * time.Second
	redialEvery  = time.Second
	strandEvery  = time.Second
	strandLinger = 500 * time.Millisecond
)

const helperWait = 60 * time.Second

func (d *Driver) open(ctx context.Context) (*Panel, error) {
	cfg := d.config()

	d.mu.Lock()
	wait := helperWait
	if d.opened {
		wait = 0
	}
	d.mu.Unlock()

	p, err := openSurface(cfg, wait)
	var stop func()
	for err != nil && !errors.Is(err, surface.ErrVersion) {
		if stop == nil {
			slog.Error("display: the helper is not answering", "err", err)
			stop = d.strand(cfg)
		}
		select {
		case <-ctx.Done():
			stop()
			return nil, ctx.Err()
		case <-time.After(redialEvery):
		}
		p, err = openSurface(cfg, 0)
	}
	if err != nil {
		if stop != nil {
			stop()
		}
		return nil, fmt.Errorf("display: %s: %w", cfg.Socket, err)
	}
	d.mu.Lock()
	d.opened = true
	if stop != nil {
		slog.Info("display: helper back")
		d.unstrand = stop
		d.forced = true
	}
	d.mu.Unlock()
	return p, nil
}

func (d *Driver) Stranded(draw func(*Panel) error) {
	d.mu.Lock()
	d.stranded = draw
	d.mu.Unlock()
}

type backdrop interface {
	show(draw func(*Panel) error) error
	close()
}

type framebufferBackdrop struct{ p *Panel }

func (b framebufferBackdrop) show(draw func(*Panel) error) error {
	if err := draw(b.p); err != nil {
		return err
	}
	b.p.toPanelOrder()
	return b.p.Flip()
}

func (b framebufferBackdrop) close() { _ = b.p.Close() }

type overlayBackdrop struct {
	o     overlay
	p     *Panel
	drawn bool
}

func (b *overlayBackdrop) show(draw func(*Panel) error) error {
	var drew error
	if !b.drawn {
		b.drawn = true
		drew = draw(b.p)
	}
	return errors.Join(drew, b.o.show())
}

func (b *overlayBackdrop) close() { b.o.close() }

// TODO: try the framebuffer on LANovo's boards and drop the overlay path if it shows.
func backdropFor(cfg Config) (backdrop, *Panel, error) {
	if cfg.Overlay.Ion == "" {
		p, err := Open(cfg.Framebuffer, cfg.Mounted)
		if err != nil {
			return nil, nil, err
		}
		return framebufferBackdrop{p: p}, p, nil
	}
	o, p, err := openOverlay(cfg, cfg.Width, cfg.Height)
	if err != nil {
		return nil, nil, err
	}
	return &overlayBackdrop{o: o, p: p}, p, nil
}

func (d *Driver) strand(cfg Config) (stop func()) {
	d.mu.Lock()
	draw, rot, brightness := d.stranded, d.rot, d.brightness
	d.mu.Unlock()
	if draw == nil {
		return func() {}
	}

	b, p, err := backdropFor(cfg)
	if err != nil {
		slog.Error("display: nowhere to show the stranded screen", "err", err)
		return func() {}
	}
	p.Turn(rot)
	if err := SetBacklight(brightness); err != nil {
		slog.Error("backlight", "err", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() {
		t := time.NewTicker(strandEvery)
		defer t.Stop()
		failed := false
		for {
			if err := b.show(draw); err != nil && !failed {
				slog.Error("display: showing the stranded screen", "err", err)
				failed = true
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	})
	return func() {
		cancel()
		wg.Wait()
		b.close()
	}
}

func openSurface(cfg Config, wait time.Duration) (*Panel, error) {
	c, err := surface.DialWait(cfg.Socket, wait)
	if err != nil {
		return nil, err
	}
	p := &Panel{surf: c, sock: cfg.Socket, askW: cfg.Width, askH: cfg.Height, surfaceZ: cfg.SurfaceZ}
	if err := p.attach(c); err != nil {
		_ = c.Close()
		return nil, err
	}
	p.rot = cfg.Mounted
	p.Width, p.Height = p.rot.Size(p.fbW, p.fbH)
	return p, nil
}

func (d *Driver) Helper() *surface.Client {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.panel == nil {
		return nil
	}
	return d.panel.surf
}

func (p *Panel) attach(c *surface.Client) error {
	w, h := c.Width, c.Height
	if p.askW > 0 && p.askH > 0 {
		w, h = p.askW, p.askH
	}
	l, err := c.Create(uiLayer, 0, 0, w, h, p.surfaceZ, 0)
	if err != nil {
		return fmt.Errorf("display: the UI layer: %w", err)
	}
	p.surf, p.layer = c, l
	c.Dropped.Listen(p.Dropped.Emit)
	p.fbW, p.fbH = w, h
	p.stride = l.Stride
	p.mem = l.Pixels
	p.back, p.front = 0, 0
	p.doubled = true
	return nil
}

func (p *Panel) flipSurface() error {
	x0, y0, x1, y1 := p.fbRect(p.clip)
	if x1 <= x0 || y1 <= y0 {
		return nil
	}
	p.seq++
	err := p.surf.Frame(uiLayer, p.seq, []surface.Rect{{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}})
	var st surface.Status
	if err == nil || errors.As(err, &st) {
		return err
	}
	return p.redial(err)
}

func (p *Panel) redial(cause error) error {
	kept := append([]byte(nil), p.mem...)
	_ = p.surf.Close()
	p.mem, p.layer = nil, nil

	c, err := surface.DialWait(p.sock, reconnect)
	if err != nil {
		return fmt.Errorf("display: lost the helper (%v) and could not get it back: %w", cause, err)
	}
	if err := p.attach(c); err != nil {
		_ = c.Close()
		return err
	}
	copy(p.mem, kept)
	p.seq++
	return p.surf.Frame(uiLayer, p.seq, []surface.Rect{{W: p.fbW, H: p.fbH}})
}
