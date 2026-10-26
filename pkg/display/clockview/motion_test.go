package clockview

import "testing"

func TestATurnRemembersWhereItCameFrom(t *testing.T) {
	if from, at := turning(nil, "test-turn", "1037"); from != "1037" || at != 1 {
		t.Fatalf("first reading turns from %q at %v, want 1037 at 1", from, at)
	}
	if from, at := turning(nil, "test-turn", "1038"); from != "1037" || at != 1 {
		t.Errorf("next minute turns from %q at %v, want 1037 at 1", from, at)
	}
	if from, _ := turning(nil, "test-turn", "1038"); from != "1037" {
		t.Errorf("the same minute again turns from %q, want 1037", from)
	}
}

func TestBarsAlwaysSweepClockwise(t *testing.T) {
	for _, tc := range []struct {
		from, to float64
		at       float32
		want     float64
	}{
		{350, 10, 0.5, 360},
		{90, 0, 0.5, 225},
		{0, 0, 0.5, 0},
		{270, 90, 1, 450},
	} {
		if got := sweep(tc.from, tc.to, tc.at); got != tc.want {
			t.Errorf("%v → %v at %v is %v, want %v", tc.from, tc.to, tc.at, got, tc.want)
		}
	}
}

func TestEveryDigitHasBars(t *testing.T) {
	for c := byte('0'); c <= '9'; c++ {
		if _, ok := barDigits[c]; !ok {
			t.Errorf("%c has no bars", c)
		}
	}
}
