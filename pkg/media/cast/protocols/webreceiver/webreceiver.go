package webreceiver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ygelfand/libcountertop/pkg/display/compositor"
	"github.com/ygelfand/libcountertop/pkg/display/surface"
	"github.com/ygelfand/libcountertop/pkg/media/cast"
	"github.com/ygelfand/libcountertop/pkg/media/cast/engine"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
	"github.com/ygelfand/libcountertop/pkg/media/urlplayer"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/runtime/service"
)

const (
	Name     = "webreceiver"
	systemNS = "urn:x-cast:com.google.cast.system"
	injected = "__inject__"
	idle     = "about:blank"
	display  = "wayland-0"

	DefaultBusPort   = 8012
	DefaultToolsPort = 9222
)

type Options struct {
	Dir         string
	State       string
	BusPort     int
	ToolsPort   int
	Nameservers func() []string
	Helper      func() *surface.Client
	Built       func(*Receiver)
	Tuning      *engine.Tuning
}

type frame struct {
	Namespace string `json:"namespace"`
	SenderID  string `json:"senderId"`
	Data      string `json:"data"`
}

type Receiver struct {
	env    cast.Env
	o      Options
	player *urlplayer.Player
	hooks  string

	mu           sync.Mutex
	tools        *devtools
	page         *websocket.Conn
	asked        cast.App
	app          cast.Application
	title        string
	pending      []frame
	lastStatus   json.RawMessage
	state        string
	mediaSession int
	requests     int
	shown        urlplayer.Status
	url          string
	tracing      atomic.Bool
	window       *compositor.Window
	unframe      func()
	ads          adBreaks
	media        *bridge
	keys         *keyring
	sponsor      *sponsor
}

func Maker(o Options) cast.Maker {
	return func(env cast.Env) cast.Protocol { return New(env, o) }
}

func New(env cast.Env, o Options) *Receiver {
	if o.BusPort == 0 {
		o.BusPort = DefaultBusPort
	}
	if o.ToolsPort == 0 {
		o.ToolsPort = DefaultToolsPort
	}
	r := &Receiver{env: env, o: o}
	r.media = &bridge{r: r}
	r.keys = &keyring{r: r}
	r.sponsor = &sponsor{r: r}
	if env.Output != nil {
		r.player = urlplayer.New(urlplayer.Options{
			HTTP:     env.Long,
			Output:   env.Output,
			Resample: env.Resample,
			Screen:   env.Video != nil,
			Target:   env.Video,
			Remote:   remote{r},
			Ready:    r.media.started,
		})
	}
	r.hooks = hooks(env, o.BusPort)
	if o.Built != nil {
		o.Built(r)
	}
	return r
}

func (r *Receiver) Name() string         { return Name }
func (r *Receiver) Namespaces() []string { return nil }

func (r *Receiver) Owns(cast.App) bool { return true }

func (r *Receiver) Run(ctx context.Context) {
	_ = service.Keep(ctx, "cast web receiver", r.run)
}

