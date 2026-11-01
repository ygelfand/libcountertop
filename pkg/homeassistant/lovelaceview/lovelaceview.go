package lovelaceview

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/compositor"
	backend "github.com/ygelfand/libcountertop/pkg/display/gogui"
	"github.com/ygelfand/libcountertop/pkg/display/icons"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/media/cast/engine"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/runtime/service"
	"github.com/ygelfand/libcountertop/pkg/say"
)

//go:embed bridge.js
var bridge string

const (
	binding    = "countertopAuth"
	setToken   = "externalAuthSetToken"
	revoked    = "externalAuthRevokeToken"
	tokenLimit = 20 * time.Second
	retryFirst = 5 * time.Second
	retryMost  = time.Minute
)

var ErrClosed = errors.New("lovelaceview: no Home Assistant dashboard is open")

type Token struct {
	URL     string
	Access  string
	Expires time.Duration
}

type Options struct {
	Name   string
	Engine *engine.Host
	Path   func() string
	Token  func(context.Context) (Token, error)
	Hint   func() string
	Redraw func()
}

type View struct {
	o    Options
	live *backend.Live

	mu       sync.Mutex
	stop     context.CancelFunc
	page     *engine.Page
	base     string
	shown    string
	w, h     int
	down     bool
	pictured bool
	failed   bool
	healing  bool
	wait     time.Duration
	retry    *time.Timer
}

func NewView(o Options) *View {
	return &View{o: o, live: backend.NewLive("lovelace-" + o.Name)}
}

func (v *View) On() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.stop != nil
}

func (v *View) Restart() {
	if v.On() {
		v.Set(false)
		v.Set(true)
	}
}

func (v *View) Set(on bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	switch {
	case on && v.stop == nil:
		ctx, stop := context.WithCancel(context.Background())
		v.stop = stop
		safe.Go("dashboard page", func() { _ = service.Keep(ctx, "dashboard page", v.session) })
	case !on && v.stop != nil:
		v.stop()
		v.stop = nil
	}
}

func (v *View) Reload(ctx context.Context) error {
	v.mu.Lock()
	page, base := v.page, v.base
	v.mu.Unlock()
	if page == nil {
		return ErrClosed
	}
	at, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("lovelaceview: the Home Assistant address %q: %w", base, err)
	}
	for _, call := range []struct {
		method string
		params any
	}{
		{"Storage.clearDataForOrigin", map[string]any{
			"origin":       at.Scheme + "://" + at.Host,
			"storageTypes": "cache_storage,service_workers",
		}},
		{"Network.clearBrowserCache", nil},
		{"Page.reload", map[string]any{"ignoreCache": true}},
	} {
		if _, err := page.Request(ctx, call.method, call.params); err != nil {
			return fmt.Errorf("lovelaceview: %s: %w", call.method, err)
		}
	}
	return nil
}

func (v *View) Retry() {
	v.mu.Lock()
	page, shown, failed := v.page, v.shown, v.failed
	if failed {
		v.wait = 0
		v.stopRetry()
	}
	v.mu.Unlock()
	if failed && page != nil && shown != "" {
		_ = page.Navigate(shown)
	}
}

func (v *View) Draw(at ui.Rect) gogui.View {
	v.fit(at)
	if v.unreachable() {
		return v.picture(at, gogui.ContainerCfg{
			ID: "lovelace-" + v.o.Name,
			OnClick: func(e gogui.EventCtx) {
				v.Retry()
				e.Consume()
			},
		})
	}
	touch := func(down bool) func(gogui.EventCtx) {
		return func(e gogui.EventCtx) {
			v.touch(e, at, down)
			e.Consume()
		}
	}
	move := func(e gogui.EventCtx) {
		if v.pressed() {
			v.touch(e, at, true)
			e.Consume()
		}
	}
	return v.picture(at, gogui.ContainerCfg{
		ID:          "lovelace-" + v.o.Name,
		OnMouseDown: touch(true),
		OnMouseMove: move,
		OnMouseUp:   touch(false),
	})
}

func (v *View) Saver(at ui.Rect, wake func()) gogui.View {
	v.fit(at)
	return v.picture(at, gogui.ContainerCfg{
		ID: "lovelace-" + v.o.Name,
		OnClick: func(e gogui.EventCtx) {
			wake()
			e.Consume()
		},
	})
}

