package clockview

var names = [...]string{
	"twelve", "one", "two", "three", "four", "five",
	"six", "seven", "eight", "nine", "ten", "eleven",
}

var counts = map[int]string{
	5: "five", 10: "ten", 20: "twenty", 25: "twenty-five",
}

func Say(hour, minute int) []string {
	near := (minute + 2) / 5 * 5
	if near >= 60 {
		hour++
		near = 0
	}

	to := near >= 35
	if to {
		hour++
		near = 60 - near
	}
	name := names[((hour%12)+12)%12]

	switch {
	case near == 0:
		return []string{name, "o'clock"}
	case near == 30:
		return []string{"half past", name}
	case near == 15 && !to:
		return []string{"quarter past", name}
	case near == 15 && to:
		return []string{"quarter to", name}
	}

	count, ok := counts[near]
	if !ok {
		return []string{name, "o'clock"}
	}

	if to {
		return []string{count + " to", name}
	}
	return []string{count + " past", name}
}
