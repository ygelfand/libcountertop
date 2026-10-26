package doorbell

import (
	"context"
	"log/slog"
	"sync"

	"github.com/ygelfand/reolinkproxy/pkg/baichuan"

	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type link struct {
	door schema.DoorbellDoor

	mu sync.Mutex
	c  *baichuan.Client
}

func (d *Doorbell) link(slot int, door schema.DoorbellDoor) *link {
	d.mu.Lock()
	defer d.mu.Unlock()
	l := d.links[slot]
	if l != nil && sameDoorbell(l.door, door) {
		return l
	}
	if l != nil {
		go l.close()
	}
	l = &link{door: door}
	d.links[slot] = l
	return l
}

func sameDoorbell(a, b schema.DoorbellDoor) bool {
	return a.Host == b.Host && a.UID == b.UID && a.User == b.User && a.Password == b.Password
}

func (l *link) ensure(ctx context.Context) (*baichuan.Client, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.c != nil {
		select {
		case <-l.c.Done():
			l.c = nil
		default:
			return l.c, nil
		}
	}
	slog.Debug("doorbell connecting", "door", l.door.Name, "host", l.door.Host, "uid", l.door.UID)
	c, err := dial(ctx, l.door)
	if err != nil {
		slog.Warn("doorbell connect failed", "door", l.door.Name, "err", err)
		return nil, err
	}
	slog.Info("doorbell connected", "door", l.door.Name)
	l.c = c
	return c, nil
}

func (l *link) reset(c *baichuan.Client, why error) {
	l.mu.Lock()
	if l.c != c {
		l.mu.Unlock()
		return
	}
	l.c = nil
	l.mu.Unlock()
	slog.Warn("doorbell connection dropped", "door", l.door.Name, "err", why)
	_ = c.Close()
}

func (l *link) close() {
	l.mu.Lock()
	c := l.c
	l.c = nil
	l.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}
