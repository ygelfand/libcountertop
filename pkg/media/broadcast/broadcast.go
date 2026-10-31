package broadcast

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/actions"
	"github.com/ygelfand/libcountertop/pkg/audio/tone"
	"github.com/ygelfand/libcountertop/pkg/audio/vad"
	"github.com/ygelfand/libcountertop/pkg/media/pcm"
	"github.com/ygelfand/libcountertop/pkg/network/peer"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	Cap  = "broadcast"
	Path = "/broadcast"
	Rate = vad.SampleRate

	fromHeader = "X-Countertop-From"
	format     = "audio/L16; rate=16000; channels=1"

	longest     = 30 * time.Second
	firstWords  = 8 * time.Second
	quietAfter  = 2 * time.Second
	leadChunks  = 6
	startChunks = 3
	speechAbove = 0.5
	quietBelow  = 0.35
	fetchWait   = 20 * time.Second
	sendWait    = 15 * time.Second
	toneLevel   = 0.5
	shownExtra  = 2 * time.Second
)

var (
	ErrNothingSaid = errors.New("broadcast: nothing was said")
	ErrNobody      = errors.New("broadcast: no device takes broadcasts")
)

type Settings interface{ Read() schema.Call }

type Peers interface {
	Self() peer.Peer
	Peers() []peer.Peer
	Find(string) (peer.Peer, bool)
}

type Sound interface {
	Frames() (<-chan []int16, func())
	Play(pcm []int16)
	Claim(name string, run func(context.Context) error)
	Tone(schema.Stream, float64, tone.Sound)
}

type Tones interface {
	Sound(c, fallback schema.Chime) tone.Sound
}

type Turns interface {
	AfterTurn(func())
	Listen() (release func())
}

type Messages interface {
	Info(title, body string, hold time.Duration)
}

type Dependencies struct {
	Settings Settings
	Peers    Peers
	Sound    Sound
	Tones    Tones
	Turns    Turns
	Busy     func() bool
	Messages Messages
	Client   *http.Client
}

type Broadcaster struct {
	o         Dependencies
	recording sync.Mutex
}

func New(o Dependencies) *Broadcaster { return &Broadcaster{o: o} }

func (b *Broadcaster) Action() actions.Action {
	return actions.Action{
		Name:  "broadcast",
		Title: say.T("actions.broadcast"),
		Args: []esphome.Arg{
			{
				Name:        "message",
				Type:        esphome.ArgString,
				Description: "What to announce, or an http(s) URL of a clip; empty records the next thing said.",
			},
		},
		Speech: "message",
		Triggers: []actions.Trigger{
			{Sentence: "(broadcast|announce)"},
			{Sentence: "send a broadcast"},
			{Sentence: "(broadcast|announce) {message}"},
		},
		Run: b.run,
	}
}

func (b *Broadcaster) Handler() http.Handler { return http.HandlerFunc(b.received) }

func (b *Broadcaster) run(c esphome.Call) (string, error) {
	return b.say(strings.TrimSpace(c.String("message"))), nil
}

func (b *Broadcaster) say(msg string) string {
	switch {
	case msg == "":
		b.o.Turns.AfterTurn(b.speak)
		return say.T("actions.broadcast.ask")
	case strings.HasPrefix(msg, "http://"), strings.HasPrefix(msg, "https://"):
		safe.Go("broadcast clip", func() { b.relay(msg) })
		return ""
	}
	return say.T("actions.broadcast.text")
}

func (b *Broadcaster) speak() {
	if !b.recording.TryLock() {
		return
	}
	defer b.recording.Unlock()
	release := b.o.Turns.Listen()
	start := b.o.Tones.Sound(schema.ChimeChirp, schema.ChimeChirp)
	b.o.Sound.Tone(schema.StreamFeedback, toneLevel, start)
	time.Sleep(start.Length())
	samples, err := b.record(context.Background())
	release()
	if err != nil {
		slog.Info("broadcast not sent", "err", err)
		b.outcome(err)
		return
	}
	_, err = b.send(samples)
	b.outcome(err)
}

func (b *Broadcaster) relay(url string) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchWait)
	defer cancel()
	samples, err := pcm.Fetch(ctx, url, Rate)
	if err != nil {
		slog.Warn("fetching the broadcast failed", "err", err)
		b.outcome(err)
		return
	}
	_, err = b.send(samples)
	b.outcome(err)
}

func (b *Broadcaster) outcome(err error) {
	chime := schema.ChimeDing
	if err != nil {
		chime = schema.ChimeTrouble
	}
	b.o.Sound.Tone(schema.StreamFeedback, toneLevel, b.o.Tones.Sound(chime, chime))
}

func (b *Broadcaster) record(ctx context.Context) ([]int16, error) {
	d, err := vad.New()
	if err != nil {
		return nil, err
	}
	frames, stop := b.o.Sound.Frames()
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, longest)
	defer cancel()
	waiting := time.NewTimer(firstWords)
	defer waiting.Stop()

	r := recording{d: d, quietChunks: int(quietAfter * Rate / time.Second / vad.Chunk)}
	for {
		select {
		case <-ctx.Done():
			return r.finish()
		case <-waiting.C:
			if !r.started {
				return nil, ErrNothingSaid
			}
		case f, ok := <-frames:
			if !ok {
				return r.finish()
			}
			if r.feed(f) {
				return r.finish()
			}
		}
	}
}

