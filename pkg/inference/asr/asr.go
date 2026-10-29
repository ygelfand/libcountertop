package asr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"

	"github.com/ygelfand/libcountertop/pkg/audio/vad"
	"github.com/ygelfand/libcountertop/pkg/hook"
	"github.com/ygelfand/libcountertop/pkg/runtime/payload"
	"github.com/ygelfand/libcountertop/pkg/runtime/service"
)

type Source interface {
	Listen(string) (<-chan []int16, func())
}

type Options struct {
	Helper  string
	Weights string
	Threads int
	Source  Source
	Paused  func() bool
}

type Client struct {
	Final   hook.Hook[Transcript]
	Partial hook.Hook[string]

	opts Options

	mu      sync.Mutex
	aliases aliases
	conn    *os.File
}

func New(o Options) *Client { return &Client{opts: o} }

func (c *Client) Name() string { return "speech" }

func (c *Client) Keywords(words []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.aliases = parseAliases(words)
	if c.conn == nil {
		return nil
	}
	return writeMessage(c.conn, msgPhrases, []byte(strings.Join(c.aliases.forms, "\n")))
}

func (c *Client) rewrite(text string) string {
	c.mu.Lock()
	a := c.aliases
	c.mu.Unlock()
	return a.rewrite(text)
}

func (c *Client) Run(ctx context.Context) error {
	if c.opts.Source == nil {
		return errors.New("asr: no microphone source")
	}
	return service.Keep(ctx, "speech", c.session)
}

func (c *Client) session(ctx context.Context) error {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return fmt.Errorf("asr: socketpair: %w", err)
	}
	unix.CloseOnExec(fds[0])
	unix.CloseOnExec(fds[1])
	if err := unix.SetNonblock(fds[0], true); err != nil {
		return fmt.Errorf(
			"asr: socketpair: %w",
			errors.Join(err, unix.Close(fds[0]), unix.Close(fds[1])),
		)
	}
	ours := os.NewFile(uintptr(fds[0]), "speech")
	theirs := os.NewFile(uintptr(fds[1]), "speech-helper")
	defer ours.Close()

	proc, err := payload.Start(payload.Command{
		Path:  c.opts.Helper,
		Args:  []string{c.opts.Weights, strconv.Itoa(max(c.opts.Threads, 1))},
		Files: []*os.File{theirs},
		Log:   "speech helper",
	})
	theirs.Close()
	if err != nil {
		return fmt.Errorf("asr: %w", err)
	}
	defer func() {
		ours.Close()
		if err := proc.Wait(); err != nil {
			slog.Debug("speech helper exit", "err", err)
		}
	}()

	first, err := readMessage(ours)
	if err != nil {
		return fmt.Errorf("asr: waiting for ready: %w", err)
	}
	r, err := readyOf(first)
	if errors.Is(err, errVersion) {
		return service.Permanent(err)
	}
	if err != nil {
		return err
	}
	slog.Info("speech helper ready", "threads", r.threads, "weights", r.weights)

	c.mu.Lock()
	c.conn = ours
	if len(c.aliases.forms) > 0 {
		err = writeMessage(ours, msgPhrases, []byte(strings.Join(c.aliases.forms, "\n")))
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
	}()
	if err != nil {
		return fmt.Errorf("asr: phrases: %w", err)
	}

	pumping, cancel := context.WithCancel(ctx)
	defer cancel()
	pumped := make(chan error, 1)
	go func() { pumped <- c.pump(pumping, ours) }()

	for {
		m, err := readMessage(ours)
		if err != nil {
			cancel()
			err = errors.Join(err, <-pumped)
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("asr: helper: %w", err)
		}
		c.handle(m)
	}
}

func (c *Client) handle(m message) {
	switch m.kind {
	case msgPartial:
		text, err := partialOf(m)
		if err != nil {
			slog.Warn("speech partial dropped", "err", err)
			return
		}
		c.Partial.Emit(c.rewrite(text))
	case msgFinal:
		t, err := finalOf(m)
		if err != nil {
			slog.Warn("speech final dropped", "err", err)
			return
		}
		t.Text = c.rewrite(t.Text)
		lowest, mean := t.Confidence()
		slog.Debug("voice heard",
			"text", t.Text,
			"lowest", fmt.Sprintf("%.3f", lowest),
			"mean", fmt.Sprintf("%.3f", mean),
			"audio", t.Audio,
			"flush", t.Took,
			"tokens", tokenScores(t.Tokens))
		c.Final.Emit(t)
	case msgFailed:
		slog.Warn("speech helper error", "err", string(m.payload))
	}
}

func (c *Client) send(kind uint32, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return errors.New("asr: the helper is gone")
	}
	return writeMessage(c.conn, kind, payload)
}

func (c *Client) pump(ctx context.Context, conn *os.File) error {
	defer conn.Close()
	gate, err := vad.NewGate()
	if err != nil {
		return err
	}
	frames, unlisten := c.opts.Source.Listen("speech")
	defer unlisten()
	for {
		select {
		case <-ctx.Done():
			return nil
		case frame, ok := <-frames:
			if !ok {
				if gate.Open() {
					return c.send(msgEnd, nil)
				}
				return errors.New("asr: the microphones stopped")
			}
			if c.opts.Paused != nil && c.opts.Paused() {
				if gate.Open() {
					if err := c.send(msgEnd, nil); err != nil {
						return err
					}
				}
				gate.Reset()
				continue
			}
			for _, e := range gate.Feed(frame) {
				if err := c.event(e); err != nil {
					return err
				}
			}
		}
	}
}

func (c *Client) event(e vad.Event) error {
	switch e.Moment {
	case vad.Speaking:
		return c.send(msgAudio, pcm(e.Samples))
	case vad.Ended:
		return c.send(msgEnd, nil)
	}
	return nil
}

func tokenScores(tokens []Token) string {
	parts := make([]string, len(tokens))
	for i, t := range tokens {
		parts[i] = fmt.Sprintf("%s:%.2f@%d", strings.TrimSpace(t.Piece), t.Prob, t.At)
	}
	return strings.Join(parts, " ")
}
