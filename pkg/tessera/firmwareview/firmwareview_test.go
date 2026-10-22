package firmwareview

import (
	"bytes"
	"testing"
)

func TestChangedRowsFindsTheBandThatDiffers(t *testing.T) {
	const stride, rows = 8, 10
	a := make([]byte, stride*rows)
	b := bytes.Clone(a)
	if top, bottom := changedRows(a, b, stride); top < bottom {
		t.Errorf("identical frames changed rows %d..%d", top, bottom)
	}
	b[3*stride+1] = 1
	b[6*stride+7] = 1
	if top, bottom := changedRows(a, b, stride); top != 3 || bottom != 7 {
		t.Errorf("changed rows %d..%d, want 3..7", top, bottom)
	}
}

func TestToRGBAExpandsEveryChannelToFullRange(t *testing.T) {
	cases := []struct {
		pixel uint16
		want  [4]byte
	}{
		{0xffff, [4]byte{0xff, 0xff, 0xff, 0xff}},
		{0x0000, [4]byte{0, 0, 0, 0xff}},
		{0xf800, [4]byte{0xff, 0, 0, 0xff}},
		{0x07e0, [4]byte{0, 0xff, 0, 0xff}},
		{0x001f, [4]byte{0, 0, 0xff, 0xff}},
	}
	for _, c := range cases {
		var got [4]byte
		toRGBA(got[:], []byte{byte(c.pixel), byte(c.pixel >> 8)})
		if got != c.want {
			t.Errorf("%#04x: %v, want %v", c.pixel, got, c.want)
		}
	}
}
