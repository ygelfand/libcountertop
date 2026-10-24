package touch

import (
	"runtime"
	"testing"
	"time"
)

func ev(t uint16, code uint16, value int32) rawEvent {
	return rawEvent{Type: t, Code: code, Value: value}
}

func syn() rawEvent { return rawEvent{Type: evSyn, Code: synReport} }

func feed(d *decoder, events ...rawEvent) []Contact {
	var got []Contact
	now := time.Now()
	for _, e := range events {
		got = append(got, d.event(e, now)...)
	}
	return got
}

func placed() decoder {
	return decoder{place: New("", landscape()).place}
}

func TestNothingIsReportedUntilSyn(t *testing.T) {
	d := placed()

	got := feed(&d,
		ev(evAbs, absMTTrackingID, 7),
		ev(evAbs, absMTPositionX, 600),
		ev(evAbs, absMTPositionY, 960),
	)
	if len(got) != 0 {
		t.Fatalf("reported %v before SYN_REPORT", got)
	}

	got = feed(&d, syn())
	if len(got) != 1 || got[0].Phase != Down {
		t.Fatalf("got %v, want one down", got)
	}
}

func TestTapIsDownThenUp(t *testing.T) {
	d := placed()

	down := feed(&d,
		ev(evAbs, absMTTrackingID, 7),
		ev(evAbs, absMTPositionX, 600),
		ev(evAbs, absMTPositionY, 960),
		syn(),
	)
	if len(down) != 1 {
		t.Fatalf("got %v, want one contact", down)
	}
	if down[0].Phase != Down || down[0].ID != 7 || down[0].Slot != 0 {
		t.Errorf("got %v, want a down in slot 0 with id 7", down[0])
	}
	if down[0].X != 959 || down[0].Y != 600 {
		t.Errorf("down at %d,%d, want 959,600", down[0].X, down[0].Y)
	}

	up := feed(&d, ev(evAbs, absMTTrackingID, released), syn())
	if len(up) != 1 || up[0].Phase != Up {
		t.Fatalf("got %v, want one up", up)
	}
	if up[0].ID != 7 {
		t.Errorf("up carries id %d, want the 7 it went down with", up[0].ID)
	}
	if up[0].X != 959 || up[0].Y != 600 {
		t.Errorf("up at %d,%d, want 959,600", up[0].X, up[0].Y)
	}
}

func TestDragReportsMoves(t *testing.T) {
	var d decoder

	feed(&d,
		ev(evAbs, absMTSlot, 0), ev(evAbs, absMTTrackingID, 7),
		ev(evAbs, absMTPositionX, 100), ev(evAbs, absMTPositionY, 200), syn())

	got := feed(&d, ev(evAbs, absMTPositionX, 140), syn())
	if len(got) != 1 || got[0].Phase != Move {
		t.Fatalf("got %v, want one move", got)
	}
	if got[0].X != 140 || got[0].Y != 200 {
		t.Errorf("move at %d,%d, want 140,200", got[0].X, got[0].Y)
	}
}

func TestAnIdleReportSaysNothing(t *testing.T) {
	var d decoder

	feed(&d, ev(evAbs, absMTSlot, 0), ev(evAbs, absMTTrackingID, 1),
		ev(evAbs, absMTPositionX, 10), syn())
	if got := feed(&d, syn()); len(got) != 0 {
		t.Errorf("an idle report produced %v", got)
	}
}

func TestALiftWithoutADownIsSilent(t *testing.T) {
	var d decoder

	got := feed(&d, ev(evAbs, absMTSlot, 0), ev(evAbs, absMTTrackingID, released), syn())
	if len(got) != 0 {
		t.Errorf("a lift with no down produced %v", got)
	}
}

func TestSlotsAreIndependent(t *testing.T) {
	var d decoder

	got := feed(&d,
		ev(evAbs, absMTSlot, 0),
		ev(evAbs, absMTTrackingID, 1),
		ev(evAbs, absMTPositionX, 10),
		ev(evAbs, absMTPositionY, 11),
		ev(evAbs, absMTSlot, 1),
		ev(evAbs, absMTTrackingID, 2),
		ev(evAbs, absMTPositionX, 90),
		ev(evAbs, absMTPositionY, 91),
		syn(),
	)
	if len(got) != 2 {
		t.Fatalf("got %v, want two contacts", got)
	}

	byID := map[int]Contact{}
	for _, c := range got {
		byID[c.ID] = c
	}
	if c := byID[1]; c.X != 10 || c.Y != 11 || c.Slot != 0 {
		t.Errorf("id 1 is %v, want slot 0 at 10,11", c)
	}
	if c := byID[2]; c.X != 90 || c.Y != 91 || c.Slot != 1 {
		t.Errorf("id 2 is %v, want slot 1 at 90,91", c)
	}

	got = feed(&d, ev(evAbs, absMTSlot, 0), ev(evAbs, absMTTrackingID, released), syn())
	if len(got) != 1 || got[0].Slot != 0 || got[0].Phase != Up || got[0].ID != 1 {
		t.Fatalf("got %v, want only id 1 in slot 0 lifting", got)
	}

	got = feed(&d, ev(evAbs, absMTSlot, 1), ev(evAbs, absMTPositionX, 95), syn())
	if len(got) != 1 || got[0].Phase != Move || got[0].ID != 2 {
		t.Fatalf("got %v, want id 2 still moving", got)
	}
}

