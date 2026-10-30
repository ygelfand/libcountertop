package tflite

import (
	"fmt"
	"math"

	"github.com/ygelfand/libcountertop/pkg/inference/vec"
)

func conv2D(o *OpDesc, in, out []*Tensor) error {
	p := o.conv
	x, w, y := in[0], in[1], out[0]
	if len(x.Shape) != 4 || len(w.Shape) != 4 {
		return fmt.Errorf("expected 4-D input and filter, got %v and %v", x.Shape, w.Shape)
	}

	batch, ih, iw, ic := x.Shape[0], x.Shape[1], x.Shape[2], x.Shape[3]
	oc, kh, kw := w.Shape[0], w.Shape[1], w.Shape[2]
	if w.Shape[3] != ic {
		return fmt.Errorf("filter has %d input channels, input has %d", w.Shape[3], ic)
	}

	var bias []float32
	if len(in) > 2 && in[2] != nil {
		bias = in[2].F32
	}

	oh := outputSize(ih, kh, p.strideH, p.dilationH, p.samePad)
	ow := outputSize(iw, kw, p.strideW, p.dilationW, p.samePad)
	y.resize([]int{batch, oh, ow, oc})

	padH, padW := 0, 0
	if p.samePad {
		padH = padFor(p.strideH, p.dilationH, ih, kh, oh)
		padW = padFor(p.strideW, p.dilationW, iw, kw, ow)
	}

	xd, wd, yd := x.F32, w.F32, y.F32
	fs := kh * kw * ic
	yi := 0
	for b := 0; b < batch; b++ {
		for oy := 0; oy < oh; oy++ {
			for ox := 0; ox < ow; ox++ {
				f := 0
				for ; ic > 1 && f+4 <= oc; f += 4 {
					var acc [4]float32
					if bias != nil {
						copy(acc[:], bias[f:f+4])
					}
					for ky := 0; ky < kh; ky++ {
						iy := oy*p.strideH - padH + ky*p.dilationH
						if iy < 0 || iy >= ih {
							continue
						}
						for kx := 0; kx < kw; kx++ {
							ix := ox*p.strideW - padW + kx*p.dilationW
							if ix < 0 || ix >= iw {
								continue
							}
							xo := ((b*ih+iy)*iw + ix) * ic
							wo := ((f*kh+ky)*kw + kx) * ic
							s := vec.Dot4(
								xd[xo:xo+ic],
								wd[wo:wo+ic],
								wd[wo+fs:wo+fs+ic],
								wd[wo+2*fs:wo+2*fs+ic],
								wd[wo+3*fs:wo+3*fs+ic],
							)
							acc[0] += s[0]
							acc[1] += s[1]
							acc[2] += s[2]
							acc[3] += s[3]
						}
					}
					for _, a := range acc {
						yd[yi] = activate(a, p.act)
						yi++
					}
				}
				for ; f < oc; f++ {
					var acc float32
					if bias != nil {
						acc = bias[f]
					}
					for ky := 0; ky < kh; ky++ {
						iy := oy*p.strideH - padH + ky*p.dilationH
						if iy < 0 || iy >= ih {
							continue
						}

						if ic == 1 && p.dilationW == 1 {
							x0 := ox*p.strideW - padW
							lo, hi := max(0, -x0), min(kw, iw-x0)
							if hi > lo {
								xo := (b*ih+iy)*iw + x0 + lo
								wo := (f*kh+ky)*kw + lo
								acc += vec.Dot(wd[wo:wo+hi-lo], xd[xo:xo+hi-lo])
							}
							continue
						}

						for kx := 0; kx < kw; kx++ {
							ix := ox*p.strideW - padW + kx*p.dilationW
							if ix < 0 || ix >= iw {
								continue
							}
							xo := ((b*ih+iy)*iw + ix) * ic
							wo := ((f*kh+ky)*kw + kx) * ic
							acc += vec.Dot(xd[xo:xo+ic], wd[wo:wo+ic])
						}
					}
					yd[yi] = activate(acc, p.act)
					yi++
				}
			}
		}
	}
	return nil
}

