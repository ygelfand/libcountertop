package voice

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/inference/asr"
	"github.com/ygelfand/libcountertop/pkg/runtime/payload"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const namesEvery = 10 * time.Minute

type Options struct {
	Helper   string
	Binary   []byte
	ModelDir string
	Source   asr.Source
	Paused   func() bool
	Settings storage.Section[schema.Voice]
	Home     Home
	Peers    func() []string
	Retired  []string
}

type Listener struct {
	*Commands

	opts   Options
	engine *asr.Client

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewListener(o Options) *Listener {
	engine := asr.New(asr.Options{
		Helper:  o.Helper,
		Weights: asr.Weights(o.ModelDir),
		Threads: asr.Threads(),
		Source:  o.Source,
		Paused:  o.Paused,
	})
	l := &Listener{opts: o, engine: engine}
	l.Commands = NewCommands(
		engine,
		func(name string) bool { return o.Settings.Read().Command(name) },
	)
	engine.Final.Listen(func(t asr.Transcript) { l.Hear(t.Text) })
	return l
}

func (l *Listener) Engine() *asr.Client { return l.engine }

func (l *Listener) Name() string { return "voice" }

func (l *Listener) Listen(h Hook) func() {
	cancel := l.Commands.Listen(h)
	l.Apply()
	return func() {
		cancel()
		l.Apply()
	}
}

func (l *Listener) Apply() {
	l.Refresh()
	if l.Live() {
		l.Start()
	} else {
		l.Stop()
	}
	l.mu.Lock()
	running := l.cancel != nil
	l.mu.Unlock()
	if running {
		go l.names()
	}
}

func (l *Listener) Start() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.wg.Go(func() { l.run(ctx) })
}

func (l *Listener) Stop() {
	l.mu.Lock()
	cancel := l.cancel
	l.cancel = nil
	l.mu.Unlock()
	if cancel != nil {
		cancel()
		l.wg.Wait()
	}
}

func (l *Listener) run(ctx context.Context) {
	if err := payload.Install(l.opts.Helper, l.opts.Binary); err != nil {
		slog.Error("speech helper not installed", "path", l.opts.Helper, "err", err)
		return
	}
	for _, pattern := range l.opts.Retired {
		retired, _ := filepath.Glob(pattern)
		for _, p := range retired {
			_ = os.Remove(p)
		}
	}
	backoff := 5 * time.Second
	for {
		_, err := asr.Fetch(ctx, l.opts.ModelDir)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return
		}
		slog.Warn("speech weights not fetched", "err", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, 10*time.Minute)
	}
	l.wg.Go(func() { l.keepNames(ctx) })
	if err := l.engine.Run(ctx); err != nil {
		slog.Error("voice commands stopped", "err", err)
	}
}

func (l *Listener) keepNames(ctx context.Context) {
	tick := time.NewTicker(namesEvery)
	defer tick.Stop()
	for {
		l.names()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (l *Listener) names() {
	if l.opts.Peers != nil {
		l.Phrases("peers", l.opts.Peers())
	}
	if l.opts.Home == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), homeAsk)
	defer cancel()
	names, err := HomeNames(ctx, l.opts.Home, l.opts.Settings.Read().Label)
	if err != nil {
		slog.Debug("voice names from Home Assistant not read", "err", err)
		return
	}
	l.Phrases("home", names)
}