func (r *Receiver) run(ctx context.Context) error {
	root, err := engine.Install(ctx, r.o.Dir)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if r.o.Tuning != nil {
		r.o.Tuning.Restarts(cancel)
	}

	srv, err := r.serve()
	if err != nil {
		return err
	}
	defer srv.Close()

	width, height := r.screen()
	socket := filepath.Join(r.o.State, display)
	comp := compositor.New(compositor.Options{Path: socket, Width: width, Height: height})
	comp.Opened.Listen(r.opened)
	if err := comp.Listen(); err != nil {
		return err
	}
	safe.Go("cast compositor", func() {
		if err := comp.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("the cast compositor stopped", "err", err)
		}
	})

	safe.Go("cast engine", func() {
		_ = engine.Run(ctx, engine.Options{
			Root:        root,
			State:       r.o.State,
			Port:        r.o.ToolsPort,
			Display:     socket,
			Args:        r.args(),
			Nameservers: r.o.Nameservers,
		})
	})
	if err := engine.Ready(ctx, r.o.ToolsPort); err != nil {
		return err
	}
	tools, err := dialPage(ctx, r.o.ToolsPort)
	if err != nil {
		return err
	}
	defer tools.close()
	for _, call := range []struct {
		method string
		params any
	}{
		{"Runtime.enable", map[string]any{}},
		{"Page.enable", map[string]any{}},
		{"Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": r.hooks}},
	} {
		if err := tools.call(call.method, call.params); err != nil {
			return fmt.Errorf("webreceiver: devtools: %w", err)
		}
	}

	r.mu.Lock()
	r.tools = tools
	asked := r.asked
	r.mu.Unlock()
	slog.Info("the cast web receiver is ready")
	if asked.ID != "" {
		safe.Go("cast web receiver launch", func() { r.launch(asked) })
	}
	defer func() {
		r.mu.Lock()
		r.tools = nil
		r.mu.Unlock()
	}()

	stop := context.AfterFunc(ctx, tools.close)
	defer stop()
	if err := tools.read(); ctx.Err() == nil {
		return err
	}
	return ctx.Err()
}

func (r *Receiver) args() []string {
	out := []string{
		"--autoplay-policy=no-user-gesture-required",
		"--disable-features=LocalNetworkAccessChecks,BlockInsecurePrivateNetworkRequests,PrivateNetworkAccessSendPreflights",
		"--user-agent=Mozilla/5.0 (X11; Linux armv7l) AppleWebKit/537.36 (KHTML, like Gecko) " +
			"Chrome/150.0.0.0 Safari/537.36 CrKey/1.56.500000",
	}
	if r.o.Tuning != nil {
		out = append(out, r.o.Tuning.Args()...)
	}
	return out
}

func (r *Receiver) serve() (*http.Server, error) {
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(r.o.BusPort))
	if err != nil {
		return nil, fmt.Errorf("webreceiver: %w", err)
	}
	srv := &http.Server{Handler: r.handler(), ReadHeaderTimeout: 5 * time.Second}
	safe.Go("cast web receiver bus", func() {
		if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("the cast web receiver bus stopped", "err", err)
		}
	})
	return srv, nil
}

func (r *Receiver) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/ipc", r.bus)
	mux.HandleFunc("/media", r.media.serve)
	mux.HandleFunc("/plays", r.plays)
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = fmt.Fprint(w, r.keys.available())
	})
	return mux
}

func (r *Receiver) Started(a cast.App) {
	safe.Go("cast web receiver launch", func() { r.launch(a) })
}

func (r *Receiver) Ended(cast.App) {
	r.mu.Lock()
	tools, page := r.tools, r.page
	r.asked, r.app, r.pending = cast.App{}, cast.Application{}, nil
	r.mediaSession, r.page, r.lastStatus = 0, nil, nil
	r.shown, r.ads = urlplayer.Status{}, adBreaks{}
	r.unframeLocked()
	if r.window != nil {
		r.window.Resume()
	}
	r.title, r.state, r.url = "", "", ""
	r.mu.Unlock()
	if page != nil {
		_ = page.Close()
	}
	if r.player != nil {
		_ = r.player.Stop()
		r.player.Describe(nil)
		r.player.Follow(nil)
	}
	r.keys.forget()
	r.media.forget()
	r.sponsor.page("")
	if r.env.Output != nil {
		r.env.Output.Attend(nil)
	}
	if tools != nil {
		_ = tools.navigate(idle)
	}
}

