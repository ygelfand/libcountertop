package textentry

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrUnavailable = errors.New("open a text field on the device")
	ErrTarget      = errors.New("scan a fresh code for this field")
	ErrConflict    = errors.New("reload the field before editing")
	ErrInvalid     = errors.New("send a valid text edit")
)

const TextMost = 16 << 10

type View struct {
	Target   string `json:"target"`
	Text     string `json:"text"`
	Password bool   `json:"password"`
	Revision uint64 `json:"revision"`
}

type Request struct {
	Target   string `json:"target"`
	Text     string `json:"text"`
	Action   string `json:"action"`
	Key      string `json:"key"`
	Revision uint64 `json:"revision"`
}

type binding struct {
	queue  func(func())
	handle func(Request) (View, error)
}

type Controller struct {
	mu    sync.Mutex
	bound *binding
}

func (c *Controller) Attach(queue func(func()), handle func(Request) (View, error)) func() {
	b := &binding{queue: queue, handle: handle}
	c.mu.Lock()
	c.bound = b
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		if c.bound == b {
			c.bound = nil
		}
		c.mu.Unlock()
	}
}

func (c *Controller) Call(ctx context.Context, request Request) (View, error) {
	if err := ctx.Err(); err != nil {
		return View{}, err
	}
	c.mu.Lock()
	b := c.bound
	c.mu.Unlock()
	if b == nil {
		return View{}, ErrUnavailable
	}
	type result struct {
		view View
		err  error
	}
	done := make(chan result, 1)
	b.queue(func() {
		if ctx.Err() != nil {
			return
		}
		c.mu.Lock()
		active := c.bound == b
		c.mu.Unlock()
		if !active {
			done <- result{err: ErrUnavailable}
			return
		}
		view, err := b.handle(request)
		done <- result{view, err}
	})
	select {
	case <-ctx.Done():
		return View{}, ctx.Err()
	case out := <-done:
		return out.view, out.err
	}
}
