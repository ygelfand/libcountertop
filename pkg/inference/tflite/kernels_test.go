package tflite

import (
	"math"
	"testing"
)

func f32(t *testing.T, name string, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d values %v, want %d %v", name, len(got), got, len(want), want)
	}
	for i := range want {
		if math.Abs(float64(got[i]-want[i])) > 1e-5 {
			t.Fatalf("%s: got %v, want %v", name, got, want)
		}
	}
}

func shape(t *testing.T, name string, got, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: shape %v, want %v", name, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: shape %v, want %v", name, got, want)
		}
	}
}

func tf(shape []int, data ...float32) *Tensor {
	t := &Tensor{Type: Float32, Shape: shape, F32: data}
	if data == nil {
		t.F32 = make([]float32, count(shape))
	}
	return t
}

func ti(shape []int, data ...int32) *Tensor {
	return &Tensor{Type: Int32, Shape: shape, I32: data}
}

func out() *Tensor { return &Tensor{Type: Float32} }

func TestBroadcastShape(t *testing.T) {
	got, err := broadcastShape([]int{1, 16, 96}, []int{1, 16, 1})
	if err != nil {
		t.Fatal(err)
	}
	shape(t, "broadcast", got, []int{1, 16, 96})

	if _, err := broadcastShape([]int{3}, []int{4}); err == nil {
		t.Error("3 and 4 should not broadcast")
	}
}

func TestElementwiseBroadcastsAcrossRows(t *testing.T) {
	a := tf([]int{2, 3}, 1, 2, 3, 4, 5, 6)
	b := tf([]int{2, 1}, 10, 20)
	y := out()
	if err := kernels[OpSub](&OpDesc{}, []*Tensor{a, b}, []*Tensor{y}); err != nil {
		t.Fatal(err)
	}
	shape(t, "sub", y.Shape, []int{2, 3})
	f32(t, "sub", y.F32, []float32{-9, -8, -7, -16, -15, -14})
}

func TestReduceMeanKeepsDims(t *testing.T) {
	x := tf([]int{1, 2, 3}, 1, 2, 3, 10, 20, 30)
	axes := ti([]int{1}, -1)
	y := out()

	o := &OpDesc{Op: OpMean, keepDims: true}
	if err := kernels[OpMean](o, []*Tensor{x, axes}, []*Tensor{y}); err != nil {
		t.Fatal(err)
	}
	shape(t, "mean", y.Shape, []int{1, 2, 1})
	f32(t, "mean", y.F32, []float32{2, 20})
}

func TestReduceMaxDropsDims(t *testing.T) {
	x := tf([]int{2, 2}, 1, 7, 5, 3)
	y := out()
	if err := kernels[OpReduceMax](
		&OpDesc{},
		[]*Tensor{x, ti([]int{1}, 1)},
		[]*Tensor{y},
	); err != nil {
		t.Fatal(err)
	}
	shape(t, "reduce_max", y.Shape, []int{2})
	f32(t, "reduce_max", y.F32, []float32{7, 5})
}

func TestTransposeMovesAxes(t *testing.T) {
	x := tf([]int{2, 3}, 1, 2, 3, 4, 5, 6)
	y := out()
	if err := kernels[OpTranspose](
		&OpDesc{},
		[]*Tensor{x, ti([]int{2}, 1, 0)},
		[]*Tensor{y},
	); err != nil {
		t.Fatal(err)
	}
	shape(t, "transpose", y.Shape, []int{3, 2})
	f32(t, "transpose", y.F32, []float32{1, 4, 2, 5, 3, 6})
}

func TestPadInsertsZerosPerAxis(t *testing.T) {
	x := tf([]int{1, 2, 2, 1}, 1, 2, 3, 4)
	pads := ti([]int{4, 2}, 0, 0, 0, 0, 1, 1, 0, 0)
	y := out()
	if err := kernels[OpPad](&OpDesc{}, []*Tensor{x, pads}, []*Tensor{y}); err != nil {
		t.Fatal(err)
	}
	shape(t, "pad", y.Shape, []int{1, 2, 4, 1})
	f32(t, "pad", y.F32, []float32{0, 1, 2, 0, 0, 3, 4, 0})
}

func TestConv2DValidPadding(t *testing.T) {
	x := tf([]int{1, 3, 3, 1}, 1, 2, 3, 4, 5, 6, 7, 8, 9)
	w := tf([]int{1, 2, 2, 1}, 1, 1, 1, 1)
	y := out()

	o := &OpDesc{Op: OpConv2D, conv: convParams{strideW: 1, strideH: 1, dilationW: 1, dilationH: 1}}
	if err := kernels[OpConv2D](o, []*Tensor{x, w, nil}, []*Tensor{y}); err != nil {
		t.Fatal(err)
	}
	shape(t, "conv", y.Shape, []int{1, 2, 2, 1})
	f32(t, "conv", y.F32, []float32{12, 16, 24, 28})
}

