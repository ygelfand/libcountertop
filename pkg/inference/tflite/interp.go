package tflite

import (
	"encoding/binary"
	"fmt"
	"math"
)

type Tensor struct {
	Name  string
	Type  TensorType
	Shape []int
	F32   []float32
	I32   []int32
	Const bool
}

func count(shape []int) int {
	n := 1
	for _, d := range shape {
		n *= d
	}
	return n
}

func (t *Tensor) Count() int { return count(t.Shape) }

func (t *Tensor) Dim(i int) int {
	if i < 0 {
		i += len(t.Shape)
	}
	if i < 0 || i >= len(t.Shape) {
		return 1
	}
	return t.Shape[i]
}

func (t *Tensor) resize(shape []int) {
	n := count(shape)
	t.Shape = append(t.Shape[:0], shape...)
	switch t.Type {
	case Float32:
		if cap(t.F32) < n {
			t.F32 = make([]float32, n)
		}
		t.F32 = t.F32[:n]
	default:
		if cap(t.I32) < n {
			t.I32 = make([]int32, n)
		}
		t.I32 = t.I32[:n]
	}
}

type Interpreter struct {
	model    *Model
	graph    *Subgraph
	tensors  []*Tensor
	branches map[int]*Interpreter
}

func New(m *Model) (*Interpreter, error) {
	return build(m, 0, map[int]*Interpreter{})
}

func build(m *Model, at int, built map[int]*Interpreter) (*Interpreter, error) {
	if at < 0 || at >= len(m.Subgraphs) {
		return nil, fmt.Errorf("tflite: subgraph %d of %d", at, len(m.Subgraphs))
	}
	if in, ok := built[at]; ok {
		return in, nil
	}
	g := m.Subgraphs[at]
	in := &Interpreter{
		model:    m,
		graph:    g,
		tensors:  make([]*Tensor, len(g.Tensors)),
		branches: built,
	}
	built[at] = in

	for i, d := range g.Tensors {
		t := &Tensor{Name: d.Name, Type: d.Type, Shape: append([]int(nil), d.Shape...)}
		raw := m.buffer(d.Buffer)
		if raw != nil {
			if err := decode(t, raw, d); err != nil {
				return nil, fmt.Errorf("tensor %d (%s): %w", i, d.Name, err)
			}
			t.Const = true
		} else {
			switch d.Type {
			case Float32, Int32, Int64, Bool:
			default:
				return nil, fmt.Errorf(
					"tflite: tensor %d (%s) has unsupported type %s",
					i,
					d.Name,
					d.Type,
				)
			}
			t.resize(t.Shape)
		}
		in.tensors[i] = t
	}

	for i, o := range g.Ops {
		if o.Op == OpIf {
			for _, b := range []int{o.then, o.els} {
				if _, err := build(m, b, built); err != nil {
					return nil, err
				}
			}
			continue
		}
		if _, ok := kernels[o.Op]; !ok {
			return nil, fmt.Errorf("tflite: operator %d is %s, which is not implemented", i, o.Op)
		}
	}
	return in, nil
}

func (in *Interpreter) branch(o *OpDesc, ins, outs []*Tensor) error {
	if len(ins) == 0 || ins[0] == nil || len(ins[0].I32) == 0 {
		return fmt.Errorf("no condition")
	}
	b := in.branches[o.els]
	if ins[0].I32[0] != 0 {
		b = in.branches[o.then]
	}
	args := ins[1:]
	if len(args) != len(b.graph.Inputs) || len(outs) != len(b.graph.Outputs) {
		return fmt.Errorf(
			"branch takes %d inputs and gives %d outputs, the op has %d and %d",
			len(b.graph.Inputs), len(b.graph.Outputs), len(args), len(outs),
		)
	}
	for i, x := range args {
		y := b.Input(i)
		y.resize(x.Shape)
		copyData(y, x)
	}
	if err := b.Invoke(); err != nil {
		return err
	}
	for i, y := range outs {
		x := b.Output(i)
		y.resize(x.Shape)
		copyData(y, x)
	}
	return nil
}

