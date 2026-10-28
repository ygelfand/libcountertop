package surface

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestRecordingUsesSeparateSocket(t *testing.T) {
	f, path := newFake(t)
	c, err := Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	r, err := c.Record(480, 800, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if r.Width != 800 || r.Height != 480 {
		t.Fatalf("dimensions %dx%d", r.Width, r.Height)
	}
	if got := <-f.gl; len(got) != 3 || got[0] != 480 || got[1] != 800 || got[2] != 1 {
		t.Fatalf("request = %v", got)
	}
	encoder := <-f.shared
	t.Cleanup(func() { _ = encoder.Close() })
	frame := make([]byte, 19)
	binary.LittleEndian.PutUint32(frame, 3)
	binary.LittleEndian.PutUint32(frame[4:], 1)
	binary.LittleEndian.PutUint64(frame[8:], 12345)
	copy(frame[16:], []byte{1, 2, 3})
	if _, err := encoder.Write(frame); err != nil {
		t.Fatal(err)
	}
	got, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if got.Flags != 1 || got.At != 12345*time.Microsecond ||
		!bytes.Equal(got.Data, []byte{1, 2, 3}) {
		t.Fatalf("frame = %+v", got)
	}
	if err := r.Keyframe(); err != nil {
		t.Fatal(err)
	}
	var command [1]byte
	if _, err := io.ReadFull(encoder, command[:]); err != nil || command[0] != 'k' {
		t.Fatalf("keyframe command = %q, %v", command, err)
	}
	if err := c.Frame(1, 7, nil); err != nil {
		t.Fatal(err)
	}
	if got := <-f.frames; got[1] != 7 {
		t.Fatalf("display frame = %v", got)
	}
}

func TestRecordingRejectsOversizedFrame(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	r := &Recording{file: a}
	go func() {
		var header [16]byte
		binary.LittleEndian.PutUint32(header[:], 4<<20+1)
		_, _ = b.Write(header[:])
	}()
	if _, err := r.Read(); err == nil {
		t.Fatal("oversized frame accepted")
	}
}
