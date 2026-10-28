package surface

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

const opScreenRecord = 38

type Recording struct {
	file          net.Conn
	Width, Height int
}

type Encoded struct {
	Data  []byte
	Flags uint32
	At    time.Duration
}

func (c *Client) Record(w, h, rotation int) (*Recording, error) {
	if w <= 0 || h <= 0 || w > 4096 || h > 4096 || rotation < 0 || rotation > 3 {
		return nil, fmt.Errorf("surface: invalid recording geometry")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.send(opScreenRecord, uint32(w), uint32(h), uint32(rotation)); err != nil {
		return nil, err
	}
	ans, fd, err := c.recv(opScreenRecord)
	if err != nil {
		closeFD(fd)
		return nil, err
	}
	if len(ans) != 3 {
		closeFD(fd)
		return nil, fmt.Errorf("surface: short recording reply")
	}
	if st := Status(ans[0]); st != 0 {
		closeFD(fd)
		return nil, st
	}
	if fd < 0 || ans[1] == 0 || ans[2] == 0 || ans[1] > 4096 || ans[2] > 4096 {
		closeFD(fd)
		return nil, fmt.Errorf("surface: invalid recording reply")
	}
	f := os.NewFile(uintptr(fd), "screen recording")
	conn, err := net.FileConn(f)
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		_ = conn.Close()
		return nil, closeErr
	}
	return &Recording{file: conn, Width: int(ans[1]), Height: int(ans[2])}, nil
}

func (r *Recording) Read() (Encoded, error) {
	var header [16]byte
	if _, err := io.ReadFull(r.file, header[:]); err != nil {
		return Encoded{}, err
	}
	n := binary.LittleEndian.Uint32(header[:4])
	if n == 0 || n > 4<<20 {
		return Encoded{}, fmt.Errorf("surface: invalid recording frame size %d", n)
	}
	f := Encoded{Data: make([]byte, n), Flags: binary.LittleEndian.Uint32(header[4:8]),
		At: time.Duration(binary.LittleEndian.Uint64(header[8:])) * time.Microsecond}
	_, err := io.ReadFull(r.file, f.Data)
	return f, err
}

func (r *Recording) Keyframe() error {
	_, err := r.file.Write([]byte{'k'})
	return err
}

func (r *Recording) Close() error { return r.file.Close() }
