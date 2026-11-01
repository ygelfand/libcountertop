package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"
	"github.com/ygelfand/go-esphome-device/api"
	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/libcountertop/pkg/hook"
	"github.com/ygelfand/libcountertop/pkg/runtime/startup"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const (
	probeWait = 5 * time.Second
	dropHold  = 10 * time.Second
	syncWait  = 30 * time.Second
)

type sender interface {
	Send(proto.Message) error
}

type reply struct {
	data []byte
	err  error
}

type link struct {
	access      Access
	integration Integration
	protocol    int
	stop        context.CancelFunc
}

type pending struct {
	ch chan reply
	to map[sender]struct{}
}

type Settings struct {
	Home storage.Section[schema.Home]
	API  storage.Section[schema.API]
}
type Controller struct {
	settings Settings
	ctx      context.Context
	stop     context.CancelFunc
	workers  sync.WaitGroup
	closed   bool

	Changed hook.Hook[Access]
	Linked  hook.Hook[struct{}]

	mu      sync.Mutex
	links   map[sender]*link
	dropped time.Time
	held    Access
	access  Access
	next    uint32
	waiting map[uint32]*pending
	born    time.Time
}

func New(settings Settings) *Controller {
	ctx, stop := context.WithCancel(context.Background())
	return &Controller{
		settings: settings,
		ctx:      ctx,
		stop:     stop,
		links:    map[sender]*link{},
		waiting:  map[uint32]*pending{},
		born:     time.Now(),
	}
}
func (h *Controller) Run(ctx context.Context) error { <-ctx.Done(); h.Close(); return nil }

func (h *Controller) FollowIntegration(f Follower) {
	f.Follow(VersionEntity, "").Reported.Listen(func(string) { h.Probe() })
}
func (h *Controller) Close() {
	h.mu.Lock()
	h.closed = true
	h.stop()
	for conn, l := range h.links {
		if l.stop != nil {
			l.stop()
		}
		delete(h.links, conn)
	}
	for id, p := range h.waiting {
		p.ch <- reply{err: ErrNotConnected}
		delete(h.waiting, id)
	}
	h.mu.Unlock()
	h.workers.Wait()
}

func (h *Controller) Name() string { return "homeassistant" }

func (h *Controller) Handle(_ context.Context, conn *esphome.Conn, msg proto.Message) error {
	switch m := msg.(type) {
	case *api.SubscribeHomeassistantServicesRequest:
		h.attach(conn)
	case *api.HomeassistantActionResponse:
		h.answer(conn, m)
	}
	return nil
}

func (h *Controller) ConnClosed(conn *esphome.Conn) { h.lost(conn) }

func (h *Controller) Startup() startup.Progress {
	return h.startup(h.settings.API.Read().Adopted, time.Now())
}

func (h *Controller) startup(adopted bool, now time.Time) startup.Progress {
	if !adopted {
		return startup.Progress{Done: true, Doing: "not adopted"}
	}
	if !h.settings.Home.Read().Enabled {
		return startup.Progress{Done: true, Doing: "home control off"}
	}
	switch h.Access() {
	case Allowed:
		return startup.Progress{Done: true, Doing: "synced"}
	case Refused:
		return startup.Progress{Failed: true, Doing: "actions not allowed"}
	}
	if now.Sub(h.born) > syncWait {
		return startup.Progress{Failed: true, Doing: "Home Assistant has not connected"}
	}
	if h.Connected() {
		return startup.Progress{Doing: "checking access"}
	}
	return startup.Progress{Doing: "waiting for Home Assistant"}
}

func (h *Controller) Access() Access {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.access
}

func (h *Controller) Connected() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.links) > 0 || h.holding(time.Now())
}

func (h *Controller) holding(now time.Time) bool {
	return !h.dropped.IsZero() && now.Sub(h.dropped) < dropHold
}

func (h *Controller) overall() Access {
	unknown := len(h.links) == 0
	for _, l := range h.links {
		switch l.access {
		case Allowed:
			return Allowed
		case Unknown:
			unknown = true
		}
	}
	if h.holding(time.Now()) {
		return h.held
	}
	if unknown {
		return Unknown
	}
	return Refused
}

func (h *Controller) judge() (Access, bool) {
	access := h.overall()
	if access == h.access {
		return access, false
	}
	h.access = access
	return access, true
}

func (h *Controller) Probe() {
	h.mu.Lock()
	conns := slices.Collect(maps.Keys(h.links))
	h.mu.Unlock()
	for _, conn := range conns {
		h.reprobe(conn)
	}
}

func (h *Controller) attach(conn sender) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	if h.links[conn] == nil {
		h.links[conn] = &link{}
	}
	h.mu.Unlock()
	h.reprobe(conn)
}

