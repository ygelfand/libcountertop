package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/compositor"
	"github.com/ygelfand/libcountertop/pkg/hook"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/runtime/service"
)

const (
	DefaultPort = 9222

	display     = "wayland-0"
	pagePrefix  = "countertop-page-"
	windowLimit = 15 * time.Second
)

type HostOptions struct {
	Dir         string
	State       string
	Port        int
	Args        []string
	Nameservers func() []string
	Network     func(context.Context) bool
	Screen      func() (width, height int)
	Tuning      *Tuning
}

type Host struct {
	o HostOptions

	Installing hook.Hook[Installation]

	mu         sync.Mutex
	up         *instance
	ready      chan struct{}
	count      int
	installing Installation
}

type Installation struct {
	Active bool
	At     float32
}

type Page struct {
	Window *compositor.Window
	Events hook.Hook[Event]

	in      *instance
	target  string
	session string
	done    chan struct{}
	ended   sync.Once
}

type Event struct {
	Method string
	Params json.RawMessage
}

type instance struct {
	conn *conn

	mu      sync.Mutex
	startup []string
	waiting map[string]chan *compositor.Window
	pages   map[string]*Page
	gone    bool
}

func NewHost(o HostOptions) *Host {
	if o.Port == 0 {
		o.Port = DefaultPort
	}
	return &Host{o: o, ready: make(chan struct{})}
}

func (h *Host) Name() string { return "cast engine" }

func (h *Host) Run(ctx context.Context) error {
	return service.Keep(ctx, "cast engine", h.run)
}

func (h *Host) Open(ctx context.Context, name string) (*Page, error) {
	in, err := h.wait(ctx)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	h.count++
	token := fmt.Sprintf("%s%s-%d", pagePrefix, name, h.count)
	h.mu.Unlock()
	return in.open(ctx, token)
}

func (h *Host) Installation() Installation {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.installing
}

func (h *Host) progress(at float32) {
	h.mu.Lock()
	same := h.installing.Active && int(h.installing.At*100) == int(at*100)
	h.mu.Unlock()
	if !same {
		h.install(Installation{Active: true, At: at})
	}
}

func (h *Host) install(i Installation) {
	h.mu.Lock()
	if h.installing == i {
		h.mu.Unlock()
		return
	}
	h.installing = i
	h.mu.Unlock()
	h.Installing.Emit(i)
}

