package resample

import (
	"math/rand"
	"testing"
)

type reference struct {
	r    *Rational
	tail []int16
	at   int
}

func newReference(r *Rational) *reference {
	return &reference{
		r:    r,
		tail: make([]int16, (r.taps-1)*r.channels),
		at:   (r.taps - 1) * r.up,
	}
}

func (ref *reference) run(in []int16) []int16 {
	r := ref.r
	ch := r.channels
	buf := append(ref.tail, in...)
	frames := len(buf) / ch
	var out []int16
	for {
		idx := ref.at / r.up
		if idx >= frames {
			break
		}
		if idx < r.taps-1 {
			ref.at += r.down
			continue
		}
		phase := ref.at % r.up
		for c := range ch {
			var sum float32
			for k := range r.taps {
				sum += r.filter[phase+k*r.up] * float32(buf[(idx-k)*ch+c])
			}
			out = append(out, clip(sum))
		}
		ref.at += r.step()
	}
	idx := min(ref.at/r.up, frames)
	drop := max(idx-(r.taps-1), 0)
	ref.at -= drop * r.up
	ref.tail = append(ref.tail[:0], buf[drop*ch:]...)
	return out
}

func TestTheResamplerMatchesTheDirectFilter(t *testing.T) {
	for _, c := range []struct {
		from, to, channels int
		skew               float64
		headroom           float32
	}{
		{96000, 16000, 4, 0, 1},
		{96000, 16000, 2, 0, 1},
		{44100, 48000, 2, 0, 1},
		{48000, 44100, 2, 0, 0.9},
		{16000, 48000, 1, 0, 1},
		{48000, 48000, 2, 120, 1},
	} {
		var r, twin *Rational
		if c.from == c.to {
			r, twin = NewSkewable(c.from, c.to, c.channels), NewSkewable(c.from, c.to, c.channels)
		} else {
			r, twin = NewRational(c.from, c.to, c.channels), NewRational(c.from, c.to, c.channels)
		}
		r.Headroom(c.headroom)
		twin.Headroom(c.headroom)
		r.Skew(c.skew)
		twin.Skew(c.skew)
		ref := newReference(twin)
		rng := rand.New(rand.NewSource(1))
		var got, want []int16
		for range 40 {
			block := make([]int16, (1+rng.Intn(700))*c.channels)
			for i := range block {
				block[i] = int16(rng.Intn(60000) - 30000)
			}
			got = append(got, r.Run(block)...)
			want = append(want, ref.run(block)...)
		}
		if len(got) != len(want) {
			t.Fatalf(
				"%d to %d x%d: %d samples, want %d",
				c.from,
				c.to,
				c.channels,
				len(got),
				len(want),
			)
		}
		for i := range got {
			if d := int(got[i]) - int(want[i]); d > 1 || d < -1 {
				t.Fatalf(
					"%d to %d x%d: sample %d is %d, want %d",
					c.from,
					c.to,
					c.channels,
					i,
					got[i],
					want[i],
				)
			}
		}
	}
}

func micBlock() []int16 {
	block := make([]int16, 960*6)
	for i := range block {
		block[i] = int16(i * 37)
	}
	return block
}

func BenchmarkMicDecimation(b *testing.B) {
	r, block := NewRational(96000, 16000, 6), micBlock()
	for b.Loop() {
		r.Run(block)
	}
}

func BenchmarkMicDecimationDirect(b *testing.B) {
	ref, block := newReference(NewRational(96000, 16000, 6)), micBlock()
	for b.Loop() {
		ref.run(block)
	}
}
