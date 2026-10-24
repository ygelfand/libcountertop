package payload

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
)

type Command struct {
	Path  string
	Args  []string
	Dir   string
	Env   []string
	Files []*os.File
	Log   string
	Binds []Bind

	OOMScore int
}

type Process struct {
	cmd    *exec.Cmd
	logged sync.WaitGroup

	mu     sync.Mutex
	exited bool
}

func Start(c Command) (*Process, error) {
	path, args, attr := c.Path, c.Args, attrs()
	if len(c.Binds) > 0 {
		var err error
		if path, args, attr, err = isolated(c); err != nil {
			return nil, err
		}
	}
	cmd := exec.Command(path, args...)
	cmd.Dir = c.Dir
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	cmd.ExtraFiles = c.Files
	cmd.SysProcAttr = attr
	p := &Process{cmd: cmd}
	if c.Log == "" {
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start %s: %w", c.Path, err)
		}
		p.score(c.OOMScore)
		return p, nil
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", c.Path, err)
	}
	cmd.Stdout, cmd.Stderr = w, w
	err = cmd.Start()
	w.Close()
	if err != nil {
		r.Close()
		return nil, fmt.Errorf("start %s: %w", c.Path, err)
	}
	p.score(c.OOMScore)
	p.logged.Go(func() { logLines(c.Log, r) })
	return p, nil
}

func (p *Process) score(n int) {
	if n == 0 {
		return
	}
	at := "/proc/" + strconv.Itoa(p.cmd.Process.Pid) + "/oom_score_adj"
	if err := os.WriteFile(at, []byte(strconv.Itoa(n)), 0o644); err != nil {
		slog.Warn(
			"a helper's out-of-memory priority could not be set",
			"path",
			p.cmd.Path,
			"err",
			err,
		)
	}
}

func (p *Process) Signal(sig os.Signal) error { return p.cmd.Process.Signal(sig) }

func (p *Process) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.exited {
		killGroup(p.cmd.Process)
	}
}

func (p *Process) Wait() error {
	err := p.cmd.Wait()
	p.mu.Lock()
	p.exited = true
	killGroup(p.cmd.Process)
	p.mu.Unlock()
	p.logged.Wait()
	return err
}

var escape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func logLines(tag string, r io.ReadCloser) {
	defer r.Close()
	s := bufio.NewScanner(r)
	for s.Scan() {
		slog.Debug(tag, "line", escape.ReplaceAllString(s.Text(), ""))
	}
	if err := s.Err(); err != nil && !errors.Is(err, os.ErrClosed) {
		slog.Debug(tag+" log ended", "err", err)
	}
}
