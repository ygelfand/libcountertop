package clock

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/hook"
	"github.com/ygelfand/libcountertop/pkg/settings/timezone"
)

const (
	settle = 10 * time.Second
	resync = time.Hour
	retry  = 30 * time.Second

	jump = 2 * time.Second
)

var (
	stepping   = setClock
	slewing    = slew
	writingRTC = writeRTC
)

type Options struct {
	Servers func() []net.IP
	Built   string
	RTC     string
	Zone    timezone.PolicyOptions
}

type Sync struct {
	Before, After time.Time
}

func (s Sync) Stepped() bool { return !s.After.Equal(s.Before) }

type Clock struct {
	Synced hook.Hook[Sync]

	options Options
	zone    *timezone.Policy
	set     atomic.Bool

	mu    sync.Mutex
	asked *esphome.Conn
	rtc   string
}

func New(options Options) *Clock {
	return &Clock{options: options, zone: timezone.NewPolicy(options.Zone), rtc: options.RTC}
}

func (c *Clock) Name() string { return "clock" }

func (c *Clock) Run(ctx context.Context) error {
	if err := sleep(ctx, settle); err != nil {
		return err
	}

	for {
		wait := resync
		if err := c.sync(ctx); err != nil {
			slog.Warn("no time from a time server", "err", err)
			wait = retry
		}

		if err := sleep(ctx, wait); err != nil {
			return err
		}
	}
}

func (c *Clock) Valid() bool {
	if c.set.Load() {
		return true
	}
	built, err := time.Parse(time.RFC3339, c.options.Built)
	return err != nil || time.Now().After(built)
}

func (c *Clock) WatchSteps(changed func(time.Duration)) func() {
	return c.Synced.Listen(func(s Sync) {
		if s.Stepped() {
			changed(s.After.Sub(s.Before))
		}
	})
}

func (c *Clock) Entities() []esphome.Entity { return []esphome.Entity{c.zone.Select} }

func (c *Clock) Restore() { c.zone.Restore() }

func (c *Clock) Zone() string {
	saved := c.options.Zone.Read()
	if saved.Chosen != "" {
		return saved.Chosen
	}
	return saved.Home
}

func (c *Clock) ChosenZone() string { return c.zone.Zone() }

func (c *Clock) SetZone(s string) { c.zone.SetZone(s) }

func (c *Clock) accept(offset, within time.Duration, source string) error {
	before := time.Now()
	after := before

	switch {
	case offset.Abs() <= within:
	case offset.Abs() < jump:
		if err := slewing(offset); err != nil {
			return err
		}
		slog.Info("clock adjusting", "source", source, "by", offset.Round(time.Millisecond))
	default:
		after = before.Add(offset)
		if err := stepping(after); err != nil {
			return err
		}
		slog.Info("clock set", "source", source, "by", offset.Round(time.Millisecond), "now", after)
	}

	c.set.Store(true)
	c.keepRTC()
	c.Synced.Emit(Sync{Before: before, After: after})
	return nil
}

func (c *Clock) keepRTC() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rtc == "" {
		return
	}
	if err := writingRTC(c.rtc, time.Now()); err != nil {
		slog.Warn("not keeping the hardware clock", "err", err)
		c.rtc = ""
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