func (h *Controller) reprobe(conn sender) {
	if !h.settings.Home.Read().Enabled {
		return
	}
	ready := make(chan struct{})
	h.mu.Lock()
	l := h.links[conn]
	if h.closed || l == nil {
		h.mu.Unlock()
		return
	}
	l.access = Unknown
	if l.stop != nil {
		l.stop()
	}
	ctx, stop := context.WithTimeout(h.ctx, probeWait)
	l.stop = stop
	h.workers.Go(func() {
		defer stop()
		select {
		case <-ready:
			h.probe(ctx, conn)
		case <-ctx.Done():
		}
	})
	access, moved := h.judge()
	h.mu.Unlock()
	if moved {
		h.Changed.Emit(access)
	}
	close(ready)
}

func (h *Controller) probe(ctx context.Context, conn sender) {
	raw, err := h.send(
		ctx,
		[]sender{conn},
		request(Ping, pingData(), nil, pingReply),
	)
	var failed *Failed
	switch {
	case err == nil:
		integration, protocol := integrationOf(raw)
		h.settle(conn, Allowed, integration, protocol)
	case errors.As(err, &failed):
		h.settle(conn, Allowed, IntegrationMissing, 0)
	case errors.Is(err, context.DeadlineExceeded):
		slog.Warn(
			"Home Assistant does not answer actions from this device; enable them in the ESPHome integration's options",
		)
		h.settle(conn, Refused, IntegrationUnknown, 0)
	}
}

func (h *Controller) Integration() Integration {
	h.mu.Lock()
	defer h.mu.Unlock()
	best := IntegrationUnknown
	for _, l := range h.links {
		best = max(best, l.integration)
	}
	return best
}

func integrationOf(raw []byte) (Integration, int) {
	var out struct {
		Response json.RawMessage `json:"response"`
	}
	var answer struct {
		Supported bool `json:"supported"`
		Protocol  int  `json:"protocol"`
	}
	if json.Unmarshal(raw, &out) != nil || json.Unmarshal(unwrap(out.Response), &answer) != nil {
		return IntegrationOutdated, 0
	}
	if !answer.Supported {
		return IntegrationOutdated, answer.Protocol
	}
	return IntegrationReady, answer.Protocol
}

func (h *Controller) speaker(protocol int) (sender, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn, l := range h.links {
		if l.access == Allowed && l.integration == IntegrationReady && l.protocol >= protocol {
			return conn, true
		}
	}
	return nil, false
}

func (h *Controller) Speaks(protocol int) bool {
	_, ok := h.speaker(protocol)
	return ok
}

func (h *Controller) Enable(on bool) {
	if on {
		h.Probe()
		return
	}
	h.mu.Lock()
	for _, l := range h.links {
		l.access = Unknown
		if l.stop != nil {
			l.stop()
		}
	}
	h.dropped, h.held = time.Time{}, Unknown
	access, moved := h.judge()
	h.mu.Unlock()
	if moved {
		h.Changed.Emit(access)
	}
}

func (h *Controller) settle(conn sender, access Access, integration Integration, protocol int) {
	h.mu.Lock()
	l := h.links[conn]
	if l == nil {
		h.mu.Unlock()
		return
	}
	l.access, l.integration, l.protocol = access, integration, protocol
	overall, moved := h.judge()
	h.mu.Unlock()
	if moved {
		h.Changed.Emit(overall)
	}
	h.Linked.Emit(struct{}{})
}

func (h *Controller) lost(conn sender) {
	h.mu.Lock()
	l := h.links[conn]
	if l == nil {
		h.mu.Unlock()
		return
	}
	delete(h.links, conn)
	if l.stop != nil {
		l.stop()
	}
	at := time.Now()
	h.held, h.dropped = h.access, at
	h.abandon(conn)
	access, moved := h.judge()
	h.mu.Unlock()
	if moved {
		h.Changed.Emit(access)
	}
	h.Linked.Emit(struct{}{})
	time.AfterFunc(dropHold, func() { h.expire(at) })
}

func (h *Controller) abandon(conn sender) {
	for id, p := range h.waiting {
		if _, ok := p.to[conn]; !ok {
			continue
		}
		delete(p.to, conn)
		if len(p.to) == 0 {
			p.ch <- reply{err: ErrNotConnected}
			delete(h.waiting, id)
		}
	}
}

func (h *Controller) expire(at time.Time) {
	h.mu.Lock()
	if !h.dropped.Equal(at) {
		h.mu.Unlock()
		return
	}
	h.dropped, h.held = time.Time{}, Unknown
	access, moved := h.judge()
	h.mu.Unlock()
	if moved {
		h.Changed.Emit(access)
	}
}

