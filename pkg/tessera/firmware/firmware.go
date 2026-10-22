package firmware

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/ygelfand/libcountertop/pkg/runtime/payload"
)

type Options struct {
	Helper   string
	Dir      string
	Port     int
	Width    int
	Height   int
	DPI      int
	Upright  bool
	Language string
	Product  string
	Columns  int
	Rows     int
	Board    string
	Prefs    []byte
}

type Helper struct {
	opts  Options
	frame *Frame

	mu   sync.Mutex
	proc *os.Process
}

func Open(o Options) (*Helper, error) {
	if o.Width <= 0 || o.Height <= 0 {
		return nil, fmt.Errorf("tessera: panel %dx%d", o.Width, o.Height)
	}
	if err := payload.Install(o.Helper, Binary); err != nil {
		return nil, fmt.Errorf("tessera: install %s: %w", o.Helper, err)
	}
	if err := os.MkdirAll(o.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("tessera: %w", err)
	}
	f, err := mapFrame(filepath.Join(o.Dir, "frame"), o.Width, o.Height)
	if err != nil {
		return nil, err
	}
	return &Helper{opts: o, frame: f}, nil
}

func (h *Helper) Frame() *Frame { return h.frame }

func (h *Helper) Close() error { return h.frame.close() }

func (h *Helper) Prefer(prefs []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.opts.Prefs = prefs
	if err := os.WriteFile(h.prefsPath(), prefs, 0o600); err != nil {
		return fmt.Errorf("tessera: %w", err)
	}
	if h.proc == nil {
		return nil
	}
	return h.proc.Signal(syscall.SIGUSR1)
}

func (h *Helper) prefsPath() string { return filepath.Join(h.opts.Dir, "prefs") }

func (h *Helper) Run(ctx context.Context) error {
	backoff := time.Second
	for {
		began := time.Now()
		err := h.session(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(began) > time.Minute {
			backoff = time.Second
		}
		slog.Warn("tessera helper stopped", "err", err, "restart_in", backoff)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, 30*time.Second)
	}
}

func (h *Helper) session(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, h.opts.Helper)
	cmd.Dir = h.opts.Dir
	cmd.Env = append(os.Environ(),
		"HOME="+h.opts.Dir,
		"TESSERA_FB="+h.frame.path,
		"TESSERA_WIDTH="+strconv.Itoa(h.opts.Width),
		"TESSERA_HEIGHT="+strconv.Itoa(h.opts.Height),
		"TESSERA_DPI="+strconv.Itoa(h.opts.DPI),
		"TESSERA_UPRIGHT="+flag(h.opts.Upright),
		"TESSERA_LANGUAGE="+h.opts.Language,
		"TESSERA_PRODUCT="+h.opts.Product,
		"TESSERA_BOARD="+h.opts.Board,
	)
	if h.opts.Port > 0 {
		cmd.Env = append(cmd.Env, "TESSERA_PORT="+strconv.Itoa(h.opts.Port))
	}
	if h.opts.Columns > 0 && h.opts.Rows > 0 {
		cmd.Env = append(cmd.Env, fmt.Sprintf("TESSERA_GRID=%dx%d,%dx%d",
			h.opts.Columns, h.opts.Rows, h.opts.Rows, h.opts.Columns))
	}
	cmd.SysProcAttr = payload.DiesWithParent()
	out, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("tessera: %w", err)
	}
	cmd.Stderr = cmd.Stdout
	if err := h.launch(cmd); err != nil {
		return err
	}
	logLines(out)
	err = cmd.Wait()
	h.mu.Lock()
	h.proc = nil
	h.mu.Unlock()
	if err == nil {
		err = errors.New("tessera: helper exited")
	}
	return err
}

func (h *Helper) launch(cmd *exec.Cmd) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.opts.Prefs) > 0 {
		if err := os.WriteFile(h.prefsPath(), h.opts.Prefs, 0o600); err != nil {
			return fmt.Errorf("tessera: %w", err)
		}
		cmd.Env = append(cmd.Env, "TESSERA_PREFS="+h.prefsPath())
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("tessera: start %s: %w", h.opts.Helper, err)
	}
	h.proc = cmd.Process
	return nil
}

func flag(on bool) string {
	if on {
		return "1"
	}
	return "0"
}

var escape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func logLines(r io.Reader) {
	s := bufio.NewScanner(r)
	for s.Scan() {
		slog.Debug("tessera", "line", escape.ReplaceAllString(s.Text(), ""))
	}
	if err := s.Err(); err != nil {
		slog.Debug("tessera log ended", "err", err)
	}
}
