package detect

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/inference/wake"
)

func TestObservationDoesNotChangeJudgement(t *testing.T) {
	a, b := New(1, nil), New(1, nil)
	a.Threshold = func(int) float64 { return .75 }
	b.Threshold = a.Threshold
	var points, crossings, held int
	var detectedA, detectedB atomic.Int32
	a.OnDetect = func(int) { detectedA.Add(1) }
	b.OnDetect = func(int) { detectedB.Add(1) }
	b.OnScore = func(slot int, score, cutoff float64, crossing, suppressed bool, dropped uint64) {
		points++
		if crossing {
			crossings++
		}
		if cutoff != .75 {
			t.Fatal(cutoff)
		}
	}
	b.OnPeak = func(slot int, p float64) {
		held++
		if p < .75 {
			t.Fatal(p)
		}
	}
	var x, y slot
	scores := []float64{
		.1,
		.74,
		.76,
		.8,
		.95,
		.7,
		.3,
		.9,
		.9,
		.9,
		.1,
		.8,
		.9,
		.9,
		.9,
		.9,
		.1,
		.1,
		.1,
		.1,
	}
	for i, v := range scores {
		at := time.Unix(1000, 0).Add(time.Duration(i) * 100 * time.Millisecond)
		a.judge(0, &x, v, at)
		b.judge(0, &y, v, at)
		if x.holding != y.holding || x.crossing != y.crossing || x.peak != y.peak ||
			x.quiet != y.quiet ||
			x.suppressed != y.suppressed {
			t.Fatal("observer altered detector", i)
		}
	}
	a.callbacks.Wait()
	b.callbacks.Wait()
	if points != len(scores) || crossings != 2 || held != 2 {
		t.Fatal(points, crossings, held)
	}
	if detectedA.Load() != 2 || detectedB.Load() != detectedA.Load() {
		t.Fatalf(
			"detections: without observer %d, with observer %d",
			detectedA.Load(),
			detectedB.Load(),
		)
	}
}

type observationSource struct{ frames chan []int16 }

func (s observationSource) Listen(string) (<-chan []int16, func()) { return s.frames, func() {} }
func (observationSource) Dropped() uint64                          { return 7 }

func TestObservationSeesFramesBeforeScoresAndWhilePaused(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(map[bool]string{false: "running", true: "paused"}[paused], func(t *testing.T) {
			fakes(t)
			source := observationSource{frames: make(chan []int16, 1)}
			source.frames <- []int16{1, 2, 3}
			close(source.frames)
			e := New(1, source)
			e.Paused = func() bool { return paused }
			if err := e.Use(0, model("test", wake.KindMicroWakeWord)); err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			var order []string
			e.OnFrame = func(frame []int16, stopped bool, dropped uint64) {
				if !reflect.DeepEqual(frame, []int16{1, 2, 3}) || stopped != paused ||
					dropped != 7 {
					t.Errorf("frame=%v paused=%v dropped=%d", frame, stopped, dropped)
				}
				order = append(order, "frame")
			}
			e.OnScore = func(_ int, _, _ float64, _, _ bool, dropped uint64) {
				if dropped != 7 {
					t.Errorf("dropped=%d", dropped)
				}
				order = append(order, "score")
			}
			if err := e.Run(context.Background()); err == nil {
				t.Fatal("closed source was not reported")
			}
			want := []string{"frame", "score"}
			if paused {
				want = want[:1]
			}
			if !reflect.DeepEqual(order, want) {
				t.Fatalf("order=%v, want %v", order, want)
			}
		})
	}
}

func TestObservationReportsOnlySuccessfulModelLoads(t *testing.T) {
	fakes(t)
	e := New(1, nil)
	defer e.Close()
	var loaded []wake.Model
	e.OnModel = func(slot int, m wake.Model) {
		if slot != 0 {
			t.Errorf("slot=%d", slot)
		}
		loaded = append(loaded, m)
	}
	m := model("test", wake.KindMicroWakeWord)
	if err := e.Use(0, m); err != nil {
		t.Fatal(err)
	}
	if err := e.Use(0, m); err != nil {
		t.Fatal(err)
	}
	if err := e.Use(1, m); err == nil {
		t.Fatal("invalid slot accepted")
	}
	if len(loaded) != 1 || !reflect.DeepEqual(loaded[0], m) {
		t.Fatalf("loads=%v", loaded)
	}
}

func TestObservationDistinguishesHoldAndRefractory(t *testing.T) {
	e := New(1, nil)
	e.Threshold = func(int) float64 { return .75 }
	var s slot
	var crossings, suppressed []bool
	var peak float64
	e.OnScore = func(_ int, _, _ float64, c, r bool, _ uint64) {
		crossings, suppressed = append(crossings, c), append(suppressed, r)
	}
	e.OnPeak = func(_ int, p float64) { peak = p }
	now := time.Unix(1000, 0)
	e.judge(0, &s, .8, now)
	e.judge(0, &s, .95, now.Add(Hold/2))
	e.judge(0, &s, .7, now.Add(Hold))
	e.judge(0, &s, .99, now.Add(Hold+Refractory/2))
	e.judge(0, &s, .8, now.Add(Hold+Refractory))
	if !reflect.DeepEqual(crossings, []bool{true, false, false, false, true}) ||
		!reflect.DeepEqual(suppressed, []bool{false, false, false, true, false}) || peak != .95 {
		t.Fatalf("crossings=%v suppressed=%v peak=%v", crossings, suppressed, peak)
	}
}