func (r *Receiver) launch(a cast.App) {
	r.mu.Lock()
	r.asked = a
	tools := r.tools
	r.mu.Unlock()
	if tools == nil {
		slog.Info("a cast app waits for the web receiver", "app", a.ID)
		return
	}
	page, err := cast.Describe(context.Background(), r.env.HTTP, a.ID)
	if err == nil && page.URL == "" {
		err = errors.New("no receiver page")
	}
	if err != nil {
		slog.Warn("a cast app has no page to run", "app", a.ID, "err", err)
		return
	}
	title, logo := firstOf(page.Name, a.Name), firstOf(page.Icon, a.Icon)
	r.mu.Lock()
	r.title = title
	r.mu.Unlock()
	if r.env.Output != nil {
		r.env.Output.Attend(
			&playback.Session{Label: title, Logo: logo, Pictured: r.env.Video != nil},
		)
	}
	slog.Info("a cast app runs in the web receiver", "app", title, "url", page.URL)
	if r.env.Video != nil {
		if t := r.env.Video(); t.Width > 0 && t.Height > 0 {
			_ = tools.size(t.Width, t.Height)
		}
	}
	_ = tools.navigate(idle)
	if err := tools.navigate(page.URL); err != nil {
		slog.Warn("the cast web receiver could not open a page", "err", err)
	}
}

func (r *Receiver) screen() (width, height int) {
	if r.env.Video == nil {
		return 0, 0
	}
	t := r.env.Video()
	return t.Width, t.Height
}

func (r *Receiver) opened(w *compositor.Window) {
	r.mu.Lock()
	r.unframeLocked()
	r.window = w
	r.mu.Unlock()
	w.Closed.Listen(func(struct{}) {
		r.mu.Lock()
		if r.window == w {
			r.unframeLocked()
			r.window = nil
		}
		r.mu.Unlock()
	})
}

func (r *Receiver) pictured() {
	if r.env.Video == nil {
		return
	}
	back, ok := r.env.Output.(playback.Backdrop)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.window == nil {
		return
	}
	r.window.Resume()
	if r.unframe == nil {
		r.unframe = r.window.Frames.Listen(func(f compositor.Frame) { r.framed(back, f) })
	}
}

func (r *Receiver) unpictured() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.window != nil && r.unframe != nil {
		r.window.Suspend()
	}
	r.unframeLocked()
}

func (r *Receiver) unframeLocked() {
	if r.unframe != nil {
		r.unframe()
		r.unframe = nil
	}
}

func (r *Receiver) framed(back playback.Backdrop, f compositor.Frame) {
	r.mu.Lock()
	casting := r.app.SessionID != ""
	r.mu.Unlock()
	if casting {
		back.Backdrop(rgba(f))
	}
}

func rgba(f compositor.Frame) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, f.Width, f.Height))
	for y := range f.Height {
		src := f.Pixels[y*f.Stride:]
		dst := out.Pix[y*out.Stride:]
		for x := range f.Width {
			p := src[4*x:]
			d := dst[4*x:]
			d[0], d[1], d[2], d[3] = p[2], p[1], p[0], 0xff
		}
	}
	return out
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (r *Receiver) Receive(app *cast.Application, m cast.Message) ([]cast.Message, error) {
	r.mu.Lock()
	r.app = *app
	r.mu.Unlock()

	if len(m.Binary) == 0 {
		r.trace("a sender told the cast page", "from", m.Source, "namespace", m.Namespace,
			"body", m.Payload)
	}
	switch m.Namespace {
	case cast.NSConnection:
		h, err := cast.Kind(m.Payload)
		if err != nil {
			return nil, err
		}
		switch h.Type {
		case cast.TypeConnect:
			r.toPage(system(map[string]any{
				"type": "senderconnected", "senderId": m.Source, "userAgent": "sender",
			}))
			r.mu.Lock()
			status := r.lastStatus
			r.mu.Unlock()
			if status != nil {
				return []cast.Message{cast.Reply(m, cast.NSMedia,
					`{"type":"MEDIA_STATUS","requestId":0,"status":`+string(status)+`}`)}, nil
			}
		case cast.TypeClose:
			r.toPage(system(map[string]any{
				"type": "senderdisconnected", "senderId": m.Source, "reason": "requested_by_sender",
			}))
		}
	default:
		if len(m.Binary) == 0 {
			r.toPage(frame{Namespace: m.Namespace, SenderID: m.Source, Data: m.Payload})
		}
	}
	return nil, nil
}