func TestReusedSlotIsANewDown(t *testing.T) {
	var d decoder

	feed(&d,
		ev(evAbs, absMTTrackingID, 1),
		ev(evAbs, absMTPositionX, 10),
		ev(evAbs, absMTPositionY, 10),
		syn(),
	)
	feed(&d, ev(evAbs, absMTTrackingID, released), syn())

	got := feed(&d,
		ev(evAbs, absMTTrackingID, 2),
		ev(evAbs, absMTPositionX, 20),
		ev(evAbs, absMTPositionY, 20),
		syn(),
	)
	if len(got) != 1 || got[0].Phase != Down || got[0].ID != 2 {
		t.Fatalf("got %v, want a down with id 2", got)
	}
}

func TestContactsArePlaced(t *testing.T) {
	d := decoder{place: func(x, y int) (int, int) { return y, x }}

	got := feed(&d, ev(evAbs, absMTSlot, 0), ev(evAbs, absMTTrackingID, 1),
		ev(evAbs, absMTPositionX, 3), ev(evAbs, absMTPositionY, 400), syn())
	if len(got) != 1 {
		t.Fatalf("got %d contacts, want 1", len(got))
	}
	if got[0].X != 400 || got[0].Y != 3 {
		t.Errorf("reported %d,%d, want the placed 400,3", got[0].X, got[0].Y)
	}
}

func mtReport() rawEvent { return rawEvent{Type: evSyn, Code: synMTReport} }

func touching(x, y int32) []rawEvent {
	return []rawEvent{
		ev(evAbs, absMTTrackingID, 0),
		ev(evAbs, absMTPositionX, x),
		ev(evAbs, absMTPositionY, y),
		mtReport(),
	}
}

func frame(blocks ...[]rawEvent) []rawEvent {
	var out []rawEvent
	for _, b := range blocks {
		out = append(out, b...)
	}
	return append(out, syn())
}

func lifted() []rawEvent { return []rawEvent{ev(evKey, btnTouch, 0), mtReport(), syn()} }

// Captured from an ivy whose touch reports through mtk-tpd.
func TestProtocolATapIsDownThenUp(t *testing.T) {
	var d decoder

	down := feed(
		&d,
		append([]rawEvent{ev(evKey, btnTouch, 1)}, frame(touching(0x248, 0x16b))...)...)
	if len(down) != 1 || down[0].Phase != Down || down[0].X != 0x248 || down[0].Y != 0x16b {
		t.Fatalf("got %v, want one down at 584,363", down)
	}

	for range 2 {
		if got := feed(&d, frame(touching(0x248, 0x16b))...); len(got) != 0 {
			t.Fatalf("a held finger that did not move produced %v", got)
		}
	}

	up := feed(&d, lifted()...)
	if len(up) != 1 || up[0].Phase != Up || up[0].ID != down[0].ID {
		t.Fatalf("got %v, want the finger that went down to lift", up)
	}
}

func TestProtocolAFollowsAMovingFinger(t *testing.T) {
	var d decoder

	feed(&d, frame(touching(100, 200))...)
	got := feed(&d, frame(touching(130, 200))...)
	if len(got) != 1 || got[0].Phase != Move || got[0].X != 130 {
		t.Fatalf("got %v, want one move to 130,200", got)
	}
}

func TestProtocolAMatchesFingersByPosition(t *testing.T) {
	var d decoder

	got := feed(&d, frame(touching(10, 10), touching(500, 900))...)
	if len(got) != 2 {
		t.Fatalf("got %v, want two downs", got)
	}
	ids := map[int]int{got[0].X: got[0].ID, got[1].X: got[1].ID}

	got = feed(&d, frame(touching(505, 905))...)
	if len(got) != 2 {
		t.Fatalf("got %v, want one move and one lift", got)
	}
	for _, c := range got {
		switch c.Phase {
		case Move:
			if c.ID != ids[500] {
				t.Errorf("the far finger moved as id %d, want %d", c.ID, ids[500])
			}
		case Up:
			if c.ID != ids[10] {
				t.Errorf("the near finger lifted as id %d, want %d", c.ID, ids[10])
			}
		default:
			t.Errorf("unexpected %v", c)
		}
	}
}

