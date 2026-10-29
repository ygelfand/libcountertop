package hci

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	component "github.com/ygelfand/libcountertop/pkg/runtime/startup"
)

type Radio struct {
	options RadioOptions
	mu      sync.Mutex
	port    *Port

	version Version
	local   Local
	can     Features

	sess *Session

	stop context.CancelFunc

	err error
}

type RadioOptions struct {
	BringUp func() (*Port, Version, error)
	Power   func(bool) (bool, error)
	Quirks  Quirks
}

type Quirks uint32

const (
	ControllerSetsParams Quirks = 1 << iota
)

func NewRadio(options RadioOptions) *Radio { return &Radio{options: options} }

func (r *Radio) Name() string { return "bluetooth" }

// A chip running its firmware does not answer the loader.
func (r *Radio) Start(parent context.Context) error {
	if _, err := r.options.Power(false); err != nil {
		return r.failed(err)
	}

	p, v, err := r.options.BringUp()
	if err != nil {
		return r.failed(err)
	}

	local, err := ReadLocal(p)
	if err != nil {
		_ = p.Close()
		return r.failed(err)
	}

	can, err := ReadFeatures(p)
	if err != nil {
		_ = p.Close()
		return r.failed(err)
	}

	ctx, stop := context.WithCancel(parent)
	sess := Reader(ctx, p)
	sess.quirks = r.options.Quirks

	r.mu.Lock()
	r.port, r.version, r.local, r.can, r.err = p, v, local, can, nil
	r.sess, r.stop = sess, stop
	r.mu.Unlock()
	return nil
}

func (r *Radio) Close() error {
	r.mu.Lock()
	p, stop, sess := r.port, r.stop, r.sess
	r.port, r.sess, r.stop = nil, nil, nil
	r.mu.Unlock()

	if stop != nil {
		stop()
	}
	if sess != nil {
		sess.Wait()
	}
	if p != nil {
		_ = p.Close()
	}
	_, err := r.options.Power(false)
	return err
}

func (r *Radio) Startup() component.Progress {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch {
	case r.err != nil:
		return component.Progress{Failed: true, Doing: r.err.Error()}
	case r.port == nil:
		return component.Progress{Doing: "starting Bluetooth"}
	}
	if r.sess != nil {
		if err := r.sess.Err(); err != nil {
			return component.Progress{Failed: true, Doing: err.Error()}
		}
	}
	return component.Progress{Done: true}
}

func (r *Radio) Up() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.port != nil && r.sess != nil && r.sess.Err() == nil
}

func (r *Radio) Can() Features {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.can
}

func (r *Radio) Controller() (Version, Local) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.version, r.local
}

func (r *Radio) Ask(cmd []byte, within time.Duration) (byte, []byte, error) {
	s, err := r.Line()
	if err != nil {
		return 0, nil, err
	}

	e, err := s.Ask(cmd, within)
	if err != nil {
		return 0, nil, err
	}
	return e.Code, e.Params, nil
}

func (r *Radio) Sniff(ctx context.Context, saw func(string)) error {
	s, err := r.Line()
	if err != nil {
		return err
	}

	r.mu.Lock()
	p := r.port
	r.mu.Unlock()

	saw(
		fmt.Sprintf(
			"the chip is marked %s",
			map[bool]string{true: "asleep", false: "awake"}[p.Asleep()],
		),
	)
	s.Sniff(ctx, saw)
	return nil
}

func (r *Radio) Line() (*Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.sess == nil {
		return nil, fmt.Errorf("ble: the radio is not up")
	}
	if err := r.sess.Err(); err != nil {
		return nil, err
	}
	return r.sess, nil
}

func (r *Radio) Scan(ctx context.Context, sc Scanning, found func(Advertisement)) error {
	s, err := r.Line()
	if err != nil {
		return err
	}
	return Scan(ctx, s, sc, found)
}

func (r *Radio) Connect(ctx context.Context, address [6]byte, addressType byte) (*Link, error) {
	s, err := r.Line()
	if err != nil {
		return nil, err
	}
	return s.Connect(ctx, address, addressType)
}

func (r *Radio) Pause() func() {
	s, err := r.Line()
	if err != nil {
		return func() {}
	}
	return s.Pause()
}

func (r *Radio) Speaker(ctx context.Context, name string, on Classic) error {
	s, err := r.Line()
	if err != nil {
		return err
	}

	if err := discoverable(s, name, true); err != nil {
		return err
	}

	defer func() {
		if err := discoverable(s, name, false); err != nil {
			slog.Warn("could not stop being discoverable", "err", err)
		}
	}()

	return Serve(ctx, s, on)
}

func (r *Radio) failed(err error) error {
	r.mu.Lock()
	r.err = err
	r.mu.Unlock()
	return err
}
