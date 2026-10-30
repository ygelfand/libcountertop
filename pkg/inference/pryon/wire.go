package pryon

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	version    = 2
	maxPayload = 1 << 16

	msgReady    = 'R'
	msgLoad     = 'L'
	msgUnload   = 'U'
	msgAudio    = 'A'
	msgLoaded   = 'K'
	msgDetected = 'D'
	msgFailed   = 'F'
)

var errVersion = errors.New("pryon: helper protocol version mismatch")

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
	head := make([]byte, 8)
	if _, err := io.ReadFull(r, head); err != nil {
		return message{}, err
	}
	n := binary.LittleEndian.Uint32(head[4:])
	if n > maxPayload {
		return message{}, fmt.Errorf("pryon: a %d byte message", n)
	}
	m := message{kind: binary.LittleEndian.Uint32(head[0:]), payload: make([]byte, n)}
	if _, err := io.ReadFull(r, m.payload); err != nil {
		return message{}, err
	}
	return m, nil
}

func pcm(samples []int16) []byte {
	out := make([]byte, 2*len(samples))
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[2*i:], uint16(s))
	}
	return out
}

func loadPayload(id, manifest, aux string) []byte {
	b := append(append([]byte(id), 0), manifest...)
	return append(append(b, 0), aux...)
}

type keywordConfig struct {
	Enabled     []string `json:"enabled-keyword-list"`
	Sensitivity int      `json:"sensitivityValue"`
}

func Aux(keyword string, sensitivity int) string {
	b, err := json.Marshal(struct {
		Keyword keywordConfig `json:"keyword"`
	}{keywordConfig{Enabled: []string{keyword}, Sensitivity: sensitivity}})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func checkReady(m message) error {
	if m.kind != msgReady || len(m.payload) < 4 {
		return fmt.Errorf("pryon: expected ready, got %q", rune(m.kind))
	}
	if v := binary.LittleEndian.Uint32(m.payload); v != version {
		return fmt.Errorf("%w: helper %d, want %d", errVersion, v, version)
	}
	return nil
}

type loaded struct {
	id     string
	status int32
}

func loadedOf(m message) (loaded, error) {
	if len(m.payload) < 4 {
		return loaded{}, errors.New("pryon: short loaded message")
	}
	return loaded{
		status: int32(binary.LittleEndian.Uint32(m.payload)),
		id:     string(m.payload[4:]),
	}, nil
}

type Detection struct {
	ID    string
	Score float64
	Start uint32
	End   uint32
}

func detectedOf(m message) (Detection, error) {
	if len(m.payload) < 12 {
		return Detection{}, errors.New("pryon: short detection")
	}
	return Detection{
		Score: float64(binary.LittleEndian.Uint32(m.payload[0:])) / 1e6,
		Start: binary.LittleEndian.Uint32(m.payload[4:]),
		End:   binary.LittleEndian.Uint32(m.payload[8:]),
		ID:    string(bytes.TrimRight(m.payload[12:], "\x00")),
	}, nil
}