func TestConv2DSamePaddingAndStride(t *testing.T) {
	x := tf([]int{1, 3, 3, 1}, 1, 2, 3, 4, 5, 6, 7, 8, 9)
	w := tf([]int{1, 2, 2, 1}, 1, 1, 1, 1)
	y := out()

	o := &OpDesc{
		Op:   OpConv2D,
		conv: convParams{samePad: true, strideW: 2, strideH: 2, dilationW: 1, dilationH: 1},
	}
	if err := kernels[OpConv2D](o, []*Tensor{x, w, nil}, []*Tensor{y}); err != nil {
		t.Fatal(err)
	}
	shape(t, "conv", y.Shape, []int{1, 2, 2, 1})
	// SAME with stride 2 pads nothing before the data and one row and column after.
	f32(t, "conv", y.F32, []float32{12, 9, 15, 9})
}

func TestMaxPoolHalvesTime(t *testing.T) {
	x := tf([]int{1, 4, 1, 1}, 1, 5, 2, 4)
	y := out()
	o := &OpDesc{Op: OpMaxPool2D, pool: poolParams{strideW: 1, strideH: 2, filterW: 1, filterH: 2}}
	if err := kernels[OpMaxPool2D](o, []*Tensor{x}, []*Tensor{y}); err != nil {
		t.Fatal(err)
	}
	shape(t, "pool", y.Shape, []int{1, 2, 1, 1})
	f32(t, "pool", y.F32, []float32{5, 4})
}

func TestFullyConnected(t *testing.T) {
	x := tf([]int{1, 3}, 1, 2, 3)
	w := tf([]int{2, 3}, 1, 0, 0, 0, 1, 1)
	b := tf([]int{2}, 0.5, -1)
	y := out()
	if err := kernels[OpFullyConnected](&OpDesc{}, []*Tensor{x, w, b}, []*Tensor{y}); err != nil {
		t.Fatal(err)
	}
	shape(t, "fc", y.Shape, []int{1, 2})
	f32(t, "fc", y.F32, []float32{1.5, 4})
}

func TestSVDFRemembersEarlierFrames(t *testing.T) {
	feature := tf([]int{2, 2}, 1, 0, 0, 1)
	time := tf([]int{2, 2}, 1, 10, 1, 10)
	bias := tf([]int{2}, 0.5, -1)
	state := tf([]int{1, 4})
	op := &OpDesc{rank: 1}
	for _, step := range []struct {
		x    []float32
		want []float32
	}{
		{[]float32{1, 2}, []float32{10.5, 19}},
		{[]float32{3, 4}, []float32{31.5, 41}},
	} {
		y := out()
		x := tf([]int{1, 2}, step.x...)
		if err := kernels[OpSVDF](
			op,
			[]*Tensor{x, feature, time, bias, state},
			[]*Tensor{y},
		); err != nil {
			t.Fatal(err)
		}
		shape(t, "svdf", y.Shape, []int{1, 2})
		f32(t, "svdf", y.F32, step.want)
	}
}

func TestSoftmaxSumsToOne(t *testing.T) {
	x := tf([]int{1, 3}, 1, 2, 3)
	y := out()
	if err := kernels[OpSoftmax](&OpDesc{beta: 1}, []*Tensor{x}, []*Tensor{y}); err != nil {
		t.Fatal(err)
	}
	f32(t, "softmax", y.F32, []float32{0.09003057, 0.24472847, 0.66524096})
}

func TestQuantizedConstantsBecomeFloats(t *testing.T) {
	for _, c := range []struct {
		typ  TensorType
		zero int64
		raw  []byte
		want []float32
	}{
		{Int8, 0, []byte{0xff, 0x02}, []float32{-0.5, 1}},
		{Uint8, 0, []byte{0xff, 0x02}, []float32{-0.5, 1}},
		{Uint8, 128, []byte{0x80, 0x82}, []float32{0, 1}},
	} {
		tensor := &Tensor{Type: c.typ}
		if err := decode(tensor, c.raw, &TensorDesc{Scale: 0.5, ZeroPoint: c.zero}); err != nil {
			t.Fatal(err)
		}
		if tensor.Type != Float32 {
			t.Errorf("%s decoded as %s", c.typ, tensor.Type)
		}
		f32(t, c.typ.String(), tensor.F32, c.want)
	}
}

func TestBatchMatMul(t *testing.T) {
	a := tf([]int{1, 2, 3}, 1, 2, 3, 4, 5, 6)
	b := tf([]int{1, 3, 2}, 1, 2, 3, 4, 5, 6)
	y := out()
	if err := kernels[OpBatchMatMul](&OpDesc{}, []*Tensor{a, b}, []*Tensor{y}); err != nil {
		t.Fatal(err)
	}
	shape(t, "matmul", y.Shape, []int{1, 2, 2})
	f32(t, "matmul", y.F32, []float32{22, 28, 49, 64})
}

