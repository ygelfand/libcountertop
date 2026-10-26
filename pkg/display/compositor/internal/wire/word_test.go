package wire

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestWordsRoundTripInHostOrder(t *testing.T) {
	var b bytes.Buffer
	for _, v := range []uint32{0, 1, 0xDEADBEEF, 0xFFFFFFFF} {
		b.Reset()
		putWord(&b, v)
		if b.Len() != 4 || binary.NativeEndian.Uint32(b.Bytes()) != v {
			t.Errorf("%#x wrote %x", v, b.Bytes())
		}
		got, err := readWord(&b)
		if err != nil || got != v {
			t.Errorf("%#x read back %#x %v", v, got, err)
		}
	}
	if _, err := readWord(bytes.NewReader([]byte{1, 2})); err == nil {
		t.Error("a short word read without an error")
	}
}

func TestFixedFractionKeepsTheLowByte(t *testing.T) {
	for _, c := range []struct {
		f    Fixed
		want int
	}{{FixedInt(3), 0}, {Fixed(0x0180), 0x80}, {Fixed(-1), 0xFF}} {
		if got := c.f.Frac(); got != c.want {
			t.Errorf("%#x.Frac() = %#x, want %#x", int32(c.f), got, c.want)
		}
	}
}

type object struct{}

func (*object) ID() uint32                    { return 7 }
func (*object) SetID(uint32)                  {}
func (*object) Dispatch(*MessageBuffer) error { return nil }
func (*object) Delete()                       {}

func TestTypedNilObjectsCountAsNil(t *testing.T) {
	var none *object
	if !isNil(nil) || !isNil(none) || !isNil(Object(none)) {
		t.Error("a nil object was not nil")
	}
	if isNil(&object{}) {
		t.Error("a real object was nil")
	}
}
