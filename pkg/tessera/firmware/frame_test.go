package firmware

import (
	"encoding/binary"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func lvglRotate(x, y, width, height, rotation int) (int, int) {
	switch rotation {
	case 90:
		return y, width - x - 1
	case 180:
		return width - x - 1, height - y - 1
	case 270:
		return height - y - 1, x
	}
	return x, y
}

func TestTouchInvertsLVGLRotation(t *testing.T) {
	const width, height = 600, 1024
	f, err := mapFrame(filepath.Join(t.TempDir(), "frame"), width, height)
	if err != nil {
		t.Fatal(err)
	}
	defer f.close()
	for _, rotation := range []int{0, 90, 180, 270} {
		atomic.StoreUint32(f.word(20), uint32(rotation))
		lw, lh := width, height
		if rotation == 90 || rotation == 270 {
			lw, lh = height, width
		}
		for _, p := range [][2]int{{0, 0}, {lw - 1, 0}, {0, lh - 1}, {lw - 1, lh - 1}, {123, 45}} {
			f.Touch(p[0], p[1], true)
			raw := atomic.LoadUint32(f.word(12))
			var b [4]byte
			binary.LittleEndian.PutUint32(b[:], raw)
			px, py := int(binary.LittleEndian.Uint16(b[0:])), int(binary.LittleEndian.Uint16(b[2:]))
			gx, gy := lvglRotate(px, py, width, height, rotation)
			if gx != p[0] || gy != p[1] {
				t.Errorf(
					"rotation %d: touch %v landed at panel (%d,%d), LVGL sees (%d,%d)",
					rotation,
					p,
					px,
					py,
					gx,
					gy,
				)
			}
		}
	}
	f.Touch(0, 0, false)
	if atomic.LoadUint32(f.word(16)) != 0 {
		t.Error("release left the panel touched")
	}
}