func (h *Host) wait(ctx context.Context) (*instance, error) {
	for {
		h.mu.Lock()
		up, ready := h.up, h.ready
		h.mu.Unlock()
		if up != nil {
			return up, nil
		}
		select {
		case <-ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (h *Host) run(ctx context.Context) error {
	if h.o.Network != nil && !h.o.Network(ctx) {
		return ctx.Err()
	}
	root, err := Install(ctx, h.o.Dir, h.progress)
	h.install(Installation{})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if h.o.Tuning != nil {
		h.o.Tuning.Restarts(cancel)
	}

	var width, height int
	if h.o.Screen != nil {
		width, height = h.o.Screen()
	}
	socket := filepath.Join(h.o.State, display)
	comp := compositor.New(compositor.Options{Path: socket, Width: width, Height: height})
	in := &instance{waiting: map[string]chan *compositor.Window{}, pages: map[string]*Page{}}
	comp.Opened.Listen(in.opened)
	if err := comp.Listen(); err != nil {
		return err
	}
	safe.Go("cast compositor", func() {
		if err := comp.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("the cast compositor stopped", "err", err)
		}
	})

	args := slices.Clone(h.o.Args)
	if h.o.Tuning != nil {
		args = append(args, h.o.Tuning.Args()...)
	}
	safe.Go("cast engine", func() {
		defer cancel()
		err := session(ctx, Options{
			Root:        root,
			State:       h.o.State,
			Port:        h.o.Port,
			Display:     socket,
			Args:        args,
			Nameservers: h.o.Nameservers,
		})
		if ctx.Err() == nil {
			slog.Warn("the cast engine stopped", "err", err)
		}
	})
	if err := Ready(ctx, h.o.Port); err != nil {
		return err
	}
	c, err := dial(ctx, h.o.Port)
	if err != nil {
		return err
	}
	in.conn = c
	c.event = in.event
	stop := context.AfterFunc(ctx, c.close)
	defer stop()

	readErr := make(chan error, 1)
	safe.Go("cast engine devtools", func() { readErr <- c.read() })
	if err := in.discover(ctx); err != nil {
		c.close()
		return errors.Join(err, <-readErr)
	}

	h.mu.Lock()
	h.up = in
	close(h.ready)
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.up, h.ready = nil, make(chan struct{})
		h.mu.Unlock()
		in.down()
	}()
	slog.Info("the cast engine is ready")

	err = <-readErr
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (in *instance) discover(ctx context.Context) error {
	raw, err := in.conn.request(ctx, "", "Target.getTargets", nil)
	if err != nil {
		return err
	}
	var got struct {
		Targets []target `json:"targetInfos"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		return fmt.Errorf("engine: devtools: %w", err)
	}
	in.mu.Lock()
	for _, t := range got.Targets {
		if t.Type == "page" {
			in.startup = append(in.startup, t.ID)
		}
	}
	in.mu.Unlock()
	_, err = in.conn.request(ctx, "", "Target.setDiscoverTargets", map[string]any{"discover": true})
	return err
}

type target struct {
	ID     string `json:"targetId"`
	Type   string `json:"type"`
	URL    string `json:"url"`
	Opener string `json:"openerId"`
}

func (in *instance) open(ctx context.Context, token string) (*Page, error) {
	found := make(chan *compositor.Window, 1)
	in.mu.Lock()
	if in.gone {
		in.mu.Unlock()
		return nil, errors.New("engine: the cast engine stopped")
	}
	in.waiting[token] = found
	in.mu.Unlock()
	defer func() {
		in.mu.Lock()
		delete(in.waiting, token)
		in.mu.Unlock()
	}()

	raw, err := in.conn.request(ctx, "", "Target.createTarget", map[string]any{
		"url":       blank(token),
		"newWindow": true,
	})
	if err != nil {
		return nil, err
	}
	var created struct {
		ID string `json:"targetId"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		return nil, fmt.Errorf("engine: devtools: %w", err)
	}
	p := &Page{in: in, target: created.ID, done: make(chan struct{})}
	raw, err = in.conn.request(ctx, "", "Target.attachToTarget", map[string]any{
		"targetId": created.ID,
		"flatten":  true,
	})
	if err == nil {
		var attached struct {
			ID string `json:"sessionId"`
		}
		err = json.Unmarshal(raw, &attached)
		p.session = attached.ID
	}
	if err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("engine: devtools: %w", err)
	}
	in.mu.Lock()
	in.pages[p.session] = p
	in.mu.Unlock()

	limit := time.NewTimer(windowLimit)
	defer limit.Stop()
	select {
	case p.Window = <-found:
	case <-limit.C:
		_ = p.Close()
		return nil, fmt.Errorf("engine: no window for %s", token)
	case <-ctx.Done():
		_ = p.Close()
		return nil, ctx.Err()
	}

	in.mu.Lock()
	startup := in.startup
	in.startup = nil
	in.mu.Unlock()
	for _, id := range startup {
		_ = in.conn.call("", "Target.closeTarget", map[string]any{"targetId": id})
	}
	return p, nil
}

func (in *instance) opened(w *compositor.Window) {
	match := func(title string) {
		var found chan *compositor.Window
		in.mu.Lock()
		for token, ch := range in.waiting {
			if strings.Contains(title, token) {
				found = ch
				delete(in.waiting, token)
				break
			}
		}
		in.mu.Unlock()
		if found != nil {
			found <- w
		}
	}
	unlisten := w.Titled.Listen(match)
	w.Closed.Listen(func(struct{}) { unlisten() })
	match(w.Title())
}

func (in *instance) event(session, method string, params json.RawMessage) {
	if session != "" {
		in.mu.Lock()
		p := in.pages[session]
		in.mu.Unlock()
		if p != nil {
			p.Events.Emit(Event{Method: method, Params: params})
		}
		return
	}
	switch method {
	case "Target.targetCreated":
		var e struct {
			Info target `json:"targetInfo"`
		}
		if json.Unmarshal(params, &e) == nil && in.stray(e.Info) {
			slog.Info("the cast engine closes a window nothing asked for", "url", e.Info.URL)
			_ = in.conn.call("", "Target.closeTarget", map[string]any{"targetId": e.Info.ID})
		}
	case "Target.targetDestroyed":
		var e struct {
			ID string `json:"targetId"`
		}
		if json.Unmarshal(params, &e) == nil {
			in.endTarget(e.ID)
		}
	case "Target.detachedFromTarget":
		var e struct {
			Session string `json:"sessionId"`
		}
		if json.Unmarshal(params, &e) == nil {
			in.mu.Lock()
			p := in.pages[e.Session]
			delete(in.pages, e.Session)
			in.mu.Unlock()
			if p != nil {
				p.end()
			}
		}
	}
}

func (in *instance) stray(t target) bool { return t.Type == "page" && t.Opener != "" }

func (in *instance) endTarget(id string) {
	in.mu.Lock()
	var ended []*Page
	for s, p := range in.pages {
		if p.target == id {
			ended = append(ended, p)
			delete(in.pages, s)
		}
	}
	in.mu.Unlock()
	for _, p := range ended {
		p.end()
	}
}

func (in *instance) down() {
	in.mu.Lock()
	in.gone = true
	pages := in.pages
	in.pages = map[string]*Page{}
	in.mu.Unlock()
	for _, p := range pages {
		p.end()
	}
}

func blank(title string) string {
	return "data:text/html," + url.PathEscape("<title>"+title)
}

func (p *Page) Call(method string, params any) error {
	return p.in.conn.call(p.session, method, params)
}

func (p *Page) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return p.in.conn.request(ctx, p.session, method, params)
}

func (p *Page) Navigate(address string) error {
	return p.Call("Page.navigate", map[string]any{"url": address})
}

func (p *Page) Emulate(width, height int) error {
	return p.Call("Emulation.setDeviceMetricsOverride", map[string]any{
		"width": width, "height": height, "deviceScaleFactor": 1, "mobile": false,
	})
}

func (p *Page) Done() <-chan struct{} { return p.done }

func (p *Page) Close() error {
	return p.in.conn.call("", "Target.closeTarget", map[string]any{"targetId": p.target})
}

func (p *Page) end() { p.ended.Do(func() { close(p.done) }) }
