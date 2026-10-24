package whistle

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/audio/vad"
)

const helperEnv = "WHISTLE_FAKE_HELPER"

func TestMain(m *testing.M) {
	if mode := os.Getenv(helperEnv); mode != "" {
		os.Exit(fakeHelper(mode))
	}
	os.Exit(m.Run())
}

func fakeHelper(mode string) int {
	conn := os.NewFile(3, "voice")
	r := make([]byte, 8)
	if mode == "old" {
		binary.LittleEndian.PutUint32(r[0:], version+1)
	} else {
		binary.LittleEndian.PutUint32(r[0:], version)
	}
	binary.LittleEndian.PutUint32(r[4:], 2)
	if err := writeMessage(conn, msgReady, r); err != nil {
		return 1
	}
	keywords := ""
	for {
		m, err := readMessage(conn)
		if err != nil {
			return 0
		}
		switch m.kind {
		case msgKeywords:
			keywords = string(m.payload)
		case msgClip:
			head := make([]byte, 16)
			copy(head[0:8], m.payload[0:8])
			binary.LittleEndian.PutUint32(head[8:], 42)
			binary.LittleEndian.PutUint32(head[12:], 5025)
			text := fmt.Sprintf(`{"text":"%d bytes [%s]","language":"en"}`, len(m.payload)-8,
				strings.ReplaceAll(keywords, "\n", ","))
			if err := writeMessage(conn, msgText, append(head, text...)); err != nil {
				return 1
			}
			if mode == "die" {
				return 3
			}
		}
	}
}

type cutter struct{ held []int16 }

func (c *cutter) Feed(samples []int16) []vad.Clip {
	c.held = append(c.held, samples...)
	if len(c.held) < 320 {
		return nil
	}
	out := vad.Clip{Samples: c.held, Start: 100 * time.Millisecond, End: 900 * time.Millisecond}
	c.held = nil
	return []vad.Clip{out}
}

func (c *cutter) Reset() { c.held = nil }

type mic struct {
	mu     sync.Mutex
	frames chan []int16
	opens  int
}

func (m *mic) Listen(string) (<-chan []int16, func()) {
	m.mu.Lock()
	m.opens++
	m.mu.Unlock()
	return m.frames, func() {}
}

func client(t *testing.T, mode string) (*Client, *mic) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(helperEnv, mode)
	m := &mic{frames: make(chan []int16, 64)}
	return New(
		Options{Helper: self, Model: "model.cact", Depth: 2, Source: m, Cutter: &cutter{}},
	), m
}

func heard(t *testing.T, c *Client) <-chan Transcript {
	t.Helper()
	got := make(chan Transcript, 8)
	cancel := c.Text.Listen(func(tr Transcript) { got <- tr })
	t.Cleanup(cancel)
	return got
}

func wait(t *testing.T, got <-chan Transcript) Transcript {
	t.Helper()
	select {
	case tr := <-got:
		return tr
	case <-time.After(10 * time.Second):
		t.Fatal("no transcript")
	}
	return Transcript{}
}

func TestTranscriptsArriveWithKeywordsApplied(t *testing.T) {
	c, m := client(t, "ok")
	got := heard(t, c)
	if err := c.Keywords([]string{"radar", "next step"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	m.frames <- make([]int16, 320)
	tr := wait(t, got)

	if tr.Text != "640 bytes [radar,next step]" {
		t.Errorf("text %q, want the audio count with both keywords", tr.Text)
	}
	if tr.Start != 100*time.Millisecond || tr.End != 900*time.Millisecond ||
		tr.Took != 42*time.Millisecond {
		t.Errorf("times %v %v %v, want 100ms 900ms 42ms", tr.Start, tr.End, tr.Took)
	}
	if tr.DecodeTPS != 50.25 {
		t.Errorf("decode tps %v, want 50.25", tr.DecodeTPS)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run returned %v after cancel, want nil", err)
	}
}

func TestKeywordsChangeWhileRunning(t *testing.T) {
	c, m := client(t, "ok")
	got := heard(t, c)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = c.Run(ctx) }()

	m.frames <- make([]int16, 320)
	if tr := wait(t, got); tr.Text != "640 bytes []" {
		t.Fatalf("text %q, want no keywords yet", tr.Text)
	}
	if err := c.Keywords([]string{"stop"}); err != nil {
		t.Fatal(err)
	}
	m.frames <- make([]int16, 320)
	if tr := wait(t, got); tr.Text != "640 bytes [stop]" {
		t.Errorf("text %q, want the new keyword", tr.Text)
	}
}

func TestHelperRestartsAndGetsKeywordsAgain(t *testing.T) {
	c, m := client(t, "die")
	got := heard(t, c)
	if err := c.Keywords([]string{"radar"}); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	go func() { _ = c.Run(ctx) }()
	go func() {
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				select {
				case m.frames <- make([]int16, 320):
				default:
				}
			}
		}
	}()

	wait(t, got)
	if tr := wait(t, got); tr.Text != "640 bytes [radar]" {
		t.Errorf("after restart text %q, want keywords resent", tr.Text)
	}
	m.mu.Lock()
	opens := m.opens
	m.mu.Unlock()
	if opens < 2 {
		t.Errorf("microphone opened %d times, want one per helper", opens)
	}
}

func TestVersionMismatchStops(t *testing.T) {
	c, _ := client(t, "old")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Run(ctx); !errors.Is(err, errVersion) {
		t.Errorf("Run = %v, want %v", err, errVersion)
	}
}

func TestMissingHelperRetriesUntilCanceled(t *testing.T) {
	c := New(Options{Helper: "/nonexistent/voice", Source: &mic{frames: make(chan []int16)}})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := c.Run(ctx); err != nil {
		t.Errorf("Run = %v, want nil once canceled", err)
	}
}
