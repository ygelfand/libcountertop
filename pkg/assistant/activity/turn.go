package activity

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/runtime/component"
)

const (
	Version = "1"

	speakerWait = 3 * time.Second
	unknown     = "Unknown"
)

type Outcome string

const (
	Completed Outcome = "completed"
	Cancelled Outcome = "cancelled"
	Timeout   Outcome = "timeout"
	Failed    Outcome = "failed"
)

type Turn struct {
	mu   sync.Mutex
	opts Options

	id   string
	slot int
	word string

	heard string
	reply string

	audio float64

	speaker  string
	person   string
	score    float64
	scored   bool
	settled  chan struct{}
	awaiting bool
	how      Outcome
	sent     bool

	listening time.Time
	thinking  time.Time
	replying  time.Time
	ended     time.Time
}

func (l *Log) Begin(slot int, word string) *Turn {
	t := &Turn{id: newID(), slot: slot, word: word, opts: l.opts, settled: make(chan struct{})}
	l.mu.Lock()
	l.now = t
	l.mu.Unlock()
	return t
}

type Match string

const (
	NoTurn  Match = "none"
	Pending Match = "pending"
	Known   Match = "known"
	Unknown Match = "unknown"
)

type Speaker struct {
	Turn   string  `json:"turn,omitempty"`
	Match  Match   `json:"match"`
	Name   string  `json:"name,omitempty"`
	Person string  `json:"person,omitempty"`
	Score  float64 `json:"score,omitempty"`
}

func (t *Turn) who() Speaker {
	if t == nil {
		return Speaker{Match: NoTurn}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case !t.ended.IsZero():
		return Speaker{Match: NoTurn}
	case t.scored && t.speaker != "":
		return Speaker{Turn: t.id, Match: Known, Name: t.speaker, Person: t.person, Score: t.score}
	case t.scored:
		return Speaker{Turn: t.id, Match: Unknown, Score: t.score}
	case t.awaiting && !t.isSettled():
		return Speaker{Turn: t.id, Match: Pending}
	}
	return Speaker{Turn: t.id, Match: Unknown}
}

func (t *Turn) Listening() {
	if t == nil {
		return
	}
	t.at(&t.listening)
}

func (t *Turn) Heard(text string) {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.ended.IsZero() {
		return
	}
	t.heard = text
	if t.thinking.IsZero() {
		t.thinking = time.Now()
	}
}

func (t *Turn) Replying(text string) {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.ended.IsZero() {
		return
	}
	t.reply = text
	if t.replying.IsZero() {
		t.replying = time.Now()
	}
}

func (t *Turn) Ends(how Outcome) {
	if t == nil {
		return
	}

	t.mu.Lock()
	if !t.ended.IsZero() {
		t.mu.Unlock()
		return
	}
	t.ended, t.how = time.Now(), how
	waits := t.awaiting && !t.isSettled()
	t.mu.Unlock()

	if waits {
		time.AfterFunc(speakerWait, t.send)
		return
	}
	t.send()
}

func (t *Turn) Awaits() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.awaiting = true
}

func (t *Turn) Spoke(name, person string, score float64) {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.scored || t.sent {
		t.mu.Unlock()
		return
	}
	t.speaker, t.person, t.score, t.scored = name, person, score, true
	t.settle()
	over := !t.ended.IsZero()
	t.mu.Unlock()
	if over {
		t.send()
	}
}

func (t *Turn) Missed() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.settle()
	over := !t.ended.IsZero()
	t.mu.Unlock()
	if over {
		t.send()
	}
}

func (t *Turn) settle() {
	if !t.isSettled() {
		close(t.settled)
	}
}

func (t *Turn) isSettled() bool {
	select {
	case <-t.settled:
		return true
	default:
		return false
	}
}

func (t *Turn) send() {
	t.mu.Lock()
	if t.sent {
		t.mu.Unlock()
		return
	}
	t.sent = true
	fields := t.fields(t.how)
	t.mu.Unlock()

	if t.opts.Emit != nil {
		t.opts.Emit(fields)
	}
}

func (t *Turn) Records(seconds float64) {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.audio = seconds
}

func (t *Turn) ID() string {
	if t == nil {
		return ""
	}
	return t.id
}

func (t *Turn) at(when *time.Time) {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.ended.IsZero() && when.IsZero() {
		*when = time.Now()
	}
}

func (t *Turn) fields(how Outcome) map[string]string {
	out := map[string]string{
		"version": Version,
		"id":      t.id,
		"slot":    strconv.Itoa(t.slot),
		"outcome": string(how),
	}

	if t.word != "" {
		out["wake_word"] = component.FitEvent(t.word)
	}
	if t.heard != "" {
		out["heard"] = component.FitEvent(t.heard)
	}
	if t.reply != "" {
		out["reply"] = component.FitEvent(t.reply)
	}
	if t.audio > 0 {
		out["audio_seconds"] = strconv.FormatFloat(t.audio, 'f', 1, 64)
	}
	if t.scored {
		out["speaker"] = unknown
		if t.speaker != "" {
			out["speaker"] = component.FitEvent(t.speaker)
		}
		if t.person != "" {
			out["speaker_person"] = t.person
		}
		out["speaker_score"] = strconv.FormatFloat(t.score, 'f', 2, 64)
	}

	marks := []struct {
		key string
		at  time.Time
	}{
		{"listen_ms", t.listening},
		{"think_ms", t.thinking},
		{"speak_ms", t.replying},
	}

	for i, mark := range marks {
		if mark.at.IsZero() {
			continue
		}

		until := t.ended
		for _, next := range marks[i+1:] {
			if !next.at.IsZero() {
				until = next.at
				break
			}
		}
		if ms := until.Sub(mark.at).Milliseconds(); ms > 0 {
			out[mark.key] = strconv.FormatInt(ms, 10)
		}
	}
	return out
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}