func TestReshapeInfersFreeDimension(t *testing.T) {
	x := tf([]int{2, 3}, 1, 2, 3, 4, 5, 6)
	y := out()
	if err := kernels[OpReshape](
		&OpDesc{},
		[]*Tensor{x, ti([]int{2}, 3, -1)},
		[]*Tensor{y},
	); err != nil {
		t.Fatal(err)
	}
	shape(t, "reshape", y.Shape, []int{3, 2})
	f32(t, "reshape", y.F32, []float32{1, 2, 3, 4, 5, 6})
}

func TestSqueezeAndExpandDims(t *testing.T) {
	x := tf([]int{1, 3, 1}, 1, 2, 3)
	y := out()
	if err := kernels[OpSqueeze](&OpDesc{}, []*Tensor{x}, []*Tensor{y}); err != nil {
		t.Fatal(err)
	}
	shape(t, "squeeze", y.Shape, []int{3})

	z := out()
	if err := kernels[OpExpandDims](
		&OpDesc{},
		[]*Tensor{y, ti([]int{1}, 0)},
		[]*Tensor{z},
	); err != nil {
		t.Fatal(err)
	}
	shape(t, "expand_dims", z.Shape, []int{1, 3})
}

func TestLeakyRelu(t *testing.T) {
	x := tf([]int{2}, -2, 3)
	y := out()
	o := &OpDesc{Op: OpLeakyRelu, alpha: 0.5}
	if err := kernels[OpLeakyRelu](o, []*Tensor{x}, []*Tensor{y}); err != nil {
		t.Fatal(err)
	}
	f32(t, "leaky_relu", y.F32, []float32{-1, 3})
}

func TestFusedReluClampsConvOutput(t *testing.T) {
	x := tf([]int{1, 1, 1, 1}, -5)
	w := tf([]int{1, 1, 1, 1}, 1)
	y := out()
	o := &OpDesc{
		Op:   OpConv2D,
		conv: convParams{strideW: 1, strideH: 1, dilationW: 1, dilationH: 1, act: ActRelu},
	}
	if err := kernels[OpConv2D](o, []*Tensor{x, w, nil}, []*Tensor{y}); err != nil {
		t.Fatal(err)
	}
	f32(t, "conv+relu", y.F32, []float32{0})
}

func TestGreaterEqualBroadcastsToBools(t *testing.T) {
	y := &Tensor{Type: Bool}
	if err := kernels[OpGreaterEqual](
		&OpDesc{},
		[]*Tensor{tf([]int{3}, 0.2, 0.5, 0.9), tf([]int{}, 0.5)},
		[]*Tensor{y},
	); err != nil {
		t.Fatal(err)
	}
	shape(t, "greater_equal", y.Shape, []int{3})
	if y.I32[0] != 0 || y.I32[1] != 1 || y.I32[2] != 1 {
		t.Errorf("greater_equal gave %v, want [0 1 1]", y.I32)
	}
}

func TestReshapeToAScalar(t *testing.T) {
	y := &Tensor{Type: Bool}
	x := &Tensor{Type: Bool, Shape: []int{1, 1}, I32: []int32{1}}
	if err := kernels[OpReshape](
		&OpDesc{},
		[]*Tensor{x, ti([]int{0})},
		[]*Tensor{y},
	); err != nil {
		t.Fatal(err)
	}
	shape(t, "reshape", y.Shape, []int{})
	if len(y.I32) != 1 || y.I32[0] != 1 {
		t.Errorf("reshape to a scalar gave %v, want [1]", y.I32)
	}
}

func TestIfRunsTheBranchItsConditionPicks(t *testing.T) {
	branch := func(op Op) *Subgraph {
		return &Subgraph{
			Tensors: []*TensorDesc{
				{Type: Float32, Shape: []int{2}},
				{Type: Float32, Shape: []int{2}},
			},
			Ops:     []*OpDesc{{Op: op, Inputs: []int{0, 0}, Outputs: []int{1}}},
			Inputs:  []int{0},
			Outputs: []int{1},
		}
	}
	m := &Model{Subgraphs: []*Subgraph{
		{
			Tensors: []*TensorDesc{
				{Type: Bool, Shape: []int{}},
				{Type: Float32, Shape: []int{2}},
				{Type: Float32, Shape: []int{2}},
			},
			Ops: []*OpDesc{
				{Op: OpIf, Inputs: []int{0, 1}, Outputs: []int{2}, then: 1, els: 2},
			},
			Inputs:  []int{0, 1},
			Outputs: []int{2},
		},
		branch(OpAdd),
		branch(OpMul),
	}}
	in, err := New(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		cond int32
		want []float32
	}{
		{1, []float32{6, 8}},
		{0, []float32{9, 16}},
	} {
		in.Input(0).I32 = []int32{tc.cond}
		copy(in.Input(1).F32, []float32{3, 4})
		if err := in.Invoke(); err != nil {
			t.Fatal(err)
		}
		f32(t, "if", in.Output(0).F32, tc.want)
	}
}
