package service

import (
	"context"
	"log/slog"
	"time"
)

func Keep(ctx context.Context, name string, f func(context.Context) error, opts ...Option) error {
	p := defaults()
	p.restart = true
	for _, o := range opts {
		o(&p)
	}
	wait := p.backoff
	for {
		began := time.Now()
		err := run(ctx, name, f)
		if ctx.Err() != nil || err == nil {
			return nil
		}
		if !p.restart || isPermanent(err) {
			return err
		}
		if time.Since(began) >= p.steady {
			wait = p.backoff
		}
		slog.Warn("helper stopped", "helper", name, "err", err, "restart_in", wait)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
		wait = grow(wait, p.maxBackoff)
	}
}
