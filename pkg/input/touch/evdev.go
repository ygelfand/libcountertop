package touch

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"syscall"
	"time"
)

// Multi-touch protocol B: a slot holds a tracking id until it goes to -1.
// Protocol A: each contact is a block ended by SYN_MT_REPORT, with no slots or lasting ids.
const (
	evSyn = 0x00
	evKey = 0x01
	evAbs = 0x03

	synReport   = 0x00
	synMTReport = 0x02

	btnTouch = 0x14a

	absMTSlot       = 0x2f
	absMTPositionX  = 0x35
	absMTPositionY  = 0x36
	absMTTrackingID = 0x39
)

const released = -1

// struct input_event.
type rawEvent struct {
	Time  syscall.Timeval
	Type  uint16
	Code  uint16
	Value int32
}

var eventSize = binary.Size(rawEvent{})

func read(r io.Reader, handle func(rawEvent)) error {
	br := bufio.NewReaderSize(r, eventSize*64)

	for {
		var e rawEvent
		if err := binary.Read(br, binary.LittleEndian, &e); err != nil {
			if errors.Is(err, os.ErrClosed) {
				return nil
			}
			return fmt.Errorf("touch: reading events: %w", err)
		}
		handle(e)
	}
}

type slot struct {
	id      int
	x, y    int
	touched bool
	changed bool

	lifting bool
}

type point struct{ x, y int }

type held struct {
	slot, id int
	at       point
}

type decoder struct {
	current int
	slots   map[int]*slot

	block  *point
	frame  []point
	held   []held
	nextID int

	place func(x, y int) (int, int)
}

func (d *decoder) at(n int) *slot {
	if d.slots == nil {
		d.slots = map[int]*slot{}
	}
	if d.slots[n] == nil {
		d.slots[n] = &slot{id: released}
	}
	return d.slots[n]
}

func (d *decoder) event(e rawEvent, now time.Time) []Contact {
	switch e.Type {
	case evAbs:
		d.abs(e.Code, int(e.Value))
	case evKey:
		if e.Code == btnTouch && e.Value == 0 && d.frame == nil && len(d.held) > 0 {
			d.frame = []point{}
		}
	case evSyn:
		switch e.Code {
		case synMTReport:
			if d.frame == nil {
				d.frame = []point{}
			}
			if d.block != nil {
				d.frame = append(d.frame, *d.block)
				d.block = nil
			}
		case synReport:
			if d.frame != nil {
				frame := d.frame
				d.frame = nil
				return d.reportFrame(frame, now)
			}
			d.block = nil
			return d.report(now)
		}
	}
	return nil
}

func (d *decoder) pending() *point {
	if d.block == nil {
		d.block = &point{}
	}
	return d.block
}

func (d *decoder) reportFrame(frame []point, now time.Time) []Contact {
	d.slots = nil
	var out []Contact
	contact := func(h held, phase Phase) Contact {
		x, y := h.at.x, h.at.y
		if d.place != nil {
			x, y = d.place(x, y)
		}
		return Contact{Slot: h.slot, ID: h.id, X: x, Y: y, Phase: phase, At: now}
	}

	type pair struct{ held, point, distance int }
	var pairs []pair
	for h := range d.held {
		for p := range frame {
			pairs = append(pairs, pair{h, p, distance(d.held[h].at, frame[p])})
		}
	}
	slices.SortStableFunc(pairs, func(a, b pair) int { return a.distance - b.distance })

	matched := make([]int, len(d.held))
	for i := range matched {
		matched[i] = -1
	}
	taken := make([]bool, len(frame))
	for _, p := range pairs {
		if matched[p.held] < 0 && !taken[p.point] {
			matched[p.held], taken[p.point] = p.point, true
		}
	}

	kept := d.held[:0:0]
	for i, h := range d.held {
		if matched[i] < 0 {
			out = append(out, contact(h, Up))
			continue
		}
		if p := frame[matched[i]]; p != h.at {
			h.at = p
			out = append(out, contact(h, Move))
		}
		kept = append(kept, h)
	}

	for i, p := range frame {
		if taken[i] {
			continue
		}
		h := held{slot: freeSlot(kept), id: d.nextID, at: p}
		d.nextID++
		kept = append(kept, h)
		out = append(out, contact(h, Down))
	}
	d.held = kept
	return out
}

func distance(a, b point) int {
	dx, dy := a.x-b.x, a.y-b.y
	return dx*dx + dy*dy
}

func freeSlot(contacts []held) int {
	for n := 0; ; n++ {
		if !slices.ContainsFunc(contacts, func(h held) bool { return h.slot == n }) {
			return n
		}
	}
}

func (d *decoder) abs(code uint16, value int) {
	switch code {
	case absMTSlot:
		d.current = value
	case absMTTrackingID:
		s := d.at(d.current)
		s.changed = true
		if value == released {
			s.lifting = true
		} else {
			s.id, s.lifting = value, false
			s.touched = false
		}
	case absMTPositionX:
		s := d.at(d.current)
		s.x, s.changed = value, true
		d.pending().x = value
	case absMTPositionY:
		s := d.at(d.current)
		s.y, s.changed = value, true
		d.pending().y = value
	}
}

func (d *decoder) report(now time.Time) []Contact {
	var out []Contact

	for n, s := range d.slots {
		if !s.changed {
			continue
		}
		s.changed = false

		x, y := s.x, s.y
		if d.place != nil {
			x, y = d.place(x, y)
		}

		switch {
		case s.lifting:
			if s.touched {
				out = append(out, Contact{Slot: n, ID: s.id, X: x, Y: y, Phase: Up, At: now})
			}
			s.id, s.touched, s.lifting = released, false, false
		case s.id == released:
		case !s.touched:
			s.touched = true
			out = append(out, Contact{Slot: n, ID: s.id, X: x, Y: y, Phase: Down, At: now})
		default:
			out = append(out, Contact{Slot: n, ID: s.id, X: x, Y: y, Phase: Move, At: now})
		}
	}
	return out
}