func (h *Controller) answer(from sender, m *api.HomeassistantActionResponse) {
	h.mu.Lock()
	p := h.waiting[m.GetCallId()]
	if p == nil {
		h.mu.Unlock()
		return
	}
	r := reply{data: m.GetResponseData()}
	if !m.GetSuccess() {
		r = reply{err: &Failed{Message: m.GetErrorMessage()}}
		delete(p.to, from)
		if len(p.to) > 0 {
			h.mu.Unlock()
			return
		}
	}
	delete(h.waiting, m.GetCallId())
	h.mu.Unlock()
	p.ch <- r
}

func (h *Controller) Call(ctx context.Context, action string, data map[string]string) error {
	_, err := h.call(ctx, request(action, data, nil, ""))
	return err
}

func (h *Controller) Perform(
	ctx context.Context,
	action string,
	data, templates map[string]string,
) error {
	_, err := h.call(ctx, request(action, data, templates, ""))
	return err
}

func (h *Controller) Template(ctx context.Context, tmpl string) (json.RawMessage, error) {
	action, data := h.carrier()
	return h.Ask(ctx, action, data, nil, tmpl)
}

func (h *Controller) carrier() (string, map[string]string) {
	if h.Integration() == IntegrationReady {
		return Ping, pingData()
	}
	return recorder, recorderData()
}

func pingData() map[string]string { return map[string]string{"protocol": strconv.Itoa(Protocol)} }

func (h *Controller) Ask(
	ctx context.Context,
	action string,
	data, templates map[string]string,
	tmpl string,
) (json.RawMessage, error) {
	raw, err := h.call(ctx, request(action, data, templates, tmpl))
	if err != nil {
		return nil, err
	}
	var out struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("homeassistant: the response: %w", err)
	}
	return unwrap(out.Response), nil
}

const recorder = "recorder.get_statistics"

func recorderData() map[string]string {
	return map[string]string{
		"statistic_ids": "countertop.none",
		"start_time":    time.Now().UTC().Format(time.RFC3339),
		"period":        "hour",
		"types":         "state",
	}
}

func (h *Controller) call(
	ctx context.Context,
	req *api.HomeassistantActionRequest,
) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h.mu.Lock()
	targets, err := h.targets()
	h.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return h.send(ctx, targets, req)
}

func (h *Controller) Relay(
	ctx context.Context,
	from *api.HomeassistantActionRequest,
) ([]byte, error) {
	req := &api.HomeassistantActionRequest{
		Service:          from.Service,
		Data:             from.Data,
		DataTemplate:     from.DataTemplate,
		Variables:        from.Variables,
		IsEvent:          from.IsEvent,
		WantsResponse:    from.WantsResponse,
		ResponseTemplate: from.ResponseTemplate,
	}
	if !req.IsEvent {
		return h.call(ctx, req)
	}
	h.mu.Lock()
	targets, err := h.targets()
	h.mu.Unlock()
	if err != nil {
		return nil, err
	}
	for _, t := range targets {
		if err := t.Send(req); err != nil {
			h.lost(t)
		}
	}
	return nil, nil
}

func (h *Controller) targets() ([]sender, error) {
	if len(h.links) == 0 {
		return nil, ErrNotConnected
	}
	var out []sender
	for conn, l := range h.links {
		if l.access != Refused {
			out = append(out, conn)
		}
	}
	if h.access == Refused || len(out) == 0 {
		return nil, ErrNotAllowed
	}
	return out, nil
}

func (h *Controller) send(
	ctx context.Context,
	targets []sender,
	req *api.HomeassistantActionRequest,
) ([]byte, error) {
	p := &pending{ch: make(chan reply, 1), to: make(map[sender]struct{}, len(targets))}
	for _, t := range targets {
		p.to[t] = struct{}{}
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, ErrNotConnected
	}
	h.next++
	id := h.next
	h.waiting[id] = p
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.waiting, id)
		h.mu.Unlock()
	}()

	req.CallId = id
	for _, t := range targets {
		if err := t.Send(req); err != nil {
			h.lost(t)
		}
	}

	select {
	case r := <-p.ch:
		return r.data, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-h.ctx.Done():
		return nil, ErrNotConnected
	}
}

func request(
	action string,
	data, templates map[string]string,
	tmpl string,
) *api.HomeassistantActionRequest {
	return &api.HomeassistantActionRequest{
		Service:          action,
		Data:             pairs(data),
		DataTemplate:     pairs(templates),
		WantsResponse:    tmpl != "",
		ResponseTemplate: tmpl,
	}
}

func pairs(m map[string]string) []*api.HomeassistantServiceMap {
	out := make([]*api.HomeassistantServiceMap, 0, len(m))
	for k, v := range m {
		out = append(out, &api.HomeassistantServiceMap{Key: k, Value: v})
	}
	return out
}

func (h *Controller) Watch(changed func(Access)) func() { return h.Changed.Listen(changed) }
