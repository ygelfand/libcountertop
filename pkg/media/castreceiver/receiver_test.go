package castreceiver

import (
	"context"
	"slices"
	"sync"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type memory struct {
	mu sync.Mutex
	c  schema.Cast
}

func (m *memory) Read() schema.Cast {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.c
}

func (m *memory) Update(change func(*schema.Cast)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	change(&m.c)
	return nil
}

type silent struct{}

func (silent) Level() int                     { return 0 }
func (silent) SetLevel(int)                   {}
func (silent) Mute(bool)                      {}
func (silent) Muted() bool                    { return false }
func (silent) Listen(func(int)) (stop func()) { return func() {} }

func fresh(t *testing.T) (*Receiver, *memory) {
	t.Helper()

	m := &memory{c: schema.DefaultCast()}
	r := NewReceiver(ReceiverOptions{
		Settings: m,
		Network:  func(context.Context) bool { return false },
		Server:   Config{Volume: silent{}},
	})
	return r, m
}

func TestTheReceiverIsOffUntilAsked(t *testing.T) {
	r, _ := fresh(t)

	if r.Enabled() {
		t.Error("a device nobody has configured is already casting")
	}
	if schema.DefaultCast().Receiver {
		t.Error("the default is on")
	}
}

func TestStartingWhileOffDoesNothing(t *testing.T) {
	r, _ := fresh(t)

	if err := r.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if r.Listening() {
		t.Error("a socket was opened")
	}
	if r.Playing() != nil {
		t.Error("something is playing")
	}
}

func TestClosingWithoutStartingIsSafe(t *testing.T) {
	r, _ := fresh(t)

	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("closing twice: %v", err)
	}
}

func TestTheSwitchFollowsTheSetting(t *testing.T) {
	r, m := fresh(t)

	if r.knobs.Entity("receiver").(*esphome.Switch).Get() {
		t.Error("the switch reads on for a receiver that is off")
	}

	r.SetReceiver(false)

	if m.Read().Receiver {
		t.Error("the setting says on")
	}
	if r.knobs.Entity("receiver").(*esphome.Switch).Get() {
		t.Error("the switch says on")
	}
	if r.Listening() {
		t.Error("turning it off opened a socket")
	}
}

func TestTheSwitchIsPublished(t *testing.T) {
	r, _ := fresh(t)

	var ids []string
	for _, e := range r.Entities() {
		ids = append(ids, e.Object())
	}
	want := []string{
		"cast_receiver",
		"youtube_sponsorblock",
		"cast_youtube_lounge",
		"cast_oracle",
		"cast_bundle",
		"cast_credentials",
		"cast_credentials_expire",
	}
	if !slices.Equal(ids, want) {
		t.Errorf("the entities are %v, want %v", ids, want)
	}
}

func TestTheEntitiesBelongToTheGivenDevice(t *testing.T) {
	r := NewReceiver(ReceiverOptions{
		Settings: &memory{c: schema.DefaultCast()},
		DeviceID: 7,
		Server:   Config{Volume: silent{}},
	})
	if got := r.knobs.Entity("receiver").(*esphome.Switch).DeviceID; got != 7 {
		t.Errorf("device %d, want 7", got)
	}
}

func TestTheComponentIsNamed(t *testing.T) {
	r, _ := fresh(t)
	if r.Name() == "" {
		t.Error("the component has no name, so nothing in a log says which one it is")
	}
}

func TestPreferencesReadTheCastSettings(t *testing.T) {
	m := &memory{c: schema.DefaultCast()}
	m.c.YouTube.OnDemand = true
	p := Preferences("product", m)

	if !p.Settings().YouTube.OnDemand {
		t.Error("the lounge mode was not read")
	}
	if err := p.SaveScreens("d", "m", "v"); err != nil {
		t.Fatal(err)
	}
	if got := m.Read().YouTube; got.Device != "d" || got.Music != "m" || got.Video != "v" {
		t.Errorf("screens saved as %+v", got)
	}
	if p.RequestIDPrefix() != "product" {
		t.Error(p.RequestIDPrefix())
	}
}