func system(body map[string]any) frame {
	data, _ := json.Marshal(body)
	return frame{Namespace: systemNS, SenderID: "SystemSender", Data: string(data)}
}

func (r *Receiver) toPage(f frame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.page == nil {
		r.pending = append(r.pending, f)
		return
	}
	if err := r.page.WriteJSON(f); err != nil {
		slog.Debug("the cast page did not take a message", "err", err)
	}
}

func (r *Receiver) bus(w http.ResponseWriter, req *http.Request) {
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	c, err := up.Upgrade(w, req, nil)
	if err != nil {
		return
	}
	r.mu.Lock()
	old := r.page
	r.page = c
	r.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	defer func() {
		r.mu.Lock()
		if r.page == c {
			r.page = nil
		}
		r.mu.Unlock()
		_ = c.Close()
	}()
	for {
		var f frame
		if err := c.ReadJSON(&f); err != nil {
			return
		}
		r.fromPage(c, f)
	}
}

func (r *Receiver) fromPage(c *websocket.Conn, f frame) {
	r.mu.Lock()
	app := r.app
	r.mu.Unlock()

	switch f.Namespace {
	case systemNS:
		r.trace("the cast page told the platform", "body", f.Data)
		var h struct {
			Type             string   `json:"type"`
			ActiveNamespaces []string `json:"activeNamespaces"`
		}
		if json.Unmarshal([]byte(f.Data), &h) != nil || h.Type != "ready" {
			return
		}
		r.ready(c, app, h.ActiveNamespaces)
		r.pictured()
		return
	case cast.NSMedia:
		r.watch(f.Data)
	}
	r.trace("the cast page told senders", "to", f.SenderID, "namespace", f.Namespace,
		"body", f.Data)

	to := f.SenderID
	if to == "" || to == "*:*" || to == "*" {
		to = cast.Broadcast
	}
	if to == injected || app.TransportID == "" {
		return
	}
	r.env.Send(cast.Message{
		Source:      app.TransportID,
		Destination: to,
		Namespace:   f.Namespace,
		Payload:     f.Data,
	})
	if status, ok := mediaStatus(
		f.Data,
	); ok && to != cast.Broadcast &&
		f.Namespace == cast.NSMedia {
		r.env.Send(cast.Message{
			Source:      app.TransportID,
			Destination: cast.Broadcast,
			Namespace:   cast.NSMedia,
			Payload:     `{"type":"MEDIA_STATUS","status":` + string(status) + `}`,
		})
	}
}

func mediaStatus(data string) (json.RawMessage, bool) {
	var s struct {
		Type   string          `json:"type"`
		Status json.RawMessage `json:"status"`
	}
	if json.Unmarshal([]byte(data), &s) != nil || s.Type != cast.TypeMediaStatus ||
		len(s.Status) == 0 {
		return nil, false
	}
	return s.Status, true
}