func decode(t *Tensor, raw []byte, d *TensorDesc) error {
	switch t.Type {
	case Uint8, Int8:
		if d.Scale == 0 {
			return fmt.Errorf("%s constant without a quantization scale", t.Type)
		}
		unsigned := t.Type == Uint8 && d.ZeroPoint != 0
		t.Type = Float32
		t.F32 = make([]float32, len(raw))
		for i, b := range raw {
			// Hybrid models keep signed weights in UINT8 tensors with a zero point of 0.
			v := int64(int8(b))
			if unsigned {
				v = int64(b)
			}
			t.F32[i] = float32(v-d.ZeroPoint) * d.Scale
		}
	case Float32:
		n := len(raw) / 4
		t.F32 = make([]float32, n)
		for i := range t.F32 {
			t.F32[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[4*i:]))
		}
	case Int32:
		n := len(raw) / 4
		t.I32 = make([]int32, n)
		for i := range t.I32 {
			t.I32[i] = int32(binary.LittleEndian.Uint32(raw[4*i:]))
		}
	case Int64:
		n := len(raw) / 8
		t.I32 = make([]int32, n)
		for i := range t.I32 {
			t.I32[i] = int32(int64(binary.LittleEndian.Uint64(raw[8*i:])))
		}
	case Bool:
		t.I32 = make([]int32, len(raw))
		for i, b := range raw {
			if b != 0 {
				t.I32[i] = 1
			}
		}
	default:
		return fmt.Errorf("unsupported constant type %s", t.Type)
	}
	return nil
}

func (in *Interpreter) Tensor(i int) *Tensor { return in.tensors[i] }

func (in *Interpreter) Input(i int) *Tensor { return in.tensors[in.graph.Inputs[i]] }

func (in *Interpreter) Output(i int) *Tensor { return in.tensors[in.graph.Outputs[i]] }

func (in *Interpreter) ResizeInput(i int, shape []int) {
	in.Input(i).resize(shape)
}

func (in *Interpreter) Reset() {
	for _, t := range in.tensors {
		if t.Const {
			continue
		}
		clear(t.F32)
		clear(t.I32)
	}
}

func (in *Interpreter) Invoke() error {
	for i, o := range in.graph.Ops {
		ins := make([]*Tensor, len(o.Inputs))
		for j, idx := range o.Inputs {
			// An optional input, such as a missing bias, is encoded as index -1.
			if idx >= 0 {
				ins[j] = in.tensors[idx]
			}
		}
		outs := make([]*Tensor, len(o.Outputs))
		for j, idx := range o.Outputs {
			outs[j] = in.tensors[idx]
		}
		run := kernels[o.Op]
		if o.Op == OpIf {
			run = in.branch
		}
		if err := run(o, ins, outs); err != nil {
			return fmt.Errorf("tflite: op %d (%s): %w", i, o.Op, err)
		}
	}
	return nil
}

type kernel func(o *OpDesc, in, out []*Tensor) error

func activate(v float32, a Activation) float32 {
	switch a {
	case ActRelu:
		if v < 0 {
			return 0
		}
	case ActRelu1:
		if v < -1 {
			return -1
		}
		if v > 1 {
			return 1
		}
	case ActRelu6:
		if v < 0 {
			return 0
		}
		if v > 6 {
			return 6
		}
	case ActTanh:
		return float32(math.Tanh(float64(v)))
	}
	return v
}

// TFLite's SAME padding puts the smaller half before the data.
func padFor(stride, dilation, in, filter, out int) int {
	effective := (filter-1)*dilation + 1
	p := ((out-1)*stride + effective - in) / 2
	if p < 0 {
		return 0
	}
	return p
}

func outputSize(in, filter, stride, dilation int, samePad bool) int {
	if samePad {
		return (in + stride - 1) / stride
	}
	effective := (filter-1)*dilation + 1
	if in < effective {
		return 0
	}
	return (in-effective)/stride + 1
}
