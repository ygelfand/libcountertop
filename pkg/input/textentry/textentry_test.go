package textentry

import (
	"context"
	"errors"
	"testing"
)

func TestControllerUnavailableAndDetached(t *testing.T) {
	c := &Controller{}
	if _, err := c.Call(context.Background(), Request{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unattached = %v", err)
	}
	detach := c.Attach(func(f func()) { f() }, func(r Request) (View, error) {
		return View{Target: "field", Text: r.Text}, nil
	})
	v, err := c.Call(context.Background(), Request{Text: "hello"})
	if err != nil || v.Text != "hello" {
		t.Fatalf("call = %+v, %v", v, err)
	}
	detach()
	if _, err := c.Call(context.Background(), Request{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("detached = %v", err)
	}
}

func TestCancelledRequestDoesNotEdit(t *testing.T) {
	c := &Controller{}
	var queued func()
	edits := 0
	c.Attach(func(f func()) { queued = f }, func(Request) (View, error) {
		edits++
		return View{}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Call(ctx, Request{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled = %v", err)
	}
	if queued != nil {
		queued()
	}
	if edits != 0 {
		t.Fatalf("edits = %d", edits)
	}
}