func (v *View) picture(at ui.Rect, cfg gogui.ContainerCfg) gogui.View {
	cfg.Sizing = gogui.FixedFixed
	cfg.Width, cfg.Height = float32(at.W), float32(at.H)
	cfg.Padding = gogui.NoPadding
	v.mu.Lock()
	pictured, failed := v.pictured, v.failed
	v.mu.Unlock()
	switch {
	case pictured && !failed:
		cfg.Content = []gogui.View{
			gogui.Image(
				gogui.ImageCfg{Src: v.live.Src(), Width: float32(at.W), Height: float32(at.H)},
			),
		}
		return gogui.Column(cfg)
	}
	line, detail := say.T("lovelace.loading"), ""
	switch {
	case failed:
		line, detail = say.T("lovelace.unreachable"), say.T("lovelace.unreachable.retry")
	case v.o.Hint != nil:
		if hint := v.o.Hint(); hint != "" {
			line = hint
		}
	}
	cfg.HAlign, cfg.VAlign = gogui.HAlignCenter, gogui.VAlignMiddle
	cfg.Spacing = gogui.SpacingLarge
	cfg.Color = gogui.CurrentTheme().Cfg.ColorBackground
	cfg.Content = card(at, line, detail)
	return gogui.Column(cfg)
}

func (v *View) fit(at ui.Rect) {
	v.mu.Lock()
	page := v.page
	changed := at.W != v.w || at.H != v.h
	v.w, v.h = at.W, at.H
	want := v.address()
	moved := page != nil && want != "" && want != v.shown
	if moved {
		v.shown = want
	}
	v.mu.Unlock()
	if page == nil {
		return
	}
	if changed && at.W > 0 && at.H > 0 {
		page.Window.Resize(at.W, at.H)
	}
	if moved {
		_ = page.Navigate(want)
	}
}

func (v *View) address() string {
	base := strings.TrimRight(v.base, "/")
	if base == "" {
		return ""
	}
	path := ""
	if v.o.Path != nil {
		path = strings.Trim(v.o.Path(), "/")
	}
	return base + "/" + path + "?external_auth=1"
}

func (v *View) session(ctx context.Context) error {
	if v.o.Token == nil {
		return errors.New("lovelaceview: no way to sign in")
	}
	first, err := v.o.Token(ctx)
	if err != nil {
		return err
	}
	v.mu.Lock()
	v.base = first.URL
	v.mu.Unlock()
	page, err := v.o.Engine.Open(ctx, v.o.Name)
	if err != nil {
		return err
	}
	defer func() { _ = page.Close() }()
	defer page.Events.Listen(func(e engine.Event) { v.event(ctx, page, e) })()
	defer page.Window.Frames.Listen(v.frame)()
	for _, call := range []struct {
		method string
		params any
	}{
		{"Runtime.enable", nil},
		{"Page.enable", nil},
		{"Runtime.addBinding", map[string]any{"name": binding}},
		{"Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": bridge}},
	} {
		if err := page.Call(call.method, call.params); err != nil {
			return err
		}
	}

	v.mu.Lock()
	v.page, v.shown = page, v.address()
	w, h, shown := v.w, v.h, v.shown
	v.mu.Unlock()
	defer func() {
		v.mu.Lock()
		if v.page == page {
			v.page, v.shown, v.pictured, v.failed, v.healing, v.wait = nil, "", false, false, false, 0
			v.stopRetry()
		}
		v.mu.Unlock()
	}()
	if w > 0 && h > 0 {
		page.Window.Resize(w, h)
	}
	if shown != "" {
		if err := page.Navigate(shown); err != nil {
			return err
		}
	}
	select {
	case <-page.Done():
		return errors.New("lovelaceview: the dashboard page closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (v *View) event(ctx context.Context, page *engine.Page, e engine.Event) {
	if e.Method == "Page.frameNavigated" {
		v.navigated(page, e.Params)
		return
	}
	if e.Method != "Runtime.bindingCalled" {
		return
	}
	var call struct {
		Name    string `json:"name"`
		Payload string `json:"payload"`
	}
	if json.Unmarshal(e.Params, &call) != nil || call.Name != binding {
		return
	}
	var ask struct {
		Kind     string `json:"kind"`
		Callback string `json:"callback"`
	}
	if json.Unmarshal([]byte(call.Payload), &ask) != nil {
		return
	}
	switch {
	case ask.Kind == "revoke" && ask.Callback == revoked:
		answer(page, revoked+"(true)")
	case ask.Kind == "get" && ask.Callback == setToken:
		safe.Go("dashboard token", func() { v.grant(ctx, page) })
	}
}

func (v *View) grant(ctx context.Context, page *engine.Page) {
	if v.o.Token == nil {
		answer(page, setToken+"(false)")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, tokenLimit)
	defer cancel()
	t, err := v.o.Token(ctx)
	if err != nil {
		slog.Warn("the dashboard got no Home Assistant token", "err", err)
		answer(page, setToken+"(false)")
		return
	}
	v.mu.Lock()
	v.base = t.URL
	v.mu.Unlock()
	body, err := json.Marshal(map[string]any{
		"access_token": t.Access,
		"expires_in":   int(t.Expires / time.Second),
	})
	if err != nil {
		answer(page, setToken+"(false)")
		return
	}
	answer(page, fmt.Sprintf("%s(true, %s)", setToken, body))
}

func answer(page *engine.Page, call string) {
	_ = page.Call("Runtime.evaluate", map[string]any{"expression": "window." + call})
}

func (v *View) navigated(page *engine.Page, raw json.RawMessage) {
	var nav struct {
		Frame struct {
			ParentID    string `json:"parentId"`
			Unreachable string `json:"unreachableUrl"`
		} `json:"frame"`
	}
	if json.Unmarshal(raw, &nav) != nil || nav.Frame.ParentID != "" {
		return
	}
	v.mu.Lock()
	v.stopRetry()
	if nav.Frame.Unreachable == "" {
		v.healing, v.wait = v.failed, 0
		v.mu.Unlock()
		return
	}
	v.failed, v.healing = true, false
	wait := max(v.wait, retryFirst)
	v.wait = min(wait*2, retryMost)
	v.retry = time.AfterFunc(wait, func() { v.again(page) })
	v.mu.Unlock()
	slog.Warn("Home Assistant dashboard did not load", "url", nav.Frame.Unreachable, "retry", wait)
	v.redraw()
}

func (v *View) again(page *engine.Page) {
	v.mu.Lock()
	current, shown := v.page == page, v.shown
	v.mu.Unlock()
	if current && shown != "" {
		_ = page.Navigate(shown)
	}
}

func (v *View) stopRetry() {
	if v.retry != nil {
		v.retry.Stop()
		v.retry = nil
	}
}

func (v *View) unreachable() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.failed
}

