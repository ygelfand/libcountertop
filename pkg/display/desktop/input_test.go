package desktop

import (
	"context"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/input/textentry"

	"github.com/ygelfand/libcountertop/pkg/input/touch"
)

func TestKeyboardDataUsesExistingInputChannel(t *testing.T) {
	controller := &textentry.Controller{}
	var requests []textentry.Request
	controller.Attach(func(f func()) { f() }, func(r textentry.Request) (textentry.View, error) {
		requests = append(requests, r)
		return textentry.View{}, nil
	})
	s := &session{ctx: context.Background(), server: &Server{keyboard: controller}}
	for _, raw := range []string{`{"phase":"text","text":"hello 🦋"}`, `{"phase":"key","key":"Backspace"}`} {
		if err := s.receive([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	if len(requests) != 2 || requests[0].Action != "type" || requests[0].Text != "hello 🦋" ||
		requests[1].Key != "Backspace" {
		t.Fatalf("requests = %+v", requests)
	}
	if s.input.last.IsZero() {
		t.Fatal("typing did not reset idle deadline")
	}
}

func TestPointerLifecycle(t *testing.T) {
	var got []touch.Contact
	p := pointer{deliver: func(c touch.Contact) { got = append(got, c) }, width: 800, height: 480}
	for _, message := range []string{
		`{"phase":"move","x":0.5,"y":0.5}`,
		`{"phase":"down","x":0.5,"y":0.5}`,
		`{"phase":"down","x":0,"y":0}`,
		`{"phase":"move","x":1,"y":1}`,
	} {
		p.receive([]byte(message))
	}
	p.release()
	if len(got) != 3 {
		t.Fatalf("contacts = %v", got)
	}
	if got[0].Phase != touch.Down || got[0].X != 400 || got[0].Y != 240 {
		t.Fatalf("down = %v", got[0])
	}
	if got[1].Phase != touch.Move || got[1].X != 799 || got[1].Y != 479 {
		t.Fatalf("move = %v", got[1])
	}
	if got[2].Phase != touch.Up || got[2].ID != got[0].ID {
		t.Fatalf("release = %v", got[2])
	}
}

func TestOnlyValidInputExtendsIdleTimeout(t *testing.T) {
	now := time.Now()
	p := pointer{
		width:   800,
		height:  480,
		last:    now.Add(-idleTimeout),
		deliver: func(touch.Contact) {},
	}
	if !p.idle(now) {
		t.Fatal("idle controller not expired")
	}
	p.receive([]byte(`{"phase":"move","x":0,"y":0}`))
	if !p.idle(now) {
		t.Fatal("invalid input extended control")
	}
	p.receive([]byte(`{"phase":"down","x":0,"y":0}`))
	if p.idle(time.Now()) {
		t.Fatal("valid input did not extend control")
	}
	p.release()
	p.receive([]byte(`{"phase":"down","x":0,"y":0}`))
	if p.contact != nil {
		t.Fatal("closed controller accepted input")
	}
}

func TestRejectMalformedPointer(t *testing.T) {
	for _, message := range []string{
		`{`, `null`, `{}`, `{"phase":"down"}`,
		`{"phase":"down","x":-0.1,"y":0}`,
		`{"phase":"down","x":0,"y":1.1}`,
		`{"phase":"down","x":null,"y":0}`,
		`{"phase":"down","x":1e1000,"y":0}`,
		`{"phase":"button","x":0,"y":0}`,
	} {
		t.Run(message, func(t *testing.T) {
			p := pointer{
				width:   800,
				height:  480,
				deliver: func(c touch.Contact) { t.Errorf("delivered %v", c) },
			}
			p.receive([]byte(message))
			p.release()
		})
	}
}
