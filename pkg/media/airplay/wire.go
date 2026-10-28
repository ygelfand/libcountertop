package airplay

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

const (
	version    = 2
	maxPayload = 4 << 20

	msgReady      = 'R'
	msgClient     = 'C'
	msgMirror     = 'M'
	msgFrame      = 'V'
	msgEnded      = 'E'
	msgDisconnect = 'X'
	msgAudio      = 'A'
	msgSound      = 'S'

	codecH264 = 1
)

type message struct {
	kind    uint32
	payload []byte
}

func writeMessage(w io.Writer, kind uint32, payload []byte) error {
	buf := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint32(buf[0:], kind)
	binary.LittleEndian.PutUint32(buf[4:], uint32(len(payload)))
	copy(buf[8:], payload)
	_, err := w.Write(buf)
	return err
}

func readMessage(r io.Reader) (message, error) {
	var head [8]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return message{}, err
	}
	m := message{kind: binary.LittleEndian.Uint32(head[0:])}
	n := binary.LittleEndian.Uint32(head[4:])
	if n > maxPayload {
		return message{}, fmt.Errorf("airplay: %d byte message", n)
	}
	m.payload = make([]byte, n)
	if _, err := io.ReadFull(r, m.payload); err != nil {
		return message{}, err
	}
	return m, nil
}

type ready struct {
	version, port uint32
}

func readyOf(p []byte) (ready, error) {
	if len(p) < 8 {
		return ready{}, fmt.Errorf("airplay: %d byte ready", len(p))
	}
	return ready{binary.LittleEndian.Uint32(p[0:]), binary.LittleEndian.Uint32(p[4:])}, nil
}

type mirroring struct {
	codec, width, height uint32
}

func mirroringOf(p []byte) (mirroring, error) {
	if len(p) < 12 {
		return mirroring{}, fmt.Errorf("airplay: %d byte mirror", len(p))
	}
	return mirroring{
		codec:  binary.LittleEndian.Uint32(p[0:]),
		width:  binary.LittleEndian.Uint32(p[4:]),
		height: binary.LittleEndian.Uint32(p[8:]),
	}, nil
}

type frame struct {
	at   time.Duration
	data []byte
}

func frameOf(p []byte) (frame, error) {
	if len(p) < 8 {
		return frame{}, fmt.Errorf("airplay: %d byte frame", len(p))
	}
	us := binary.LittleEndian.Uint64(p[0:])
	return frame{at: time.Duration(us) * time.Microsecond, data: p[8:]}, nil
}

type audio struct {
	rate, channels int
	pcm            []int16
}

func audioOf(p []byte) (audio, error) {
	if len(p) < 8 || (len(p)-8)%2 != 0 {
		return audio{}, fmt.Errorf("airplay: %d byte audio", len(p))
	}
	a := audio{
		rate:     int(binary.LittleEndian.Uint32(p[0:])),
		channels: int(binary.LittleEndian.Uint32(p[4:])),
		pcm:      make([]int16, (len(p)-8)/2),
	}
	if a.rate <= 0 || a.channels <= 0 {
		return audio{}, fmt.Errorf("airplay: audio at %d Hz x%d", a.rate, a.channels)
	}
	for i := range a.pcm {
		a.pcm[i] = int16(binary.LittleEndian.Uint16(p[8+2*i:]))
	}
	return a, nil
}
