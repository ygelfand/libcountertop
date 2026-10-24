package service

import (
	"context"
	"errors"
	"time"
)

type Service interface {
	Name() string

	Run(ctx context.Context) error
}

type Starter interface {
	Start(ctx context.Context) error
}

type Closer interface {
	Close() error
}

type Reacquirer interface {
	Release() error
}

type State string

const (
	StateWaiting State = "waiting"

	StateRunning State = "running"

	StateRetrying State = "retrying"

	StateFailed State = "failed"

	StateStopped State = "stopped"
)

type Status struct {
	Name     string
	State    State
	Err      error
	Restarts int

	Since time.Time
}

func Healthy(all []Status) bool {
	for _, s := range all {
		if s.State == StateFailed || s.State == StateRetrying {
			return false
		}
	}
	return true
}

type policy struct {
	required bool

	restart bool

	backoff    time.Duration
	maxBackoff time.Duration

	steady time.Duration
}

type Option func(*policy)

func Required() Option {
	return func(p *policy) { p.required = true }
}

func Restart(initial, max time.Duration) Option {
	return func(p *policy) {
		p.restart = true
		p.backoff = initial
		p.maxBackoff = max
	}
}

func Once() Option {
	return func(p *policy) { p.restart = false }
}

type permanent struct{ err error }

func (p permanent) Error() string { return p.err.Error() }

func (p permanent) Unwrap() error { return p.err }

func Permanent(err error) error { return permanent{err: err} }

func isPermanent(err error) bool {
	var p permanent
	return errors.As(err, &p)
}

func defaults() policy {
	return policy{
		backoff:    time.Second,
		maxBackoff: 30 * time.Second,
		steady:     time.Minute,
	}
}
