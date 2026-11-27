package timer

import (
	"fmt"
	"strings"
	"time"
)

type Card struct {
	Showing bool
	Ringing bool

	Name string

	Left time.Duration
	Of   time.Duration

	// All lists every timer the display knows, oldest first: running, paused, and finished
	// ones that are still ringing. The fields above still describe only the soonest.
	All []Entry
}

// Entry is one timer as the Split style draws it.
type Entry struct {
	ID     string
	Name   string
	Left   time.Duration
	Of     time.Duration
	Paused bool
	Done   bool
}

func (c Card) key() string {
	if !c.Showing && len(c.All) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%v|%s|%s", c.Ringing, c.Name, Remaining(c.Left))
	for _, e := range c.All {
		fmt.Fprintf(&b, "|%s,%s,%v,%v,%s", e.ID, e.Name, e.Paused, e.Done, Remaining(e.Left))
	}
	return b.String()
}

func (c Card) Share() float64 {
	if c.Of <= 0 {
		return 1
	}
	return min(max(c.Left.Seconds()/c.Of.Seconds(), 0), 1)
}

func Remaining(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	whole := int((d + time.Second - 1) / time.Second)

	h, m, s := whole/3600, whole/60%60, whole%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
