package resample

import (
	"math"

	"github.com/ygelfand/libcountertop/pkg/inference/vec"
)

const ratioTaps = 16

type Rational struct {
	up   int
	down int

	channels int

	taps   int
	filter []float32
	kern   [][]float32

	at int

	hist [][]float32

	skew, owed float64

	clipped uint64
}

func NewRational(from, to, channels int) *Rational {
	if channels < 1 {
		channels = 1
	}

	g := gcd(from, to)
	r := &Rational{up: to / g, down: from / g, channels: channels, taps: ratioTaps}

	if from != to {
		cut := 1.0
		if to < from {
			cut = 0.9 * float64(to) / float64(from)
		}
		r.taps = int(math.Ceil(ratioTaps / cut))
		r.filter = lowpass(r.up, r.taps, cut)
		r.kernels()
	}
	r.Reset()
	return r
}

const skewGrid = 160

func NewSkewable(from, to, channels int) *Rational {
	if from != to {
		return NewRational(from, to, channels)
	}
	r := &Rational{
		up:       skewGrid,
		down:     skewGrid,
		channels: max(channels, 1),
		taps:     ratioTaps,
		filter:   lowpass(skewGrid, ratioTaps, 1),
	}
	r.kernels()
	r.Reset()
	return r
}

func (r *Rational) Headroom(g float32) {
	for i := range r.filter {
		r.filter[i] *= g
	}
	r.kernels()
}

func (r *Rational) kernels() {
	r.kern = make([][]float32, r.up)
	for p := range r.up {
		k := make([]float32, r.taps)
		for i := range r.taps {
			k[r.taps-1-i] = r.filter[p+i*r.up]
		}
		r.kern[p] = k
	}
}

func (r *Rational) Ratio() (up, down int) { return r.up, r.down }

func (r *Rational) Run(in []int16) []int16 {
	if r.filter == nil || len(in) == 0 {
		return in
	}

	ch := r.channels
	for c, h := range r.hist {
		for i := c; i < len(in); i += ch {
			h = append(h, float32(in[i]))
		}
		r.hist[c] = h
	}
	frames := len(r.hist[0])

	out := make([]int16, 0, (len(in)*r.up/r.down)+ch)

	for {
		idx := r.at / r.up
		if idx >= frames {
			break
		}
		if idx < r.taps-1 {
			r.at += r.down
			continue
		}

		kern := r.kern[r.at%r.up]
		from := idx - r.taps + 1
		for _, h := range r.hist {
			sum := vec.Dot(kern, h[from:idx+1])
			if sum > 32767 || sum < -32768 {
				r.clipped++
			}
			out = append(out, clip(sum))
		}
		r.at += r.step()
	}

	idx := min(r.at/r.up, frames)
	drop := max(idx-(r.taps-1), 0)

	r.at -= drop * r.up
	for c, h := range r.hist {
		r.hist[c] = append(h[:0], h[drop:]...)
	}

	return out
}

func (r *Rational) Reset() {
	r.at = 0
	r.skew, r.owed = 0, 0

	if len(r.hist) != r.channels {
		r.hist = make([][]float32, r.channels)
	}
	for c := range r.hist {
		r.hist[c] = append(r.hist[c][:0], make([]float32, r.taps-1)...)
	}
	r.at = (r.taps - 1) * r.up
}

func (r *Rational) Clipped() uint64 { return r.clipped }

func (r *Rational) Skew(ppm float64) { r.skew = ppm * 1e-6 * float64(r.down) }

func (r *Rational) step() int {
	r.owed += r.skew
	extra := int(r.owed)
	r.owed -= float64(extra)
	return r.down + extra
}

func lowpass(phases, taps int, cut float64) []float32 {
	n := phases * taps
	h := make([]float32, n)

	center := float64(n-1) / 2
	for i := range h {
		x := cut * (float64(i) - center) / float64(phases)

		s := 1.0
		if x != 0 {
			s = math.Sin(math.Pi*x) / (math.Pi * x)
		}

		w := 0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/float64(n-1))
		h[i] = float32(s * w)
	}

	for p := range phases {
		var sum float32
		for k := range taps {
			sum += h[p+k*phases]
		}
		if sum == 0 {
			continue
		}
		for k := range taps {
			h[p+k*phases] /= sum
		}
	}
	return h
}

func clip(v float32) int16 {
	switch {
	case v > 32767:
		return 32767
	case v < -32768:
		return -32768
	}
	return int16(v)
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a == 0 {
		return 1
	}
	return a
}