func (r *Receiver) ready(c *websocket.Conn, app cast.Application, namespaces []string) {
	r.mu.Lock()
	title := r.title
	r.mu.Unlock()
	if r.env.Advertise != nil && app.AppID != "" {
		r.env.Advertise(app.AppID, title, namespaces)
	}
	reply := system(map[string]any{
		"type":              "ready",
		"applicationId":     app.AppID,
		"applicationName":   app.DisplayName,
		"iconUrl":           app.IconURL,
		"sessionId":         app.SessionID,
		"launchingSenderId": "",
		"launchedFrom":      "CAST",
		"deviceCapabilities": map[string]any{
			"display_supported":     r.env.Video != nil,
			"touch_input_supported": r.env.Video != nil,
			"is_hdr_supported":      false,
			"is_dv_supported":       false,
			"bluetooth_supported":   false,
			"is_group":              false,
		},
		"version":          "1.56",
		"messagesVersion":  "1.0",
		"systemVersion":    "1.56",
		"deviceIdentifier": r.env.Device.ID,
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.page != c {
		return
	}
	_ = c.WriteJSON(reply)
	for _, f := range r.pending {
		_ = c.WriteJSON(f)
	}
	r.pending = nil
}

func (r *Receiver) watch(data string) {
	status, ok := mediaStatus(data)
	if !ok {
		return
	}
	var sessions []struct {
		MediaSessionID int          `json:"mediaSessionId"`
		PlayerState    string       `json:"playerState"`
		IdleReason     string       `json:"idleReason"`
		CurrentTime    float64      `json:"currentTime"`
		BreakStatus    *breakStatus `json:"breakStatus"`
		Media          *struct {
			castBreaks
			Metadata   *cast.Metadata `json:"metadata"`
			StreamType string         `json:"streamType"`
			Duration   float64        `json:"duration"`
		} `json:"media"`
	}
	_ = json.Unmarshal(status, &sessions)
	r.mu.Lock()
	r.lastStatus = status
	var st urlplayer.Status
	ended := false
	if len(sessions) > 0 {
		s := sessions[0]
		state := s.PlayerState
		if state == cast.StateIdle && s.IdleReason != "" && s.IdleReason != cast.IdleInterrupted {
			state = s.IdleReason
		}
		ended = state != r.state && state != s.PlayerState
		r.state = state
		if s.MediaSessionID != r.mediaSession {
			r.ads = adBreaks{}
		}
		r.mediaSession = s.MediaSessionID
		if s.Media != nil {
			r.shown.Length, r.shown.Live = seconds(
				s.Media.Duration,
			), s.Media.StreamType == cast.StreamLive
			if len(s.Media.Breaks) > 0 {
				r.ads = newAdBreaks(s.Media.castBreaks)
			}
		}
		r.shown.At, r.shown.Playing = seconds(s.CurrentTime), s.PlayerState == cast.StatePlaying
		r.shown.Break, r.shown.Breaks = r.ads.now(s.BreakStatus), r.ads.at
		st = r.shown
	}
	r.mu.Unlock()
	if len(sessions) == 0 || r.player == nil {
		return
	}
	if ended {
		_ = r.player.Stop()
		r.pictured()
	}
	r.player.Follow(&st)
	if m := sessions[0].Media; m != nil && m.Metadata != nil {
		r.player.Describe(m.Metadata)
	}
}

func (r *Receiver) inject(kind string, extra map[string]any) error {
	r.mu.Lock()
	r.requests++
	body := map[string]any{
		"type":           kind,
		"requestId":      900000 + r.requests,
		"mediaSessionId": r.mediaSession,
	}
	r.mu.Unlock()
	maps.Copy(body, extra)
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	r.toPage(frame{Namespace: cast.NSMedia, SenderID: injected, Data: string(data)})
	return nil
}

func (r *Receiver) plays(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	asked := req.URL.Query().Get("type")
	feed := req.URL.Query().Has("feed")
	ok := r.player != nil
	switch {
	case ok && feed:
		ok = r.player.Feeds(displayType(asked))
	case ok:
		ok = r.player.Plays(displayType(asked))
	}
	slog.Debug("the cast page asked what plays", "type", asked, "feed", feed, "answer", ok)
	_, _ = fmt.Fprint(w, ok)
}

type remote struct{ r *Receiver }

func (m remote) Play() error  { return m.r.inject(cast.TypePlay, nil) }
func (m remote) Pause() error { return m.r.inject(cast.TypePause, nil) }
func (m remote) Stop() error {
	if m.r.env.End == nil {
		return m.r.inject(cast.TypeStopMedia, nil)
	}
	m.r.env.End()
	return nil
}

func (m remote) Failed(err error) { m.r.media.failed(err) }

func (m remote) Seek(to time.Duration) error {
	return m.r.inject(cast.TypeSeek, map[string]any{"currentTime": to.Seconds()})
}

var (
	_ cast.Owner   = (*Receiver)(nil)
	_ cast.Starter = (*Receiver)(nil)
)
