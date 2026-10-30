// Package pryon runs Amazon's on-device Pryon wake word engine in a helper process and lists its keyword models.
package pryon

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ygelfand/libcountertop/pkg/runtime/payload"
)

const (
	readyWait = 20 * time.Second
	loadWait  = 20 * time.Second
)

type Options struct {
	Helper  string
	Library string
}

type Client struct {
	detected func(Detection)

	proc *payload.Process
	conn *os.File

	writing sync.Mutex

	mu      sync.Mutex
	waiting map[string]chan int32
	done    chan struct{}
}

func Start(o Options, detected func(Detection)) (*Client, error) {
	if o.Helper == "" || o.Library == "" {
		return nil, errors.New("pryon: no helper or library")
	}
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return nil, fmt.Errorf("pryon: socketpair: %w", err)
	}
	unix.CloseOnExec(fds[0])
	unix.CloseOnExec(fds[1])
	ours := os.NewFile(uintptr(fds[0]), "pryon")
	theirs := os.NewFile(uintptr(fds[1]), "pryon-helper")

	cmd := payload.Command{
		Path:  o.Helper,
		Args:  []string{o.Library},
		Files: []*os.File{theirs},
		Log:   "pryon helper",
		Env:   []string{"LD_LIBRARY_PATH=" + filepath.Dir(o.Library)},
	}
	proc, err := payload.Start(cmd)
	theirs.Close()
	if err != nil {
		ours.Close()
		return nil, fmt.Errorf("pryon: %w", err)
	}
	c := &Client{
		detected: detected,
		proc:     proc,
		conn:     ours,
		waiting:  map[string]chan int32{},
		done:     make(chan struct{}),
	}

	ready := make(chan error, 1)
	go func() {
		first, err := readMessage(ours)
		if err == nil {
			err = checkReady(first)
		}
		ready <- err
		if err == nil {
			c.read()
		}
	}()
	select {
	case err = <-ready:
	case <-time.After(readyWait):
		err = errors.New("pryon: the helper never became ready")
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	slog.Info("pryon helper ready", "library", o.Library)
	return c, nil
}

func (c *Client) read() {
	defer close(c.done)
	for {
		m, err := readMessage(c.conn)
		if err != nil {
			slog.Debug("pryon helper gone", "err", err)
			return
		}
		switch m.kind {
		case msgLoaded:
			l, err := loadedOf(m)
			if err != nil {
				slog.Warn("pryon load reply dropped", "err", err)
				continue
			}
			c.mu.Lock()
			if ch, ok := c.waiting[l.id]; ok {
				ch <- l.status
				delete(c.waiting, l.id)
			}
			c.mu.Unlock()
		case msgDetected:
			d, err := detectedOf(m)
			if err != nil {
				slog.Warn("pryon detection dropped", "err", err)
				continue
			}
			if c.detected != nil {
				c.detected(d)
			}
		case msgFailed:
			slog.Warn("pryon helper error", "err", string(m.payload))
		}
	}
}

func (c *Client) send(kind uint32, payload []byte) error {
	c.writing.Lock()
	defer c.writing.Unlock()
	return writeMessage(c.conn, kind, payload)
}

func (c *Client) Load(id, manifest, aux string) error {
	reply := make(chan int32, 1)
	c.mu.Lock()
	c.waiting[id] = reply
	c.mu.Unlock()
	if err := c.send(msgLoad, loadPayload(id, manifest, aux)); err != nil {
		return fmt.Errorf("pryon: load %s: %w", id, err)
	}
	select {
	case status := <-reply:
		if status != 0 {
			return fmt.Errorf("pryon: load %s refused (%d)", id, status)
		}
		return nil
	case <-c.done:
		return fmt.Errorf("pryon: load %s: the helper stopped", id)
	case <-time.After(loadWait):
		c.mu.Lock()
		delete(c.waiting, id)
		c.mu.Unlock()
		return fmt.Errorf("pryon: load %s timed out", id)
	}
}

func (c *Client) Unload(id string) {
	if err := c.send(msgUnload, []byte(id)); err != nil {
		slog.Debug("pryon unload failed", "id", id, "err", err)
	}
}

func (c *Client) Push(samples []int16) error { return c.send(msgAudio, pcm(samples)) }

func (c *Client) Alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

func (c *Client) Close() {
	c.conn.Close()
	c.proc.Stop()
	if err := c.proc.Wait(); err != nil {
		slog.Debug("pryon helper exit", "err", err)
	}
}
