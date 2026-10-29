package asr

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

const helperEnv = "ASR_FAKE_HELPER"

func TestMain(m *testing.M) {
	if mode := os.Getenv(helperEnv); mode != "" {
		os.Exit(fakeHelper(mode))
	}
	os.Exit(m.Run())
}

func fakeHelper(mode string) int {
	conn := os.NewFile(3, "speech")
	r := make([]byte, 12)
	v := uint32(version)
	if mode == "old" {
		v++
	}
	binary.LittleEndian.PutUint32(r[0:], v)
	binary.LittleEndian.PutUint32(r[4:], 3)
	binary.LittleEndian.PutUint32(r[8:], size)
	if err := writeMessage(conn, msgReady, r); err != nil {
		return 1
	}
	phrases, audio := "", 0
	for {
		m, err := readMessage(conn)
		if err != nil {
			return 0
		}
		switch m.kind {
		case msgPhrases:
			phrases = string(m.payload)
		case msgAudio:
			audio += len(m.payload)
			head := make([]byte, 8)
			if err := writeMessage(conn, msgPartial, append(head, " hearing"...)); err != nil {
				return 1
			}
		case msgEnd:
			head := make([]byte, 8)
			binary.LittleEndian.PutUint32(head[0:], uint32(audio/32))
			binary.LittleEndian.PutUint32(head[4:], 7)
			body := fmt.Sprintf(`{"text":" heard [%s]","tokens":[{"p":" heard","ms":400}]}`,
				strings.ReplaceAll(phrases, "\n", ","))
			if err := writeMessage(conn, msgFinal, append(head, body...)); err != nil {
				return 1
			}
			audio = 0
		}
	}
}

func speech(t *testing.T) []int16 {
	t.Helper()
	raw, err := os.ReadFile("../../audio/vad/testdata/speech.raw")
	if err != nil {
		t.Fatal(err)
	}
	pcm := make([]int16, len(raw)/2)
	for i := range pcm {
		pcm[i] = int16(binary.LittleEndian.Uint16(raw[i*2:]))
	}
	return pcm
}

type mic struct{ frames chan []int16 }

func (m *mic) Listen(string) (<-chan []int16, func()) { return m.frames, func() {} }

func (m *mic) play(samples []int16) {
	for len(samples) > 0 {
		n := min(320, len(samples))
		m.frames <- samples[:n]
		samples = samples[n:]
	}
}

func client(t *testing.T, mode string) (*Client, *mic) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(helperEnv, mode)
	m := &mic{frames: make(chan []int16, 4096)}
	return New(Options{Helper: self, Weights: "weights.bin", Threads: 3, Source: m}), m
}

func TestAnUtteranceComesBackWithItsPhrases(t *testing.T) {
	c, m := client(t, "ok")
	finals := make(chan Transcript, 4)
	t.Cleanup(c.Final.Listen(func(tr Transcript) { finals <- tr }))
	partials := make(chan string, 256)
	t.Cleanup(c.Partial.Listen(func(s string) {
		select {
		case partials <- s:
		default:
		}
	}))
	if err := c.Keywords([]string{"radar", "kitchen lights"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	m.play(make([]int16, 16000))
	m.play(speech(t))
	m.play(make([]int16, 24000))

	select {
	case tr := <-finals:
		if tr.Text != "heard [kitchen lights,radar]" {
			t.Errorf("text %q, want the phrases applied before the audio", tr.Text)
		}
		if tr.Took != 7*time.Millisecond || tr.Audio < time.Second {
			t.Errorf("took %v audio %v, want 7ms and the speech's length", tr.Took, tr.Audio)
		}
		if len(tr.Tokens) != 1 || tr.Tokens[0].At != 400 {
			t.Errorf("tokens %+v", tr.Tokens)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no final text")
	}
	select {
	case p := <-partials:
		if p != "hearing" {
			t.Errorf("partial %q, want it trimmed", p)
		}
	default:
		t.Error("no partial text while speaking")
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run after cancel: %v", err)
	}
}

func TestSilenceSendsNothing(t *testing.T) {
	c, m := client(t, "ok")
	finals := make(chan Transcript, 4)
	t.Cleanup(c.Final.Listen(func(tr Transcript) { finals <- tr }))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	m.play(make([]int16, 3*16000))
	select {
	case tr := <-finals:
		t.Errorf("a final %q from silence", tr.Text)
	case <-time.After(500 * time.Millisecond):
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run after cancel: %v", err)
	}
}

func TestAnOlderHelperStopsTheClient(t *testing.T) {
	c, _ := client(t, "old")
	err := c.Run(context.Background())
	if !errors.Is(err, errVersion) {
		t.Fatalf("Run with an older helper: %v, want the version mismatch", err)
	}
}