func (v *View) redraw() {
	if v.o.Redraw != nil {
		v.o.Redraw()
	}
}

func card(at ui.Rect, line, detail string) []gogui.View {
	mark := float32(min(at.W, at.H)) / 5
	big := gogui.CurrentTheme().Cfg.TextStyleDef
	big.Size *= 1.4
	big.Align = gogui.TextAlignCenter
	small := widgets.Secondary()
	small.Align = gogui.TextAlignCenter
	words := []gogui.View{
		gogui.Text(gogui.TextCfg{Text: line, TextStyle: big, Mode: gogui.TextModeWrap}),
	}
	if detail != "" {
		words = append(
			words,
			gogui.Text(gogui.TextCfg{Text: detail, TextStyle: small, Mode: gogui.TextModeWrap}),
		)
	}
	return []gogui.View{
		gogui.Svg(gogui.SvgCfg{
			ID:      "lovelace-mark",
			SvgData: icons.HomeAssistant,
			Width:   mark,
			Height:  mark,
		}),
		gogui.Column(gogui.ContainerCfg{
			Sizing:  gogui.FixedFit,
			Width:   float32(at.W) * 0.6,
			Padding: gogui.NoPadding,
			Spacing: gogui.SpacingMedium,
			HAlign:  gogui.HAlignCenter,
			Content: words,
		}),
	}
}

func (v *View) frame(f compositor.Frame) {
	v.mu.Lock()
	healed := v.healing
	if healed {
		v.failed, v.healing = false, false
	}
	v.mu.Unlock()
	damage := f.Damage
	if healed {
		damage = []image.Rectangle{image.Rect(0, 0, f.Width, f.Height)}
	}
	for _, r := range damage {
		if r.Empty() {
			continue
		}
		rgba := make([]byte, r.Dx()*r.Dy()*4)
		for y := r.Min.Y; y < r.Max.Y; y++ {
			src := f.Pixels[y*f.Stride+r.Min.X*4 : y*f.Stride+r.Max.X*4]
			dst := rgba[(y-r.Min.Y)*r.Dx()*4:]
			for x := 0; x < len(src); x += 4 {
				dst[x], dst[x+1], dst[x+2], dst[x+3] = src[x+2], src[x+1], src[x], 0xff
			}
		}
		v.live.Write(f.Width, f.Height, r, rgba)
	}
	v.mu.Lock()
	v.pictured = true
	v.mu.Unlock()
	v.redraw()
}

func (v *View) pressed() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.down
}

func (v *View) touch(e gogui.EventCtx, at ui.Rect, down bool) {
	v.mu.Lock()
	was := v.down
	v.down = down
	page := v.page
	v.mu.Unlock()
	if page == nil || e.Event == nil || at.W == 0 || at.H == 0 {
		return
	}
	w, h := page.Window.Size()
	s := e.Layout.Shape
	x := float64(e.Event.MouseX-s.X) * float64(w) / float64(at.W)
	y := float64(e.Event.MouseY-s.Y) * float64(h) / float64(at.H)
	switch {
	case down && !was:
		page.Window.TouchDown(0, x, y)
	case down:
		page.Window.TouchMotion(0, x, y)
	case was:
		page.Window.TouchUp(0)
	}
}
