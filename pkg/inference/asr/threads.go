package asr

import (
	"os"
	"strconv"
	"strings"
)

const possible = "/sys/devices/system/cpu/possible"

func Threads() int {
	raw, err := os.ReadFile(possible)
	if err != nil {
		return 3
	}
	return threadsFor(cores(strings.TrimSpace(string(raw))))
}

func threadsFor(cores int) int {
	if cores >= 8 {
		return 4
	}
	return 3
}

func cores(list string) int {
	n := 0
	for part := range strings.SplitSeq(list, ",") {
		lo, hi, ranged := strings.Cut(part, "-")
		a, err := strconv.Atoi(lo)
		if err != nil {
			continue
		}
		b := a
		if ranged {
			if b, err = strconv.Atoi(hi); err != nil {
				continue
			}
		}
		n += b - a + 1
	}
	return n
}
