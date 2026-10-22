package homecontrol

import (
	"context"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/shell"
	ha "github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/states"
	"github.com/ygelfand/libcountertop/pkg/hook"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

type Client interface {
	Entities(context.Context, ha.Filter) ([]ha.Entity, error)
	Labels(context.Context) ([]ha.Label, error)
	Call(context.Context, string, map[string]string) error
	Enable(bool)
	Probe()
	Watch(func(ha.Access)) func()
}
type States interface {
	Follow(string, string) *states.Value
}

type Dependencies struct {
	Settings storage.Section[config.Home]
	Client   Client
	States   States
	Shell    *shell.Shell
	Grid     config.TesseraGrid
}
type Engine struct {
	Speaking hook.Hook[config.HomeMode]
	Regrid   hook.Hook[config.TesseraGrid]
	Redrawn  hook.Hook[bool]

	options    Dependencies
	ctx        context.Context
	stop       context.CancelFunc
	cache      *cache
	board      *Board
	home       *Home
	selections []Selection
	knobs      *setting.Controls[config.Home]
	mu         sync.Mutex
	unwatch    func()
}

func New(o Dependencies) *Engine {
	ctx, stop := context.WithCancel(context.Background())
	e := &Engine{options: o, ctx: ctx, stop: stop, home: &Home{}}
	e.selections = []Selection{
		{Key: "lights", Filter: ha.Filter{Domains: []string{"light"}}, engine: e},
		{Key: "switches", Filter: ha.Filter{Domains: []string{"switch"}}, engine: e},
	}
	e.knobs = e.controls()
	e.cache = &cache{
		ctx:   ctx,
		got:   map[string]*result{},
		fetch: e.fetch,
		now:   time.Now,
		done:  o.Shell.Redraw,
	}
	e.board = &Board{
		engine: e,
		open:   map[string]bool{},
		values: map[string]*states.Value{},
		from:   e.cache,
		follow: o.States.Follow,
		call:   o.Client.Call,
		redraw: o.Shell.Redraw,
	}
	return e
}
func (e *Engine) Name() string            { return "home control" }
func (e *Engine) Selections() []Selection { return append([]Selection(nil), e.selections...) }
func (e *Engine) Run(ctx context.Context) error {
	e.mu.Lock()
	if e.ctx.Err() != nil {
		e.mu.Unlock()
		return nil
	}
	if e.options.Client != nil {
		e.unwatch = e.options.Client.Watch(func(access ha.Access) {
			if access == ha.Allowed {
				e.cache.retry()
			}
			e.options.Shell.Redraw()
		})
	}
	e.mu.Unlock()
	defer e.Close()
	<-ctx.Done()
	return nil
}
func (e *Engine) Close() {
	e.cache.mu.Lock()
	e.cache.closed = true
	e.cache.mu.Unlock()
	e.board.mu.Lock()
	e.board.closed = true
	e.board.mu.Unlock()
	e.stop()
	e.mu.Lock()
	unwatch := e.unwatch
	e.unwatch = nil
	e.mu.Unlock()
	if unwatch != nil {
		unwatch()
	}
	e.cache.workers.Wait()
	e.board.workers.Wait()
	e.board.mu.Lock()
	unsubscribe := e.board.unsubscribe
	e.board.unsubscribe = nil
	e.board.mu.Unlock()
	for _, f := range unsubscribe {
		f()
	}
}

func (e *Engine) fetch(ctx context.Context, filter ha.Filter) ([]ha.Entity, []ha.Label, error) {
	entities, err := e.options.Client.Entities(ctx, filter)
	if err != nil {
		return nil, nil, err
	}
	labels, err := e.options.Client.Labels(ctx)
	return entities, labels, err
}
