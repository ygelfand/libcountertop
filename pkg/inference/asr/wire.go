package asr

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const Rate = 16000

const (
	version    = 1
	maxPayload = 1 << 20

	msgReady   = 'R'
	msgPhrases = 'P'
	msgAudio   = 'A'
	msgEnd     = 'E'
	msgPartial = 'p'
	msgFinal   = 'W'
	msgFailed  = 'F'
)

var errVersion = errors.New("asr: helper protocol version mismatch")

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
		return message{}, fmt.Errorf("asr: a %d byte message", n)
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

type ready struct {
	threads int
	weights int
}

func readyOf(m message) (ready, error) {
	if m.kind != msgReady || len(m.payload) < 12 {
		return ready{}, fmt.Errorf("asr: expected ready, got %q", rune(m.kind))
	}
	if v := binary.LittleEndian.Uint32(m.payload[0:]); v != version {
		return ready{}, fmt.Errorf("%w: helper %d, want %d", errVersion, v, version)
	}
	return ready{
		threads: int(binary.LittleEndian.Uint32(m.payload[4:])),
		weights: int(binary.LittleEndian.Uint32(m.payload[8:])),
	}, nil
}

type Token struct {
	Piece string  `json:"p"`
	At    int     `json:"ms"`
	Prob  float64 `json:"prob"`
}

type Transcript struct {
	Text   string
	Tokens []Token
	Audio  time.Duration
	Took   time.Duration
}

func (t Transcript) Confidence() (lowest, mean float64) {
	if len(t.Tokens) == 0 {
		return 0, 0
	}
	lowest = t.Tokens[0].Prob
	for _, k := range t.Tokens {
		lowest = min(lowest, k.Prob)
		mean += k.Prob
	}
	return lowest, mean / float64(len(t.Tokens))
}

func head(m message) (audio, took time.Duration, body []byte, err error) {
	if len(m.payload) < 8 {
		return 0, 0, nil, errors.New("asr: a text message without its header")
	}
	audio = time.Duration(binary.LittleEndian.Uint32(m.payload[0:])) * time.Millisecond
	took = time.Duration(binary.LittleEndian.Uint32(m.payload[4:])) * time.Millisecond
	return audio, took, m.payload[8:], nil
}

func partialOf(m message) (string, error) {
	_, _, body, err := head(m)
	return strings.TrimSpace(string(body)), err
}

func finalOf(m message) (Transcript, error) {
	audio, took, body, err := head(m)
	if err != nil {
		return Transcript{}, err
	}
	var j struct {
		Text   string  `json:"text"`
		Tokens []Token `json:"tokens"`
	}
	if err := json.Unmarshal(body, &j); err != nil {
		return Transcript{}, fmt.Errorf("asr: the final text: %w", err)
	}
	return Transcript{
		Text:   strings.TrimSpace(j.Text),
		Tokens: j.Tokens,
		Audio:  audio,
		Took:   took,
	}, nil
}
