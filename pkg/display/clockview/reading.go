package clockview

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Reading struct {
	Time   string
	Suffix string
	Date   string

	Second int
}

func Read(at time.Time, twentyFour bool) Reading {
	r := Reading{Date: at.Format("Monday, 2 January"), Second: at.Second()}

	if twentyFour {
		r.Time = at.Format("15:04")
		return r
	}

	r.Time = at.Format("3:04")
	r.Suffix = at.Format("PM")
	return r
}

func (r Reading) String() string { return fmt.Sprintf("%s%s, %s", r.Time, r.Suffix, r.Date) }

func (r Reading) Undated() Reading { r.Date = ""; return r }

func (r Reading) Dated() bool { return r.Date != "" }

func split(r Reading) (hour, minute string, ok bool) {
	hour, minute, ok = strings.Cut(r.Time, ":")
	if !ok {
		return "", "", false
	}
	if len(hour) == 1 {
		hour = "0" + hour
	}
	return hour, minute, true
}

func clockHands(r Reading) (hour, minute int, ok bool) {
	h, m, cut := strings.Cut(r.Time, ":")
	if !cut {
		return 0, 0, false
	}

	hour, err := strconv.Atoi(h)
	if err != nil {
		return 0, 0, false
	}

	minute, err = strconv.Atoi(m)
	if err != nil {
		return 0, 0, false
	}
	return hour, minute, true
}
