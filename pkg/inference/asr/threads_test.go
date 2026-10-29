package asr

import "testing"

func TestThreadsFollowTheCores(t *testing.T) {
	for _, c := range []struct {
		list string
		want int
	}{
		{"0-7", 4},
		{"0-3", 3},
		{"0-3,4-7", 4},
		{"0,1,2,3", 3},
		{"0", 3},
		{"", 3},
	} {
		if got := threadsFor(cores(c.list)); got != c.want {
			t.Errorf("%q: %d threads, want %d", c.list, got, c.want)
		}
	}
}