type recording struct {
	d           *vad.Detector
	pending     []int16
	lead        [][]int16
	clip        []int16
	started     bool
	voiced      int
	quiet       int
	quietChunks int
}

func (r *recording) feed(f []int16) bool {
	r.pending = append(r.pending, f...)
	for len(r.pending) >= vad.Chunk {
		chunk := slices.Clone(r.pending[:vad.Chunk])
		r.pending = r.pending[vad.Chunk:]
		p, err := r.d.Speech(chunk)
		if err != nil {
			continue
		}
		if !r.started {
			r.lead = append(r.lead, chunk)
			if len(r.lead) > leadChunks {
				r.lead = r.lead[1:]
			}
			if p >= speechAbove {
				r.voiced++
			} else {
				r.voiced = 0
			}
			if r.voiced >= startChunks {
				r.started = true
				for _, c := range r.lead {
					r.clip = append(r.clip, c...)
				}
				r.lead = nil
			}
			continue
		}
		r.clip = append(r.clip, chunk...)
		switch {
		case p < quietBelow:
			r.quiet++
		case p >= speechAbove:
			r.quiet = 0
		}
		if r.quiet >= r.quietChunks {
			return true
		}
	}
	return false
}

func (r *recording) finish() ([]int16, error) {
	if !r.started {
		return nil, ErrNothingSaid
	}
	return r.clip, nil
}

func (b *Broadcaster) Targets() []peer.Peer {
	var out []peer.Peer
	for _, p := range b.o.Peers.Peers() {
		if p.Standing.Trusted() && slices.Contains(p.Caps, Cap) {
			out = append(out, p)
		}
	}
	return out
}

func (b *Broadcaster) send(samples []int16) (int, error) {
	targets := b.Targets()
	if len(targets) == 0 {
		return 0, ErrNobody
	}
	body := encode(samples)
	from := b.o.Peers.Self().ID
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		sent int
	)
	for _, p := range targets {
		wg.Go(func() {
			if err := b.post(p, from, body); err != nil {
				slog.Warn("broadcast not delivered", "to", p.Name, "err", err)
				return
			}
			mu.Lock()
			sent++
			mu.Unlock()
		})
	}
	wg.Wait()
	slog.Info("broadcast sent", "to", sent, "of", len(targets), "seconds", len(samples)/Rate)
	if sent == 0 {
		return 0, ErrNobody
	}
	return sent, nil
}

func (b *Broadcaster) post(p peer.Peer, from string, body []byte) error {
	addr, err := p.SecureAddr()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendWait)
	defer cancel()
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://"+addr+Path,
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", format)
	req.Header.Set(fromHeader, from)
	resp, err := b.o.Client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("broadcast: %s answered %d", p.Name, resp.StatusCode)
	}
	return nil
}

func (b *Broadcaster) received(w http.ResponseWriter, r *http.Request) {
	if !b.o.Settings.Read().Broadcasts {
		http.Error(w, "not taking broadcasts", http.StatusForbidden)
		return
	}
	if b.o.Busy != nil && b.o.Busy() {
		http.Error(w, "busy", http.StatusConflict)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, int64(longest/time.Second)*Rate*2+1))
	if err != nil || len(raw) < 2 || len(raw) > int(longest/time.Second)*Rate*2 {
		http.Error(w, "bad broadcast", http.StatusBadRequest)
		return
	}
	name := r.Header.Get(fromHeader)
	if p, ok := b.o.Peers.Find(name); ok {
		name = p.Name
	}
	w.WriteHeader(http.StatusAccepted)
	b.play(name, decode(raw))
}

func (b *Broadcaster) play(from string, samples []int16) {
	slog.Info("broadcast received", "from", from, "seconds", len(samples)/Rate)
	length := time.Duration(len(samples)) * time.Second / Rate
	if b.o.Messages != nil {
		b.o.Messages.Info(
			say.T("broadcast.title"),
			say.F("broadcast.from", map[string]any{"Peer": from}),
			length+shownExtra,
		)
	}
	chime := b.o.Tones.Sound(schema.ChimeDing, schema.ChimeDing)
	lead := make([]int16, int(chime.Length()*Rate/time.Second))
	b.o.Sound.Claim("broadcast", func(context.Context) error {
		b.o.Sound.Tone(schema.StreamAlerts, toneLevel, chime)
		b.o.Sound.Play(append(lead, samples...))
		return nil
	})
}

func encode(samples []int16) []byte {
	out := make([]byte, 2*len(samples))
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[2*i:], uint16(s))
	}
	return out
}

func decode(raw []byte) []int16 {
	out := make([]int16, len(raw)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(raw[2*i:]))
	}
	return out
}
