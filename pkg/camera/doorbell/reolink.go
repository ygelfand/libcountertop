package doorbell

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/ygelfand/reolinkproxy/pkg/baichuan"

	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	snapWait = 15 * time.Second
)

var (
	ErrUnreachable = errors.New("doorbell: unreachable")
	ErrBadLogin    = errors.New("doorbell: login refused")
	ErrNoPassword  = errors.New("doorbell: no password")
)

type OtherDoorbell struct{ UID string }

func (e *OtherDoorbell) Error() string {
	return fmt.Sprintf("doorbell: a different doorbell answers there (%s)", e.UID)
}

func LoginCode(err error) (int, bool) {
	var status *baichuan.StatusError
	if !errors.As(err, &status) {
		return 0, false
	}
	return int(status.Code), true
}

func dial(ctx context.Context, door schema.DoorbellDoor) (*baichuan.Client, error) {
	c, err := baichuan.Dial(ctx, baichuan.Config{
		Host: door.Host, UID: door.UID, Username: door.User, Password: door.Password,
	})
	if err != nil {
		return nil, errors.Join(ErrUnreachable, err)
	}
	if err := c.Login(ctx); err != nil {
		_ = c.Close()
		var status *baichuan.StatusError
		if errors.As(err, &status) {
			return nil, errors.Join(ErrBadLogin, err)
		}
		return nil, errors.Join(ErrUnreachable, err)
	}
	return c, nil
}

func (d *Doorbell) reolinkEvents(ctx context.Context, slot int, door schema.DoorbellDoor) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	l := d.link(slot, door)
	c, err := l.ensure(ctx)
	if err != nil {
		if errors.Is(err, ErrUnreachable) {
			d.mark(slot, Unreachable)
		} else {
			d.mark(slot, BadLogin)
		}
		return err
	}

	var mu sync.Mutex
	var active []string
	stop, err := c.ListenForMotion(ctx, uint8(door.Channel), func(e baichuan.MotionEvent) {
		now := slices.Concat(e.AITypes, e.States)
		if slices.Contains(now, "MD") || slices.Contains(now, "other") {
			now = append(now, string(Motion))
		}
		mu.Lock()
		was := active
		active = now
		mu.Unlock()
		for _, k := range kinds {
			if slices.Contains(now, string(k)) && !slices.Contains(was, string(k)) {
				go d.record(ctx, slot, door, k)
			}
		}
	})
	if err != nil {
		l.reset(c, err)
		d.mark(slot, Unreachable)
		return errors.Join(ErrUnreachable, err)
	}
	defer stop()
	d.via(slot, sourceReolink, "")
	d.mark(slot, Ready)

	select {
	case <-ctx.Done():
		return nil
	case <-c.Done():
		l.reset(c, c.Err())
		d.mark(slot, Unreachable)
		return errors.Join(ErrUnreachable, c.Err())
	}
}

func (d *Doorbell) Snapshot(ctx context.Context, slot int) ([]byte, error) {
	door, ok := doorAt(d.o.Settings.Read(), slot)
	if !ok {
		return nil, errUnsupported
	}
	if !door.Signed() {
		return nil, ErrNoPassword
	}
	ctx, cancel := context.WithTimeout(ctx, snapWait)
	defer cancel()
	l := d.link(slot, door)
	c, err := l.ensure(ctx)
	if err != nil {
		return nil, err
	}
	pic, err := c.Snap(ctx, uint8(door.Channel), snapStream(door.Stream))
	if err != nil {
		select {
		case <-c.Done():
			l.reset(c, err)
		default:
		}
	}
	return pic, err
}

func reolinkCheck(ctx context.Context, door schema.DoorbellDoor) ([]byte, error) {
	c, err := dial(ctx, door)
	if errors.Is(err, ErrUnreachable) && door.UID != "" && door.Host != "" {
		if uid, ok := answering(ctx, door); ok && uid != door.UID {
			return nil, &OtherDoorbell{UID: uid}
		}
	}
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return c.Snap(ctx, uint8(door.Channel), snapStream(door.Stream))
}

func answering(ctx context.Context, door schema.DoorbellDoor) (string, bool) {
	door.UID = ""
	c, err := dial(ctx, door)
	if err != nil {
		return "", false
	}
	defer c.Close()
	uid, err := c.GetUID(ctx)
	return uid, err == nil && uid != ""
}

func snapStream(s schema.DoorbellStream) string {
	if s == schema.DoorbellSub {
		return "sub"
	}
	return "main"
}
