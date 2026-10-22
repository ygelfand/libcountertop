package firmwarerelay

import (
	"strconv"
	"strings"

	"github.com/ygelfand/go-esphome-device/api"
)

const defaultChange = 2 * 3600

func parseZone(spec string) (*api.ParsedTimezone, bool) {
	p := &posix{s: spec}
	if !p.name() {
		return nil, false
	}
	std, ok := p.offset()
	if !ok {
		return nil, false
	}
	tz := &api.ParsedTimezone{StdOffsetSeconds: std, DstOffsetSeconds: std}
	if p.done() {
		return tz, true
	}
	if !p.name() {
		return nil, false
	}
	tz.DstOffsetSeconds = std - 3600
	if !p.done() && p.peek() != ',' {
		dst, ok := p.offset()
		if !ok {
			return nil, false
		}
		tz.DstOffsetSeconds = dst
	}
	if p.done() {
		return tz, true
	}
	if !p.take(',') {
		return nil, false
	}
	if tz.DstStart, ok = p.rule(); !ok || !p.take(',') {
		return nil, false
	}
	if tz.DstEnd, ok = p.rule(); !ok || !p.done() {
		return nil, false
	}
	return tz, true
}

type posix struct {
	s string
	i int
}

func (p *posix) done() bool { return p.i >= len(p.s) }

func (p *posix) peek() byte {
	if p.done() {
		return 0
	}
	return p.s[p.i]
}

func (p *posix) take(c byte) bool {
	if p.peek() != c {
		return false
	}
	p.i++
	return true
}

func (p *posix) name() bool {
	if p.take('<') {
		end := strings.IndexByte(p.s[p.i:], '>')
		if end < 1 {
			return false
		}
		p.i += end + 1
		return true
	}
	start := p.i
	for !p.done() && (p.peek() >= 'A' && p.peek() <= 'Z' || p.peek() >= 'a' && p.peek() <= 'z') {
		p.i++
	}
	return p.i-start >= 3
}

func (p *posix) number() (int, bool) {
	start := p.i
	for !p.done() && p.peek() >= '0' && p.peek() <= '9' {
		p.i++
	}
	n, err := strconv.Atoi(p.s[start:p.i])
	return n, err == nil
}

func (p *posix) clock() (int32, bool) {
	sign := int32(1)
	if p.take('-') {
		sign = -1
	} else {
		p.take('+')
	}
	h, ok := p.number()
	if !ok {
		return 0, false
	}
	seconds := h * 3600
	for _, unit := range []int{60, 1} {
		if !p.take(':') {
			break
		}
		n, ok := p.number()
		if !ok {
			return 0, false
		}
		seconds += n * unit
	}
	return sign * int32(seconds), true
}

func (p *posix) offset() (int32, bool) { return p.clock() }

func (p *posix) rule() (*api.DSTRule, bool) {
	r := &api.DSTRule{TimeSeconds: defaultChange}
	switch {
	case p.take('M'):
		r.Type = api.DSTRuleType_DST_RULE_TYPE_MONTH_WEEK_DAY
		month, ok := p.number()
		if !ok || !p.take('.') {
			return nil, false
		}
		week, ok := p.number()
		if !ok || !p.take('.') {
			return nil, false
		}
		day, ok := p.number()
		if !ok {
			return nil, false
		}
		r.Month, r.Week, r.DayOfWeek = uint32(month), uint32(week), uint32(day)
	case p.take('J'):
		r.Type = api.DSTRuleType_DST_RULE_TYPE_JULIAN_NO_LEAP
		day, ok := p.number()
		if !ok {
			return nil, false
		}
		r.Day = uint32(day)
	default:
		r.Type = api.DSTRuleType_DST_RULE_TYPE_DAY_OF_YEAR
		day, ok := p.number()
		if !ok {
			return nil, false
		}
		r.Day = uint32(day)
	}
	if p.take('/') {
		t, ok := p.clock()
		if !ok {
			return nil, false
		}
		r.TimeSeconds = t
	}
	return r, true
}
