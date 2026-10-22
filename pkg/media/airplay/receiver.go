package airplay

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"
	"golang.org/x/sys/unix"

	"github.com/ygelfand/libcountertop/pkg/media/playback"
	"github.com/ygelfand/libcountertop/pkg/runtime/payload"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

var (
	errVersion  = errors.New("airplay: helper speaks another protocol version")
	errNoScreen = errors.New("airplay: screen size not known")
)

type Options struct {
	Settings storage.Section[schema.AirPlay]
	Device   uint32
	Helper   string
	Binary   []byte
	Keyfile  string
	Name     func() string
	MAC      func() string
	Size     func() (width, height int)
	Largest  image.Point
	Output   func(ended func()) playback.Output
	Rate     int
	Channels int
}

type Receiver struct {
	opts  Options
	knobs *Controls

	mu     sync.Mutex
	cancel context.CancelFunc
	live   *screening
	wg     sync.WaitGroup
}

func NewReceiver(o Options) *Receiver {
	r := &Receiver{opts: o}
	r.knobs = &Controls{
		Table:  NewTable(),
		Device: o.Device,
		Read:   o.Settings.Read,
		Save:   r.save,
	}
	return r
}

func (r *Receiver) Name() string { return "airplay" }

func (r *Receiver) Entities() []esphome.Entity { return r.knobs.Entities() }

func (r *Receiver) Settings() *Controls { return r.knobs }

func (r *Receiver) Restore(c schema.AirPlay) {
	r.knobs.PublishFrom(c)
	if c.Enabled {
		r.Start()
	}
}

func (r *Receiver) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.wg.Go(func() { r.run(ctx) })
}

func (r *Receiver) Stop() {
	r.mu.Lock()
	cancel := r.cancel
	r.cancel = nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
		r.wg.Wait()
	}
}

func (r *Receiver) SetEnabled(on bool) {
	if err := r.knobs.Change("airplay", setting.OnOff(on)); err != nil {
		slog.Error("saving a setting failed", "setting", "media.airplay", "err", err)
	}
}

func (r *Receiver) Resize() {
	r.mu.Lock()
	live := r.live
	r.mu.Unlock()
	if live != nil {
		live.resized()
	}
}

func (r *Receiver) save(s Setting, v string) error {
	if err := setting.Store(r.opts.Settings, s, v); err != nil {
		return err
	}
	if r.opts.Settings.Read().Enabled {
		r.Start()
	} else {
		r.Stop()
	}
	return nil
}

func (r *Receiver) run(ctx context.Context) {
	if err := payload.Install(r.opts.Helper, r.opts.Binary); err != nil {
		slog.Error("airplay helper not installed", "path", r.opts.Helper, "err", err)
		return
	}
	backoff := time.Second
	for {
		began := time.Now()
		err := r.session(ctx)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			backoff = time.Second
			continue
		}
		if errors.Is(err, errVersion) {
			slog.Error("airplay stopped", "err", err)
			return
		}
		if time.Since(began) > time.Minute {
			backoff = time.Second
		}
		slog.Warn("airplay helper stopped", "err", err, "restart_in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, 30*time.Second)
	}
}

