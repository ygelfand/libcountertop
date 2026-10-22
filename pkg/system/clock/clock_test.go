package clock

import (
	"errors"
	"net"
	"testing"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/timezone"
)

type moves struct {
	stepped []time.Time
	slewed  []time.Duration
	rtc     []string
	refuse  error
}

func watch(t *testing.T) *moves {
	t.Helper()
	m := &moves{}
	stepping = func(at time.Time) error { m.stepped = append(m.stepped, at); return nil }
	slewing = func(by time.Duration) error { m.slewed = append(m.slewed, by); return nil }
	writingRTC = func(path string, _ time.Time) error {
		m.rtc = append(m.rtc, path)
		return m.refuse
	}
	t.Cleanup(func() { stepping, slewing, writingRTC = setClock, slew, writeRTC })
	return m
}

func TestEachSyncKeepsTheHardwareClock(t *testing.T) {
	m := watch(t)
	c := newClock(Options{RTC: "/dev/rtc0"})

	_ = c.accept(time.Hour, precise, "test")
	_ = c.accept(time.Millisecond, precise, "test")

	if len(m.rtc) != 2 || m.rtc[0] != "/dev/rtc0" {
		t.Errorf("hardware clock written %v, want /dev/rtc0 after each sync", m.rtc)
	}
}

func TestARefusingHardwareClockIsLeftAlone(t *testing.T) {
	m := watch(t)
	m.refuse = errors.New("invalid argument")
	c := newClock(Options{RTC: "/dev/rtc0"})

	for range 3 {
		_ = c.accept(time.Hour, precise, "test")
	}

	if len(m.rtc) != 1 {
		t.Errorf("tried the hardware clock %d times, want once", len(m.rtc))
	}
}

func TestNoHardwareClockNamedIsNeverWritten(t *testing.T) {
	m := watch(t)
	c := newClock(Options{})

	_ = c.accept(time.Hour, precise, "test")

	if len(m.rtc) != 0 {
		t.Errorf("wrote a hardware clock nobody named: %v", m.rtc)
	}
}

func newClock(o Options) *Clock {
	o.Zone = timezone.PolicyOptions{Read: func() schema.Time { return schema.Time{} }}
	return New(o)
}

func TestHowAnOffsetIsApplied(t *testing.T) {
	tests := []struct {
		name   string
		offset time.Duration
		within time.Duration
		slews  bool
		steps  bool
	}{
		{"inside what the source can tell", 400 * time.Millisecond, time.Second, false, false},
		{"exactly at the source's resolution", time.Second, time.Second, false, false},
		{"a second and a half fast", -1500 * time.Millisecond, time.Second, true, false},
		{"a few milliseconds by ntp", 50 * time.Millisecond, precise, true, false},
		{"just under the jump", jump - time.Millisecond, precise, true, false},
		{"at the jump", jump, precise, false, true},
		{"a boot in 2010", 16 * 365 * 24 * time.Hour, time.Second, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := watch(t)
			c := newClock(Options{})

			var got []Sync
			c.Synced.Listen(func(s Sync) { got = append(got, s) })

			if err := c.accept(tt.offset, tt.within, "test"); err != nil {
				t.Fatalf("accept: %v", err)
			}
			if (len(m.slewed) == 1) != tt.slews {
				t.Errorf("slewed %v, want slewing %v", m.slewed, tt.slews)
			}
			if (len(m.stepped) == 1) != tt.steps {
				t.Errorf("stepped %v, want stepping %v", m.stepped, tt.steps)
			}
			if len(got) != 1 || got[0].Stepped() != tt.steps {
				t.Errorf("synced %v, want one sync that is a step: %v", got, tt.steps)
			}
			if !c.Valid() {
				t.Error("the time is not valid after a sync")
			}
		})
	}
}

func TestOnlyStepsReachStepWatchers(t *testing.T) {
	watch(t)
	c := newClock(Options{})

	var steps []time.Duration
	c.WatchSteps(func(by time.Duration) { steps = append(steps, by) })

	_ = c.accept(500*time.Millisecond, precise, "test")
	_ = c.accept(time.Hour, precise, "test")

	if len(steps) != 1 {
		t.Fatalf("%d steps reported, want 1: %v", len(steps), steps)
	}
	if steps[0] < 59*time.Minute || steps[0] > 61*time.Minute {
		t.Errorf("step reported as %v, want about an hour", steps[0])
	}
}

func TestValidBeforeAnySync(t *testing.T) {
	tests := []struct {
		name  string
		built string
		want  bool
	}{
		{"the clock is past the build", "2020-01-01T00:00:00Z", true},
		{"the clock is before the build", "2999-01-01T00:00:00Z", false},
		{"no build date", "unknown", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newClock(Options{Built: tt.built}).Valid(); got != tt.want {
				t.Errorf("Valid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHomeAssistantsNoAnswerIsIgnored(t *testing.T) {
	m := watch(t)
	c := newClock(Options{Built: "2999-01-01T00:00:00Z"})

	c.fromHome(0)

	if len(m.stepped)+len(m.slewed) != 0 || c.Valid() {
		t.Error("an empty answer moved the clock or made it valid")
	}
}

func TestHomeAssistantAgreeingLeavesTheClock(t *testing.T) {
	m := watch(t)
	c := newClock(Options{})

	c.fromHome(uint32(time.Now().Unix()))

	if len(m.stepped)+len(m.slewed) != 0 {
		t.Errorf("an answer within a second moved the clock: %v %v", m.stepped, m.slewed)
	}
}

func TestServers(t *testing.T) {
	if got := newClock(Options{}).servers(); len(got) != len(pool) || got[0] != pool[0] {
		t.Errorf("with no DHCP servers: %v, want the pool", got)
	}

	none := newClock(Options{Servers: func() []net.IP { return nil }})
	if got := none.servers(); len(got) != len(pool) {
		t.Errorf("with an empty lease: %v, want the pool", got)
	}

	offered := newClock(Options{Servers: func() []net.IP {
		return []net.IP{net.IPv4(192, 168, 1, 1), net.IPv4(10, 0, 0, 1)}
	}})
	got := offered.servers()
	if len(got) != 2 || got[0] != "192.168.1.1" || got[1] != "10.0.0.1" {
		t.Errorf("with a lease offering two: %v", got)
	}
}

func TestAskedOncePerConnection(t *testing.T) {
	c := newClock(Options{})
	first, second := new(esphome.Conn), new(esphome.Conn)

	if !c.shouldAsk(first) {
		t.Fatal("the first message on a connection did not ask the time")
	}
	if c.shouldAsk(first) {
		t.Fatal("a later message on the same connection asked again")
	}
	if !c.shouldAsk(second) {
		t.Fatal("a new connection did not ask the time")
	}
	if c.shouldAsk(nil) {
		t.Fatal("no connection asked the time")
	}
}

func TestZoneFallsBackToHome(t *testing.T) {
	saved := schema.Time{Home: "CET-1"}
	c := New(Options{Zone: timezone.PolicyOptions{Read: func() schema.Time { return saved }}})

	if got := c.Zone(); got != "CET-1" {
		t.Errorf("Zone() = %q with nothing chosen, want Home Assistant's", got)
	}
	saved.Chosen = "America/New_York"
	if got := c.Zone(); got != "America/New_York" {
		t.Errorf("Zone() = %q, want the chosen zone", got)
	}
}