func maxPool2D(o *OpDesc, in, out []*Tensor) error {
	p := o.pool
	x, y := in[0], out[0]
	if len(x.Shape) != 4 {
		return fmt.Errorf("expected a 4-D input, got %v", x.Shape)
	}

	batch, ih, iw, c := x.Shape[0], x.Shape[1], x.Shape[2], x.Shape[3]
	oh := outputSize(ih, p.filterH, p.strideH, 1, p.samePad)
	ow := outputSize(iw, p.filterW, p.strideW, 1, p.samePad)
	y.resize([]int{batch, oh, ow, c})

	padH, padW := 0, 0
	if p.samePad {
		padH = padFor(p.strideH, 1, ih, p.filterH, oh)
		padW = padFor(p.strideW, 1, iw, p.filterW, ow)
	}

	xd, yd := x.F32, y.F32
	yi := 0
	for b := 0; b < batch; b++ {
		for oy := 0; oy < oh; oy++ {
			y0 := oy*p.strideH - padH
			for ox := 0; ox < ow; ox++ {
				x0 := ox*p.strideW - padW
				for ch := 0; ch < c; ch++ {
					best := float32(math.Inf(-1))
					for ky := 0; ky < p.filterH; ky++ {
						iy := y0 + ky
						if iy < 0 || iy >= ih {
							continue
						}
						for kx := 0; kx < p.filterW; kx++ {
							ix := x0 + kx
							if ix < 0 || ix >= iw {
								continue
							}
							if v := xd[((b*ih+iy)*iw+ix)*c+ch]; v > best {
								best = v
							}
						}
					}
					yd[yi] = activate(best, p.act)
					yi++
				}
			}
		}
	}
	return nil
}

func fullyConnected(o *OpDesc, in, out []*Tensor) error {
	x, w, y := in[0], in[1], out[0]
	if len(w.Shape) < 2 {
		return fmt.Errorf("expected 2-D weights, got %v", w.Shape)
	}

	inC := w.Shape[len(w.Shape)-1]
	oc := count(w.Shape[:len(w.Shape)-1])
	total := x.Count()
	if inC == 0 || total%inC != 0 {
		return fmt.Errorf("input of %d values does not divide into rows of %d", total, inC)
	}
	batch := total / inC

	var bias []float32
	if len(in) > 2 && in[2] != nil {
		bias = in[2].F32
	}

	y.resize([]int{batch, oc})
	act := o.act
	for b := 0; b < batch; b++ {
		row := x.F32[b*inC : (b+1)*inC]
		for f := 0; f < oc; f++ {
			acc := vec.Dot(row, w.F32[f*inC:(f+1)*inC])
			if bias != nil {
				acc += bias[f]
			}
			y.F32[b*oc+f] = activate(acc, act)
		}
	}
	return nil
}

func svdf(o *OpDesc, in, out []*Tensor) error {
	if len(in) < 5 || in[4] == nil {
		return fmt.Errorf("expected input, feature and time weights, bias and state")
	}
	x, feature, time, state, y := in[0], in[1], in[2], in[4], out[0]
	if len(feature.Shape) != 2 || len(time.Shape) != 2 || o.rank <= 0 {
		return fmt.Errorf("feature %v, time %v, rank %d", feature.Shape, time.Shape, o.rank)
	}
	filters, inSize, memory := feature.Shape[0], feature.Shape[1], time.Shape[1]
	if time.Shape[0] != filters || filters%o.rank != 0 || x.Count()%inSize != 0 {
		return fmt.Errorf(
			"feature %v, time %v, rank %d, input %v",
			feature.Shape,
			time.Shape,
			o.rank,
			x.Shape,
		)
	}
	units := filters / o.rank
	batch := x.Count() / inSize
	if state.Count() != batch*filters*memory {
		return fmt.Errorf("state of %d values, want %d", state.Count(), batch*filters*memory)
	}
	var bias []float32
	if in[3] != nil {
		bias = in[3].F32
	}

	y.resize([]int{batch, units})
	for b := 0; b < batch; b++ {
		row := x.F32[b*inSize : (b+1)*inSize]
		s := state.F32[b*filters*memory : (b+1)*filters*memory]
		copy(s, s[1:])
		for f := 0; f < filters; f++ {
			s[f*memory+memory-1] = vec.Dot(row, feature.F32[f*inSize:(f+1)*inSize])
		}
		for u := 0; u < units; u++ {
			var acc float32
			for r := 0; r < o.rank; r++ {
				f := u*o.rank + r
				acc += vec.Dot(s[f*memory:(f+1)*memory], time.F32[f*memory:(f+1)*memory])
			}
			if bias != nil {
				acc += bias[u]
			}
			y.F32[b*units+u] = activate(acc, o.act)
		}
	}
	return nil
}