func TestProtocolANewTouchesGetNewIDs(t *testing.T) {
	var d decoder

	first := feed(&d, frame(touching(10, 10))...)
	feed(&d, lifted()...)
	second := feed(&d, frame(touching(10, 10))...)
	if len(second) != 1 || second[0].Phase != Down || second[0].ID == first[0].ID {
		t.Fatalf("got %v after %v, want a down with a new id", second, first)
	}
}

func TestBTNTouchDoesNotChangeProtocolB(t *testing.T) {
	var d decoder

	feed(&d, ev(evKey, btnTouch, 1), ev(evAbs, absMTTrackingID, 3),
		ev(evAbs, absMTPositionX, 10), ev(evAbs, absMTPositionY, 20), syn())
	got := feed(&d, ev(evAbs, absMTTrackingID, released), ev(evKey, btnTouch, 0), syn())
	if len(got) != 1 || got[0].Phase != Up || got[0].ID != 3 {
		t.Fatalf("got %v, want id 3 to lift", got)
	}
}

func TestEventSizeMatchesTheKernelStruct(t *testing.T) {
	want := map[string]int{"arm": 16, "386": 16, "amd64": 24, "arm64": 24}[runtime.GOARCH]
	if want == 0 {
		t.Skipf("no expected size for %s", runtime.GOARCH)
	}
	if eventSize != want {
		t.Errorf("eventSize = %d on %s, want %d", eventSize, runtime.GOARCH, want)
	}
}

func gestures(d *decoder, r *Recognizer, events ...rawEvent) []Gesture {
	var got []Gesture
	for _, c := range feed(d, events...) {
		if g, ok := r.Feed(c); ok {
			got = append(got, g)
		}
	}
	return got
}

func TestASwipeFromEventsToAGesture(t *testing.T) {
	d := placed()
	r := NewRecognizer(panelH, panelW)

	var got []Gesture
	got = append(got, gestures(&d, r,
		ev(evAbs, absMTTrackingID, 4),
		ev(evAbs, absMTPositionX, 600),
		ev(evAbs, absMTPositionY, 1900),
		syn(),
	)...)
	got = append(got, gestures(&d, r, ev(evAbs, absMTPositionY, 1500), syn())...)
	got = append(got, gestures(&d, r, ev(evAbs, absMTPositionY, 1100), syn())...)
	got = append(got, gestures(&d, r, ev(evAbs, absMTTrackingID, released), syn())...)

	if len(got) != 1 {
		t.Fatalf("a swipe across the glass produced %d gestures, want 1", len(got))
	}
	if got[0].Kind != Swipe {
		t.Errorf("kind = %v, want a swipe", got[0].Kind)
	}
	if got[0].From != Left {
		t.Errorf("from = %v, want the left edge", got[0].From)
	}
	if got[0].Toward != Right {
		t.Errorf("toward = %v, want the right", got[0].Toward)
	}
}

func TestATapFromEventsToAGesture(t *testing.T) {
	d := placed()
	r := NewRecognizer(panelH, panelW)

	got := gestures(&d, r,
		ev(evAbs, absMTTrackingID, 9),
		ev(evAbs, absMTPositionX, 600),
		ev(evAbs, absMTPositionY, 960),
		syn(),
		ev(evAbs, absMTTrackingID, released),
		syn(),
	)

	if len(got) != 1 {
		t.Fatalf("a tap on the glass produced %d gestures, want 1", len(got))
	}
	if got[0].Kind != Tap {
		t.Errorf("kind = %v, want a tap", got[0].Kind)
	}
}

func TestTwoFingersEachGetTheirOwnGesture(t *testing.T) {
	d := placed()
	r := NewRecognizer(panelH, panelW)

	got := gestures(&d, r,
		ev(evAbs, absMTSlot, 0), ev(evAbs, absMTTrackingID, 1),
		ev(evAbs, absMTPositionX, 300), ev(evAbs, absMTPositionY, 400),
		ev(evAbs, absMTSlot, 1), ev(evAbs, absMTTrackingID, 2),
		ev(evAbs, absMTPositionX, 900), ev(evAbs, absMTPositionY, 1400),
		syn(),
	)
	got = append(got, gestures(&d, r,
		ev(evAbs, absMTSlot, 0), ev(evAbs, absMTTrackingID, released), syn())...)
	got = append(got, gestures(&d, r,
		ev(evAbs, absMTSlot, 1), ev(evAbs, absMTTrackingID, released), syn())...)

	if len(got) != 2 {
		t.Fatalf("got %d gestures, want one per finger", len(got))
	}
}