func (r *Receiver) session(ctx context.Context) error {
	w, h := r.opts.Size()
	if w <= 0 || h <= 0 {
		return errNoScreen
	}
	sentW, sentH := w, h
	if most := r.opts.Largest; most.X > 0 && most.Y > 0 {
		sentW, sentH = fit(w, h, most)
	}
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		return fmt.Errorf("airplay: socketpair: %w", err)
	}
	unix.CloseOnExec(fds[0])
	unix.CloseOnExec(fds[1])
	if err := unix.SetNonblock(fds[0], true); err != nil {
		return fmt.Errorf(
			"airplay: socketpair: %w",
			errors.Join(err, unix.Close(fds[0]), unix.Close(fds[1])),
		)
	}
	ours := os.NewFile(uintptr(fds[0]), "airplay")
	theirs := os.NewFile(uintptr(fds[1]), "airplay-helper")
	defer ours.Close()

	cmd := exec.Command(
		r.opts.Helper,
		r.opts.Name(),
		r.opts.MAC(),
		r.opts.Keyfile,
		strconv.Itoa(sentW),
		strconv.Itoa(sentH),
	)
	cmd.ExtraFiles = []*os.File{theirs}
	cmd.SysProcAttr = payload.DiesWithParent()
	cmd.Stderr = os.Stderr
	err = cmd.Start()
	theirs.Close()
	if err != nil {
		return fmt.Errorf("airplay: start %s: %w", r.opts.Helper, err)
	}
	var exited sync.WaitGroup
	exited.Go(func() {
		if err := cmd.Wait(); err != nil {
			slog.Debug("airplay helper exit", "err", err)
		}
	})
	defer exited.Wait()
	live, restart := context.WithCancel(ctx)
	defer restart()
	stop := context.AfterFunc(live, func() { ours.Close() })
	defer stop()

	first, err := readMessage(ours)
	if err != nil {
		return fmt.Errorf("airplay: waiting for ready: %w", err)
	}
	if first.kind != msgReady {
		return fmt.Errorf("airplay: helper opened with %q", rune(first.kind))
	}
	rd, err := readyOf(first.payload)
	if err != nil {
		return err
	}
	if rd.version != version {
		return fmt.Errorf("%w: %d", errVersion, rd.version)
	}
	slog.Info(
		"airplay ready",
		"name",
		r.opts.Name(),
		"port",
		rd.port,
		"size",
		fmt.Sprintf("%dx%d", sentW, sentH),
	)

	s := &screening{r: r, conn: ours, width: w, height: h, restart: restart}
	r.mu.Lock()
	r.live = s
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		if r.live == s {
			r.live = nil
		}
		r.mu.Unlock()
	}()
	defer s.over()
	for {
		m, err := readMessage(ours)
		if err != nil {
			if live.Err() != nil {
				return nil
			}
			return fmt.Errorf("airplay: helper: %w", err)
		}
		s.handle(ctx, m)
	}
}

type screening struct {
	r             *Receiver
	conn          *os.File
	width, height int
	restart       func()

	write  sync.Mutex
	client string

	mu     sync.Mutex
	shown  *mirror
	output playback.Output
}

func (s *screening) handle(ctx context.Context, m message) {
	switch m.kind {
	case msgClient:
		s.client = string(m.payload)
		slog.Info("airplay client", "name", s.client)
	case msgMirror:
		size, err := mirroringOf(m.payload)
		if err != nil || size.codec != codecH264 {
			slog.Warn("airplay mirror refused", "codec", size.codec, "err", err)
			s.disconnect()
			return
		}
		slog.Info(
			"airplay mirroring",
			"client",
			s.client,
			"size",
			fmt.Sprintf("%dx%d", size.width, size.height),
		)
		s.begin(size)
	case msgFrame:
		f, err := frameOf(m.payload)
		if err != nil {
			slog.Warn("airplay frame dropped", "err", err)
			return
		}
		s.mu.Lock()
		shown := s.shown
		s.mu.Unlock()
		if shown != nil {
			shown.push(ctx, f)
		}
	case msgAudio:
		a, err := audioOf(m.payload)
		if err != nil {
			slog.Warn("airplay audio dropped", "err", err)
			return
		}
		s.mu.Lock()
		shown := s.shown
		s.mu.Unlock()
		if shown != nil {
			shown.hear(a)
		}
	case msgEnded:
		slog.Info("airplay mirroring over", "client", s.client)
		s.over()
	}
}

func (s *screening) begin(size mirroring) {
	s.over()
	src := newMirror(s.client, size, s.r.opts.Rate, s.r.opts.Channels, s.disconnect)
	out := s.r.opts.Output(func() { s.ended(src) })
	s.mu.Lock()
	s.shown, s.output = src, out
	s.mu.Unlock()
	out.Play(src)
}

func (s *screening) ended(src *mirror) {
	s.mu.Lock()
	current := s.shown == src
	s.mu.Unlock()
	src.end()
	if current {
		s.disconnect()
	}
}

func (s *screening) over() {
	s.mu.Lock()
	src, out := s.shown, s.output
	s.shown, s.output = nil, nil
	s.mu.Unlock()
	if src == nil {
		return
	}
	src.end()
	out.Stop(src)
	s.resized()
}

func (s *screening) resized() {
	s.mu.Lock()
	showing := s.shown != nil
	s.mu.Unlock()
	if showing {
		return
	}
	w, h := s.r.opts.Size()
	if w <= 0 || h <= 0 || (w == s.width && h == s.height) {
		return
	}
	slog.Info("airplay screen resized", "size", fmt.Sprintf("%dx%d", w, h))
	s.restart()
}

func (s *screening) disconnect() {
	s.write.Lock()
	defer s.write.Unlock()
	if err := writeMessage(s.conn, msgDisconnect, nil); err != nil {
		slog.Debug("airplay disconnect not sent", "err", err)
	}
}