func softmax(o *OpDesc, in, out []*Tensor) error {
	x, y := in[0], out[0]
	n := x.Dim(-1)
	if n == 0 || x.Count()%n != 0 {
		return fmt.Errorf("input %v", x.Shape)
	}
	y.resize(x.Shape)
	for at := 0; at < x.Count(); at += n {
		row, dst := x.F32[at:at+n], y.F32[at:at+n]
		top := row[0]
		for _, v := range row {
			top = max(top, v)
		}
		var sum float64
		for i, v := range row {
			e := math.Exp(float64(o.beta * (v - top)))
			dst[i] = float32(e)
			sum += e
		}
		for i := range dst {
			dst[i] = float32(float64(dst[i]) / sum)
		}
	}
	return nil
}

func batchMatMul(o *OpDesc, in, out []*Tensor) error {
	a, b, y := in[0], in[1], out[0]
	adjX, adjY := o.adjX, o.adjY
	if len(a.Shape) < 2 || len(b.Shape) < 2 {
		return fmt.Errorf("expected at least 2-D inputs, got %v and %v", a.Shape, b.Shape)
	}

	m, k := a.Dim(-2), a.Dim(-1)
	if adjX {
		m, k = k, m
	}
	kb, n := b.Dim(-2), b.Dim(-1)
	if adjY {
		kb, n = n, kb
	}
	if k != kb {
		return fmt.Errorf("inner dimensions %d and %d do not match", k, kb)
	}

	batchShape, err := broadcastShape(a.Shape[:len(a.Shape)-2], b.Shape[:len(b.Shape)-2])
	if err != nil {
		return err
	}
	y.resize(append(append([]int{}, batchShape...), m, n))

	batches := count(batchShape)
	as := strides(a.Shape[:len(a.Shape)-2], batchShape)
	bs := strides(b.Shape[:len(b.Shape)-2], batchShape)
	idx := make([]int, len(batchShape))

	ao, bo := 0, 0
	for batch := 0; batch < batches; batch++ {
		am := a.F32[ao*m*k:]
		bm := b.F32[bo*kb*n:]
		ym := y.F32[batch*m*n:]

		for i := 0; i < m; i++ {
			for j := 0; j < n; j++ {
				var acc float32
				for p := 0; p < k; p++ {
					var av, bv float32
					if adjX {
						av = am[p*m+i]
					} else {
						av = am[i*k+p]
					}
					if adjY {
						bv = bm[j*kb+p]
					} else {
						bv = bm[p*n+j]
					}
					acc += av * bv
				}
				ym[i*n+j] = acc
			}
		}
		ao, bo = step(idx, batchShape, as, bs, ao, bo)
	}
	return nil
}

func step(idx, shape, as, bs []int, ao, bo int) (int, int) {
	for d := len(shape) - 1; d >= 0; d-- {
		idx[d]++
		ao += as[d]
		bo += bs[d]
		if idx[d] < shape[d] {
			return ao, bo
		}
		idx[d] = 0
		ao -= as[d] * shape[d]
		bo -= bs[d] * shape[d]
	}
	return ao, bo
}
