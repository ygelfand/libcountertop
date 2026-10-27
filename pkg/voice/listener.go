package voice

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/fetch"
	"github.com/ygelfand/libcountertop/pkg/inference/whistle"
	"github.com/ygelfand/libcountertop/pkg/runtime/payload"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const depth = 3

type Options struct {
	Helper   string
	Binary   []byte
	ModelDir string
	Client   *http.Client
	Source   whistle.Source
	Paused   func() bool
	Settings storage.Section[schema.Voice]
	Clips    string
}

type Listener struct {
	*Commands

	opts   Options
	engine *whistle.Client

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewListener(o Options) *Listener {
	if o.Client == nil {
		o.Client = fetch.Client(10 * time.Minute)
	}
	engine := whistle.New(whistle.Options{
		Helper: o.Helper,
		Model:  whistle.Whistle.Path(o.ModelDir),
		Depth:  depth,
		Source: o.Source,
		Paused: o.Paused,
		Clips:  o.Clips,
	})
	l := &Listener{opts: o, engine: engine}
	l.Commands = NewCommands(
		engine,
		func(name string) bool { return o.Settings.Read().Command(name) },
	)
	engine.Text.Listen(func(t whistle.Transcript) { l.Hear(t.Text) })
	return l
}

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
		slog.Error("voice helper not installed", "path", l.opts.Helper, "err", err)
		return
	}
	backoff := 5 * time.Second
	for {
		_, err := whistle.Whistle.Fetch(ctx, l.opts.Client, l.opts.ModelDir)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return
		}
		slog.Warn("voice model not fetched", "err", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, 10*time.Minute)
	}
	if err := l.engine.Run(ctx); err != nil {
		slog.Error("voice commands stopped", "err", err)
	}
}
