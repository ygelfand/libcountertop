package vad

import (
	"math/rand/v2"
	"testing"
)

func silence(seconds float64) []int16 { return make([]int16, int(seconds*SampleRate)) }

func noise(seconds float64, level float64) []int16 {
	r := rand.New(rand.NewPCG(3, 4))
	out := make([]int16, int(seconds*SampleRate))
	for i := range out {
		out[i] = int16(r.NormFloat64() * level)
	}
	return out
}

func voiced(t *testing.T) []int16 { return speech(t)[18*Chunk : 122*Chunk] }

func moments(t *testing.T, parts ...[]int16) ([]Moment, int) {
	t.Helper()
	g, err := NewGate()
	if err != nil {
		t.Fatal(err)
	}
	var out []Moment
	samples := 0
	for _, p := range parts {
		for len(p) > 0 {
			n := min(160, len(p))
			for _, e := range g.Feed(p[:n]) {
				if e.Moment != Speaking || len(out) == 0 || out[len(out)-1] != Speaking {
					out = append(out, e.Moment)
				}
				samples += len(e.Samples)
			}
			p = p[n:]
		}
	}
	return out, samples
}

func TestSpeechOpensAndClosesTheGateOnce(t *testing.T) {
	words := voiced(t)[:2*SampleRate]
	got, samples := moments(t, silence(2), words, silence(1))
	want := []Moment{Began, Speaking, Ended}
	if len(got) != len(want) {
		t.Fatalf("moments %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("moments %v, want %v", got, want)
		}
	}
	if min := len(words) + preRoll*Chunk/2; samples < min {
		t.Errorf("%d samples passed, want the speech and its pre-roll (at least %d)", samples, min)
	}
}

func TestLongSpeechStaysOneUtterance(t *testing.T) {
	words := voiced(t)
	long := append(append([]int16{}, words...), words...)
	got, _ := moments(t, silence(1), long, silence(1))
	ended := 0
	for _, m := range got {
		if m == Ended {
			ended++
		}
	}
	if got[0] != Began || ended != 1 {
		t.Errorf("moments %v, want one utterance however long", got)
	}
}

func TestNoiseKeepsTheGateShut(t *testing.T) {
	if got, _ := moments(t, silence(3), noise(5, 300), silence(1)); len(got) != 0 {
		t.Errorf("moments %v from noise and silence", got)
	}
}
