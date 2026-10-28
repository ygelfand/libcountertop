package whistle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ygelfand/libcountertop/pkg/audio/capture"
	"github.com/ygelfand/libcountertop/pkg/audio/vad"
	"github.com/ygelfand/libcountertop/pkg/hook"
	"github.com/ygelfand/libcountertop/pkg/runtime/payload"
	"github.com/ygelfand/libcountertop/pkg/runtime/service"
)

type Source interface {
	Listen(string) (<-chan []int16, func())
}

type Cutter interface {
	Feed(samples []int16) []vad.Clip
	Reset()
}

type Options struct {
	Helper string
	Model  string
	Depth  int
	Source Source
	Paused func() bool
	Cutter Cutter
	Clips  string
}

type Transcript struct {
	Text      string
	Words     []Word
	Start     time.Duration
	End       time.Duration
	Took      time.Duration
	DecodeTPS float64
}

type Word struct {
	Text        string  `json:"word"`
	Start       float64 `json:"start"`
	End         float64 `json:"end"`
	Probability float64 `json:"probability"`
}

func (t Transcript) Confidence() (lowest, mean float64) {
	if len(t.Words) == 0 {
		return 0, 0
	}
	lowest = t.Words[0].Probability
	for _, w := range t.Words {
		lowest = min(lowest, w.Probability)
		mean += w.Probability
	}
	return lowest, mean / float64(len(t.Words))
}

type Client struct {
	Text hook.Hook[Transcript]

	opts Options

	mu       sync.Mutex
	keywords []string
	conn     *os.File

	saved int
}

const keptClips = 50

func New(o Options) *Client { return &Client{opts: o} }

func (c *Client) Name() string { return "voice" }

func (c *Client) Keywords(words []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keywords = append([]string(nil), words...)
	if c.conn == nil {
		return nil
	}
	return writeMessage(c.conn, msgKeywords, []byte(strings.Join(c.keywords, "\n")))
}

func (c *Client) Run(ctx context.Context) error {
	if c.opts.Source == nil {
		return errors.New("whistle: no microphone source")
	}
	return service.Keep(ctx, "voice", c.session)
}

func (c *Client) session(ctx context.Context) error {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return fmt.Errorf("whistle: socketpair: %w", err)
	}
	unix.CloseOnExec(fds[0])
	unix.CloseOnExec(fds[1])
	if err := unix.SetNonblock(fds[0], true); err != nil {
		return fmt.Errorf(
			"whistle: socketpair: %w",
			errors.Join(err, unix.Close(fds[0]), unix.Close(fds[1])),
		)
	}
	ours := os.NewFile(uintptr(fds[0]), "voice")
	theirs := os.NewFile(uintptr(fds[1]), "voice-helper")
	defer ours.Close()

	proc, err := payload.Start(payload.Command{
		Path:  c.opts.Helper,
		Args:  []string{c.opts.Model, strconv.Itoa(c.opts.Depth)},
		Files: []*os.File{theirs},
	})
	theirs.Close()
	if err != nil {
		return fmt.Errorf("whistle: %w", err)
	}
	defer func() {
		ours.Close()
		if err := proc.Wait(); err != nil {
			slog.Debug("voice helper exit", "err", err)
		}
	}()

	first, err := readMessage(ours)
	if err != nil {
		return fmt.Errorf("whistle: waiting for ready: %w", err)
	}
	depth, err := ready(first)
	if errors.Is(err, errVersion) {
		return service.Permanent(err)
	}
	if err != nil {
		return err
	}
	slog.Info("voice helper ready", "depth", depth)

	c.mu.Lock()
	c.conn = ours
	if len(c.keywords) > 0 {
		err = writeMessage(ours, msgKeywords, []byte(strings.Join(c.keywords, "\n")))
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
	}()
	if err != nil {
		return fmt.Errorf("whistle: keywords: %w", err)
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
			return fmt.Errorf("whistle: helper: %w", err)
		}
		switch m.kind {
		case msgText:
			t, err := transcript(m)
			if err != nil {
				slog.Warn("voice transcript dropped", "err", err)
				continue
			}
			lowest, mean := t.Confidence()
			slog.Debug("voice heard",
				"text", t.Text,
				"lowest", fmt.Sprintf("%.3f", lowest),
				"mean", fmt.Sprintf("%.3f", mean),
				"words", wordProbabilities(t.Words),
				"start", t.Start,
				"length", t.End-t.Start,
				"took", t.Took,
				"decode_tps", t.DecodeTPS)
			c.Text.Emit(t)
		case msgFailed:
			slog.Warn("voice helper error", "err", string(m.payload))
		}
	}
}

func (c *Client) pump(ctx context.Context, conn *os.File) error {
	defer conn.Close()
	cut := c.opts.Cutter
	if cut == nil {
		s, err := vad.NewSegmenter()
		if err != nil {
			return err
		}
		cut = s
	}
	cut.Reset()
	frames, unlisten := c.opts.Source.Listen("voice")
	defer unlisten()
	for {
		select {
		case <-ctx.Done():
			return nil
		case frame, ok := <-frames:
			if !ok {
				return errors.New("whistle: the microphones stopped")
			}
			if c.opts.Paused != nil && c.opts.Paused() {
				cut.Reset()
				continue
			}
			for _, cl := range cut.Feed(frame) {
				c.keep(ctx, cl)
				c.mu.Lock()
				err := writeMessage(conn, msgClip, clip(cl))
				c.mu.Unlock()
				if err != nil {
					return err
				}
			}
		}
	}
}

func (c *Client) keep(ctx context.Context, cl vad.Clip) {
	if c.opts.Clips == "" || !slog.Default().Enabled(ctx, slog.LevelDebug) {
		return
	}
	if err := os.MkdirAll(c.opts.Clips, 0o755); err != nil {
		slog.Debug("voice clip not saved", "err", err)
		return
	}
	c.saved++
	path := filepath.Join(c.opts.Clips, fmt.Sprintf("clip-%02d.wav", c.saved%keptClips))
	if err := capture.Write(path, cl.Samples, vad.SampleRate, 1); err != nil {
		slog.Debug("voice clip not saved", "err", err)
		return
	}
	slog.Debug("voice clip saved", "path", path, "start", cl.Start)
}

func wordProbabilities(words []Word) string {
	parts := make([]string, len(words))
	for i, w := range words {
		parts[i] = fmt.Sprintf("%s:%.2f", strings.TrimSpace(w.Text), w.Probability)
	}
	return strings.Join(parts, " ")
}
