package firmware

import (
	"encoding/binary"
	"fmt"
	"image"
	"os"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	headerSize = 64
	magic      = 0x42465354
)

type Frame struct {
	path   string
	width  int
	height int
	mem    []byte
}

func mapFrame(path string, width, height int) (*Frame, error) {
	size := headerSize + width*height*2
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("tessera: %w", err)
	}
	defer f.Close()
	if err := f.Truncate(int64(size)); err != nil {
		return nil, fmt.Errorf("tessera: %w", err)
	}
	mem, err := unix.Mmap(int(f.Fd()), 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("tessera: map %s: %w", path, err)
	}
	clear(mem[:headerSize])
	return &Frame{path: path, width: width, height: height, mem: mem}, nil
}

func (f *Frame) Size() (width, height int) {
	wh := atomic.LoadUint32(f.word(4))
	return int(wh & 0xffff), int(wh >> 16)
}

func (f *Frame) Rotation() int { return int(atomic.LoadUint32(f.word(20))) }

type Turn struct {
	Count uint32
	Step  int
	From  int
	Area  image.Rectangle
}

func (f *Frame) Turn() Turn {
	t := Turn{Count: atomic.LoadUint32(f.word(24))}
	t.Step = int(int32(atomic.LoadUint32(f.word(28))))
	t.From = int(atomic.LoadUint32(f.word(44)))
	xy, wh := atomic.LoadUint32(f.word(32)), atomic.LoadUint32(f.word(36))
	x, y := int(xy&0xffff), int(xy>>16)
	t.Area = image.Rect(x, y, x+int(wh&0xffff), y+int(wh>>16))
	return t
}

func (f *Frame) Want(page int) {
	seq := atomic.LoadUint32(f.word(48))>>16 + 1
	atomic.StoreUint32(f.word(48), seq<<16|uint32(page)&0xffff)
}

func (f *Frame) Rest(on bool) {
	var v uint32
	if on {
		v = 1
	}
	atomic.StoreUint32(f.word(40), v)
}

func (f *Frame) Ready() bool { return atomic.LoadUint32(f.word(0)) == magic }

func (f *Frame) Seq() uint32 { return atomic.LoadUint32(f.word(8)) }

func (f *Frame) Copy(dst []byte) (uint32, bool) {
	before := f.Seq()
	if before%2 != 0 {
		return before, false
	}
	w, h := f.Size()
	if w == 0 || headerSize+w*h*2 > len(f.mem) {
		return before, false
	}
	copy(dst, f.mem[headerSize:headerSize+w*h*2])
	after := f.Seq()
	return after, after == before
}

func (f *Frame) Touch(x, y int, down bool) {
	if down {
		px, py := f.panel(x, y)
		var xy [4]byte
		binary.LittleEndian.PutUint16(xy[0:], uint16(clamp(px, f.width)))
		binary.LittleEndian.PutUint16(xy[2:], uint16(clamp(py, f.height)))
		atomic.StoreUint32(f.word(12), binary.LittleEndian.Uint32(xy[:]))
	}
	var state uint32
	if down {
		state = 1
	}
	atomic.StoreUint32(f.word(16), state)
}

func (f *Frame) panel(x, y int) (int, int) {
	switch f.Rotation() {
	case 90:
		return f.width - y - 1, x
	case 180:
		return f.width - x - 1, f.height - y - 1
	case 270:
		return y, f.height - x - 1
	}
	return x, y
}

func (f *Frame) word(offset int) *uint32 { return (*uint32)(unsafe.Pointer(&f.mem[offset])) }

func (f *Frame) close() error { return unix.Munmap(f.mem) }

func clamp(v, size int) int { return max(0, min(v, size-1)) }
