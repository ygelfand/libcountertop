package timerview

import (
	"math"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/clockview"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func TestTheRingEmptiesClockwiseFromTwelve(t *testing.T) {
	for _, tc := range []struct {
		share      float64
		start, end float64
	}{
		{1, -math.Pi / 2, 3 * math.Pi / 2},
		{0.75, 0, 3 * math.Pi / 2},
		{0.5, math.Pi / 2, 3 * math.Pi / 2},
		{0.25, math.Pi, 3 * math.Pi / 2},
	} {
		start, sweep := ringArc(tc.share)
		if math.Abs(float64(start)-tc.start) > 1e-5 {
			t.Errorf("%v left starts at %v, want %v", tc.share, start, tc.start)
		}
		if end := float64(start + sweep); math.Abs(end-tc.end) > 1e-5 {
			t.Errorf("%v left ends at %v, want twelve o'clock", tc.share, end)
		}
	}
	if _, sweep := ringArc(0); sweep != 0 {
		t.Errorf("an empty ring still sweeps %v", sweep)
	}
}

func TestTheSweepRedrawsOnlyAsFastAsItMoves(t *testing.T) {
	for _, tc := range []struct {
		of     time.Duration
		length float32
		want   time.Duration
	}{
		{time.Hour, 1000, sweepSlowest},
		{10 * time.Second, 2000, sweepFastest},
		{100 * time.Second, 2000, 100 * time.Millisecond},
		{0, 1000, sweepSlowest},
		{time.Minute, 0, sweepSlowest},
	} {
		if got := sweepFrame(tc.of, tc.length); got != tc.want {
			t.Errorf("%v over %vpx redraws every %v, want %v", tc.of, tc.length, got, tc.want)
		}
	}
}

func TestTheCornerStaysInsideARoundScreen(t *testing.T) {
	const side, margin, wide = 480, 12, 160
	x, y := cornerSpot(side, side, wide, margin, margin)
	r := float64(side)/2 - margin
	for _, at := range [][2]float32{{x, y}, {x + wide, y}} {
		dx, dy := float64(at[0])-side/2, float64(at[1])-side/2
		if math.Hypot(dx, dy) > r+0.5 {
			t.Errorf("corner %v is outside the circle of radius %v", at, r)
		}
	}

	x, y = cornerSpot(1280, 800, wide, margin, margin)
	if x != 1280-margin-wide || y != margin {
		t.Errorf("on a wide screen the chip sits at %v,%v, want the corner", x, y)
	}
}

func TestTheFaceReadsTheTimeLeft(t *testing.T) {
	for _, tc := range []struct {
		left   time.Duration
		kind   schema.Face
		want   string
		second int
	}{
		{4*time.Minute + 59*time.Second, "plain", "4:59", 59},
		{4*time.Minute + 58500*time.Millisecond, "segments", "4:59", 59},
		{90 * time.Minute, "plain", "1:30", 0},
		{4*time.Minute + 59*time.Second, "analog-seconds", "0:04", 59},
		{0, "cards", "0:00", 0},
		{-time.Second, "plain", "0:00", 0},
	} {
		r := timerReading(tc.left, timerFace(tc.kind), "pasta")
		if r.Time != tc.want || r.Second != tc.second || r.Date != "pasta" {
			t.Errorf(
				"%v on %s reads %+v, want %s and second %d",
				tc.left,
				tc.kind,
				r,
				tc.want,
				tc.second,
			)
		}
	}
}

func TestEveryFaceHasATimerReading(t *testing.T) {
	for _, k := range append(clockview.Names(), schema.FaceNone) {
		switch got := timerFace(k).Name; got {
		case schema.FaceNone, "words", "analog", "roman":
			t.Errorf("%s counts down on %s", k, got)
		}
	}
}
