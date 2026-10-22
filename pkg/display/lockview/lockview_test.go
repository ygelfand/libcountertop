package lockview

import (
	"errors"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type stored struct{ at schema.Lock }

func (s *stored) read() schema.Lock         { return s.at }
func (s *stored) save(at schema.Lock) error { s.at = at; return nil }

func gate(pin string, now *time.Time) (*Gate, *stored, *shell.Shell) {
	s := &stored{at: schema.Lock{PIN: pin}}
	sh := shell.New()
	return New(Options{
		Shell:  sh,
		Read:   s.read,
		Save:   s.save,
		SetPIN: func(pin string) error { s.at = schema.Lock{PIN: pin}; return nil },
		Now:    func() time.Time { return *now },
	}), s, sh
}

func TestANewPinIsTypedTwice(t *testing.T) {
	now := time.Unix(0, 0)
	g, s, sh := gate("", &now)
	g.Change()
	if _, ok := sh.Top().(*Setter); !ok {
		t.Fatalf("top is %T, want the setter", sh.Top())
	}

	g.Choose("123")
	g.Confirm("123")
	if s.at.PIN != "" {
		t.Fatalf("a 3-digit pin was set: %q", s.at.PIN)
	}

	g.Choose("2468")
	g.Confirm("2469")
	if s.at.PIN != "" || g.note != "lock.mismatch" {
		t.Fatalf("pin=%q note=%q after a mismatch", s.at.PIN, g.note)
	}

	g.Choose("2468")
	g.Confirm("2468")
	if s.at.PIN != "2468" {
		t.Errorf("pin = %q, want 2468", s.at.PIN)
	}
	if _, ok := sh.Top().(*Setter); ok {
		t.Error("the setter stayed after the pin was set")
	}
	if !g.Locked() {
		t.Error("not locked after setting a pin")
	}
	g.Clear()
	if g.Locked() {
		t.Error("still locked after clearing")
	}
}

func TestNoPinOpensStraightAway(t *testing.T) {
	now := time.Unix(0, 0)
	g, _, sh := gate("", &now)
	opened := false
	g.Open(func() { opened = true })
	if !opened || sh.Top() != nil {
		t.Errorf("opened=%v top=%T, want opened with nothing pushed", opened, sh.Top())
	}
}

func TestTheRightPinOpens(t *testing.T) {
	now := time.Unix(0, 0)
	g, _, sh := gate("1234", &now)
	opened := false
	g.Open(func() { opened = true })
	if opened {
		t.Fatal("opened before the pin")
	}
	if _, ok := sh.Top().(*Prompt); !ok {
		t.Fatalf("top is %T, want the prompt", sh.Top())
	}
	g.Enter("1234")
	if !opened {
		t.Error("the right pin did not open")
	}
	if _, ok := sh.Top().(*Prompt); ok {
		t.Error("the prompt stayed after the right pin")
	}
}

func TestFiveWrongPinsStartTheBackoff(t *testing.T) {
	now := time.Unix(0, 0)
	g, s, _ := gate("1234", &now)
	opened := false
	g.Open(func() { opened = true })

	for range freeTries - 1 {
		g.Enter("0000")
	}
	if s.at.Until.After(now) {
		t.Fatalf("locked after %d tries", freeTries-1)
	}
	g.Enter("0000")
	if want := now.Add(firstWait); !s.at.Until.Equal(want) {
		t.Fatalf("locked until %v, want %v", s.at.Until, want)
	}

	g.Enter("1234")
	if opened {
		t.Fatal("the right pin opened while locked")
	}

	now = now.Add(firstWait)
	g.Enter("0000")
	if want := now.Add(2 * firstWait); !s.at.Until.Equal(want) {
		t.Errorf("after the sixth failure locked until %v, want %v", s.at.Until, want)
	}

	now = now.Add(2 * firstWait)
	g.Enter("1234")
	if !opened {
		t.Fatal("the right pin did not open after the wait")
	}
	if s.at.Failures != 0 || !s.at.Until.IsZero() {
		t.Errorf("lock = %+v, want the failures cleared", s.at)
	}
}

func TestTheWaitDoublesUpToAnHour(t *testing.T) {
	for _, c := range []struct {
		failures int
		want     time.Duration
	}{
		{0, 0},
		{4, 0},
		{5, 30 * time.Second},
		{6, time.Minute},
		{7, 2 * time.Minute},
		{12, longestWait},
		{100, longestWait},
	} {
		if got := Wait(c.failures); got != c.want {
			t.Errorf("Wait(%d) = %v, want %v", c.failures, got, c.want)
		}
	}
}

func TestThePinTakesFourToEightDigitsOrNothing(t *testing.T) {
	row, ok := NewTable().Find("lock_pin")
	if !ok {
		t.Fatal("no pin row")
	}
	if !row.Portable() {
		t.Error("the pin does not sync to other devices")
	}
	for _, c := range []struct {
		in   string
		want error
	}{
		{"1234", nil},
		{"12345678", nil},
		{"", nil},
		{"123", ErrShape},
		{"123456789", ErrShape},
		{"12a4", ErrShape},
	} {
		at := schema.Lock{PIN: "9999", Failures: 7, Until: time.Unix(99, 0)}
		err := row.Write(&at, c.in)
		if !errors.Is(err, c.want) {
			t.Errorf("Write(%q) = %v, want %v", c.in, err, c.want)
			continue
		}
		if err == nil && (at.PIN != c.in || at.Failures != 0 || !at.Until.IsZero()) {
			t.Errorf("Write(%q) left %+v, want the pin set and the failures cleared", c.in, at)
		}
	}
}
