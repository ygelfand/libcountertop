package vad

import (
	"bufio"
	"encoding/binary"
	"math"
	"math/rand/v2"
	"os"
	"strconv"
	"testing"
)

func speech(t testing.TB) []int16 {
	t.Helper()
	raw, err := os.ReadFile("testdata/speech.raw")
	if err != nil {
		t.Fatal(err)
	}
	pcm := make([]int16, len(raw)/2)
	for i := range pcm {
		pcm[i] = int16(binary.LittleEndian.Uint16(raw[i*2:]))
	}
	return pcm
}

func reference(t *testing.T) []float32 {
	t.Helper()
	f, err := os.Open("testdata/speech.prob")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []float32
	s := bufio.NewScanner(f)
	for s.Scan() {
		v, err := strconv.ParseFloat(s.Text(), 32)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, float32(v))
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestMatchesTheOnnxModel(t *testing.T) {
	want := reference(t)
	pcm := speech(t)
	d, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(pcm) / Chunk; got != len(want) {
		t.Fatalf("%d chunks of speech, reference has %d", got, len(want))
	}
	worst := 0.0
	for i := range want {
		p, err := d.Speech(pcm[i*Chunk : (i+1)*Chunk])
		if err != nil {
			t.Fatal(err)
		}
		worst = max(worst, math.Abs(float64(p-want[i])))
	}
	if worst > 1e-4 {
		t.Errorf("worst difference from the onnx model %.2g, want under 1e-4", worst)
	}
}

func TestSilenceAndNoiseAreNotSpeech(t *testing.T) {
	d, err := New()
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewPCG(1, 2))
	chunk := make([]int16, Chunk)
	for n := range 60 {
		for i := range chunk {
			chunk[i] = int16(r.NormFloat64() * 200)
		}
		p, err := d.Speech(chunk)
		if err != nil {
			t.Fatal(err)
		}
		if p > 0.5 {
			t.Fatalf("chunk %d of noise scored %.2f", n, p)
		}
	}
}

func TestResetForgetsTheStream(t *testing.T) {
	pcm := speech(t)
	d, err := New()
	if err != nil {
		t.Fatal(err)
	}
	first, _ := d.Speech(pcm[:Chunk])
	for i := 1; i < 20; i++ {
		_, _ = d.Speech(pcm[i*Chunk : (i+1)*Chunk])
	}
	d.Reset()
	again, _ := d.Speech(pcm[:Chunk])
	if first != again {
		t.Errorf("after Reset the first chunk scored %v, fresh %v", again, first)
	}
}

func TestChunkSize(t *testing.T) {
	d, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Speech(make([]int16, 480)); err == nil {
		t.Error("a 480-sample chunk was accepted")
	}
}

func BenchmarkSpeech(b *testing.B) {
	pcm := speech(b)
	d, err := New()
	if err != nil {
		b.Fatal(err)
	}
	chunks := len(pcm) / Chunk
	b.ResetTimer()
	for i := range b.N {
		c := i % chunks
		_, _ = d.Speech(pcm[c*Chunk : (c+1)*Chunk])
	}
}
