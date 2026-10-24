package firmware

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"

	"github.com/ygelfand/libcountertop/pkg/runtime/payload"
	"github.com/ygelfand/libcountertop/pkg/runtime/service"
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
	proc *payload.Process
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
	return service.Keep(ctx, "tessera", h.session)
}

func (h *Helper) session(ctx context.Context) error {
	env := []string{
		"HOME=" + h.opts.Dir,
		"TESSERA_FB=" + h.frame.path,
		"TESSERA_WIDTH=" + strconv.Itoa(h.opts.Width),
		"TESSERA_HEIGHT=" + strconv.Itoa(h.opts.Height),
		"TESSERA_DPI=" + strconv.Itoa(h.opts.DPI),
		"TESSERA_UPRIGHT=" + flag(h.opts.Upright),
		"TESSERA_LANGUAGE=" + h.opts.Language,
		"TESSERA_PRODUCT=" + h.opts.Product,
		"TESSERA_BOARD=" + h.opts.Board,
	}
	if h.opts.Port > 0 {
		env = append(env, "TESSERA_PORT="+strconv.Itoa(h.opts.Port))
	}
	if h.opts.Columns > 0 && h.opts.Rows > 0 {
		env = append(env, fmt.Sprintf("TESSERA_GRID=%dx%d,%dx%d",
			h.opts.Columns, h.opts.Rows, h.opts.Rows, h.opts.Columns))
	}
	proc, err := h.launch(env)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, proc.Stop)
	defer stop()
	err = proc.Wait()
	h.mu.Lock()
	h.proc = nil
	h.mu.Unlock()
	if err == nil {
		err = errors.New("tessera: helper exited")
	}
	return err
}

func (h *Helper) launch(env []string) (*payload.Process, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.opts.Prefs) > 0 {
		if err := os.WriteFile(h.prefsPath(), h.opts.Prefs, 0o600); err != nil {
			return nil, fmt.Errorf("tessera: %w", err)
		}
		env = append(env, "TESSERA_PREFS="+h.prefsPath())
	}
	proc, err := payload.Start(payload.Command{
		Path: h.opts.Helper,
		Dir:  h.opts.Dir,
		Env:  env,
		Log:  "tessera",
	})
	if err != nil {
		return nil, fmt.Errorf("tessera: %w", err)
	}
	h.proc = proc
	return proc, nil
}

func flag(on bool) string {
	if on {
		return "1"
	}
	return "0"
}
