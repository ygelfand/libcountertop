package alarm

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

func spokenClock(text string) (int, int, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.NewReplacer("a.m.", "am", "p.m.", "pm", "a.m", "am", "p.m", "pm", "o'clock", "", "twenty-", "twenty ", "thirty-", "thirty ", "forty-", "forty ", "fifty-", "fifty ").
		Replace(text)
	text = strings.Join(strings.Fields(text), " ")
	switch text {
	case "midnight":
		return 0, 0, nil
	case "noon":
		return 12, 0, nil
	}
	period := ""
	for _, suffix := range []string{"am", "pm"} {
		if strings.HasSuffix(text, suffix) {
			period = suffix
			text = strings.TrimSpace(strings.TrimSuffix(text, suffix))
			break
		}
	}
	adjust := 0
	for prefix, minutes := range map[string]int{"quarter past ": 15, "half past ": 30, "quarter to ": -15} {
		if strings.HasPrefix(text, prefix) {
			adjust = minutes
			text = strings.TrimPrefix(text, prefix)
			break
		}
	}
	hour, minute := -1, 0
	if left, right, ok := strings.Cut(text, ":"); ok {
		hour, _ = clockNumber(left)
		minute, _ = clockNumber(right)
	} else {
		parts := strings.Fields(text)
		if len(parts) > 0 {
			hour, _ = clockNumber(parts[0])
		}
		if len(parts) > 1 {
			minute, _ = clockNumber(strings.Join(parts[1:], " "))
		}
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 ||
		(period != "" && (hour < 1 || hour > 12)) || (adjust != 0 && minute != 0) {
		return 0, 0, fmt.Errorf("say a time such as seven thirty AM or 19:30")
	}
	if period != "" {
		hour %= 12
		if period == "pm" {
			hour += 12
		}
	}
	minutes := (hour*60 + minute + adjust + 24*60) % (24 * 60)
	return minutes / 60, minutes % 60, nil
}

func clockNumber(text string) (int, bool) {
	if text == "" {
		return -1, false
	}
	if n, err := strconv.Atoi(text); err == nil && n >= 0 {
		return n, true
	}
	words := []string{
		"zero",
		"one",
		"two",
		"three",
		"four",
		"five",
		"six",
		"seven",
		"eight",
		"nine",
		"ten",
		"eleven",
		"twelve",
		"thirteen",
		"fourteen",
		"fifteen",
		"sixteen",
		"seventeen",
		"eighteen",
		"nineteen",
	}
	for n, word := range words {
		if text == word {
			return n, true
		}
	}
	for prefix, base := range map[string]int{"oh ": 0, "zero ": 0, "twenty": 20, "thirty": 30, "forty": 40, "fifty": 50} {
		if text == prefix {
			return base, true
		}
		if strings.HasPrefix(text, prefix+" ") || (base == 0 && strings.HasPrefix(text, prefix)) {
			tail := strings.TrimSpace(strings.TrimPrefix(text, prefix))
			for n, word := range words[:10] {
				if word == tail {
					return base + n, true
				}
			}
		}
	}
	return -1, false
}

func spokenSchedule(text, days string) (int, int, Weekdays, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	for _, sep := range []string{" every ", " on "} {
		if clock, repeat, ok := strings.Cut(text, sep); ok {
			if days != "" {
				return 0, 0, nil, fmt.Errorf("give one repeat schedule")
			}
			text, days = clock, repeat
			break
		}
	}
	switch strings.ToLower(strings.TrimSpace(days)) {
	case "day", "days", "daily", "every day":
		days = "sun,mon,tue,wed,thu,fri,sat"
	case "weekday", "weekdays":
		days = "mon,tue,wed,thu,fri"
	case "weekend", "weekends":
		days = "sat,sun"
	}
	var repeat Weekdays
	for _, day := range strings.Fields(strings.NewReplacer(",", " ", " and ", " ").Replace(strings.ToLower(days))) {
		found := false
		for n, short := range dayNames {
			full := strings.ToLower(time.Weekday(n).String())
			if day == short || day == full || day == full+"s" {
				repeat = append(repeat, time.Weekday(n))
				found = true
				break
			}
		}
		if !found {
			return 0, 0, nil, fmt.Errorf("say weekdays, weekends, every day, or weekday names")
		}
	}
	hour, minute, err := spokenClock(text)
	slices.Sort(repeat)
	return hour, minute, slices.Compact(repeat), err
}
