package cast

import (
	"context"
	"testing"
)

type owner struct {
	got []Message
}

func (*owner) Name() string         { return "engine" }
func (*owner) Namespaces() []string { return nil }
func (*owner) Run(context.Context)  {}
func (*owner) Owns(a App) bool      { return a.ID != "C35B0678" }
func (o *owner) Receive(_ *Application, m Message) ([]Message, error) {
	o.got = append(o.got, m)
	return nil, nil
}

func TestAnOwnerTakesTheWholeTransportOfAnAppWithoutAProtocol(t *testing.T) {
	o := &owner{}
	r := NewReceiver("Kitchen")
	r.Register(o)

	out := answered(
		t,
		r,
		from(SenderID, NSReceiver, `{"type":"LAUNCH","requestId":1,"appId":"9AC194DC"}`),
	)
	transport := status(t, out.Payload).Applications[0].TransportID

	for _, m := range []Message{
		toApp(SenderID, transport, NSConnection, `{"type":"CONNECT"}`),
		toApp(SenderID, transport, NSMedia, `{"type":"LOAD","requestId":2,"media":{"contentId":"x"}}`),
		toApp(SenderID, transport, "urn:x-cast:com.plexapp.plexcast", `{"type":"HELLO"}`),
	} {
		if _, err := r.Receive(m); err != nil {
			t.Fatal(err)
		}
	}
	if len(o.got) != 3 {
		t.Fatalf("the owner got %d of the three messages on the app's transport", len(o.got))
	}
	if !r.senders[SenderID] {
		t.Error("a sender connecting to an owned app is not known to the receiver")
	}
	if r.Media() != nil {
		t.Error("the receiver loaded media for an app it does not own")
	}

	sent := r.Advertise("9AC194DC", "Plex", []string{NSMedia, "urn:x-cast:com.plexapp.plexcast"})
	if len(sent) != 1 {
		t.Fatalf("advertising sent %d messages, want one status", len(sent))
	}
	app := status(t, sent[0].Payload).Applications[0]
	if app.DisplayName != "Plex" || len(app.Namespaces) != 2 ||
		app.Namespaces[1].Name != "urn:x-cast:com.plexapp.plexcast" {
		t.Errorf("after advertising, the app is %+v", app)
	}
}

func TestAnAppTheOwnerTurnsDownStaysWithTheReceiver(t *testing.T) {
	o := &owner{}
	r := NewReceiver("Kitchen")
	r.Register(o)
	r.Player = &player{}

	out := answered(
		t,
		r,
		from(SenderID, NSReceiver, `{"type":"LAUNCH","requestId":1,"appId":"C35B0678"}`),
	)
	transport := status(t, out.Payload).Applications[0].TransportID
	if _, err := r.Receive(toApp(SenderID, transport, NSMedia,
		`{"type":"LOAD","requestId":2,"media":{"contentId":"http://x/a.mp3"}}`)); err != nil {
		t.Fatal(err)
	}
	if len(o.got) != 0 || r.Media() == nil {
		t.Errorf("the owner got %d messages and the receiver loaded %v", len(o.got), r.Media())
	}
}
