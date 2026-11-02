package lovelaceview

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/hook"
	"github.com/ygelfand/libcountertop/pkg/media/cast/engine"
	"github.com/ygelfand/libcountertop/pkg/runtime/product"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

type Setting = setting.Setting[schema.Lovelace]

const (
	Group       setting.Group = "Lovelace"
	listLimit                 = 20 * time.Second
	reloadLimit               = 20 * time.Second
)

type FeatureOptions struct {
	Settings   storage.Section[schema.Lovelace]
	Controller *homeassistant.Controller
	Engine     *engine.Host
	Node       func() string
	Redraw     func()
	Device     uint32
}

type Feature struct {
	o      FeatureOptions
	knobs  *setting.Controls[schema.Lovelace]
	reload *esphome.Button

	Home  *View
	Saver *View

	Listed hook.Hook[struct{}]

	mu     sync.Mutex
	list   homeassistant.Dashboards
	denied bool
	ready  bool
}

func New(o FeatureOptions) *Feature {
	f := &Feature{o: o}
	f.knobs = &setting.Controls[schema.Lovelace]{
		Table: f.table(),
		Read:  o.Settings.Read,
		Save:  f.store,
	}
	f.Home = NewView(Options{
		Name:   "home",
		Engine: o.Engine,
		Path:   func() string { c := o.Settings.Read(); return path(c.Dashboard, c.View) },
		Theme:  func() string { return string(o.Settings.Read().Theme) },
		Token:  f.token,
		Hint:   f.hint,
		Redraw: o.Redraw,
	})
	f.Saver = NewView(Options{
		Name:   "saver",
		Engine: o.Engine,
		Path: func() string {
			c := o.Settings.Read()
			return path(c.SaverDashboard, c.SaverView)
		},
		Theme:  func() string { return string(o.Settings.Read().Theme) },
		Token:  f.token,
		Hint:   f.hint,
		Redraw: o.Redraw,
	})
	f.reload = &esphome.Button{
		Base: esphome.Base{
			ObjectID: "dashboard_reload",
			Name:     "Reload dashboard",
			Icon:     "mdi:web-refresh",
			Category: esphome.CategoryConfig,
			DeviceID: o.Device,
		},
		OnPress: f.Refresh,
	}
	o.Controller.Linked.Listen(func(struct{}) {
		if f.cameBack() {
			f.Home.Retry()
			f.Saver.Retry()
		}
		safe.Go("dashboard lists", f.refresh)
	})
	safe.Go("dashboard lists", f.refresh)
	return f
}

func Build(
	p product.Context,
	settings storage.Section[schema.Lovelace],
	host *engine.Host,
	node func() string,
) *Feature {
	f := New(FeatureOptions{
		Settings:   settings,
		Controller: p.Controller,
		Engine:     host,
		Node:       node,
		Redraw:     p.Redraw,
		Device:     p.Screen,
	})
	p.Settings.Add(f.Controls())
	return f
}

func (f *Feature) Name() string { return "lovelace" }

func (f *Feature) Entities() []esphome.Entity { return []esphome.Entity{f.reload} }

func (f *Feature) Controls() *setting.Controls[schema.Lovelace] { return f.knobs }

func (f *Feature) Reload(ctx context.Context) error {
	var errs []error
	open := 0
	for _, v := range []*View{f.Home, f.Saver} {
		err := v.Reload(ctx)
		if errors.Is(err, ErrClosed) {
			continue
		}
		open++
		errs = append(errs, err)
	}
	if open == 0 {
		return ErrClosed
	}
	return errors.Join(errs...)
}

func (f *Feature) Refresh() { safe.Go("dashboard reload", f.fresh) }

func (f *Feature) fresh() {
	ctx, cancel := context.WithTimeout(context.Background(), reloadLimit)
	defer cancel()
	if err := f.Reload(ctx); err != nil && !errors.Is(err, ErrClosed) {
		slog.Warn("the dashboard could not be reloaded", "err", err)
	}
}

func (f *Feature) Rest() *View {
	c := f.o.Settings.Read()
	if f.Home.On() && path(c.Dashboard, c.View) == path(c.SaverDashboard, c.SaverView) {
		f.Saver.Set(false)
		return f.Home
	}
	f.Saver.Set(true)
	return f.Saver
}

func (f *Feature) Wake() { f.Saver.Set(false) }

func (f *Feature) Ready() bool { return f.o.Controller.Speaks(homeassistant.DashboardProtocol) }

func (f *Feature) cameBack() bool {
	now := f.Ready()
	f.mu.Lock()
	defer f.mu.Unlock()
	back := now && !f.ready
	f.ready = now
	return back
}

func (f *Feature) refresh() {
	if !f.Ready() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), listLimit)
	defer cancel()
	list, err := f.o.Controller.Dashboards(ctx, f.o.Node())
	if err != nil {
		slog.Warn("the dashboard list could not be fetched", "err", err)
		return
	}
	f.mu.Lock()
	was := f.list.Version
	f.list = list
	f.mu.Unlock()
	if was != "" && list.Version != was {
		slog.Info(
			"Home Assistant changed version, reloading the dashboard",
			"from",
			was,
			"to",
			list.Version,
		)
		f.fresh()
	}
	f.knobs.Refresh()
	f.Listed.Emit(struct{}{})
	if f.o.Redraw != nil {
		f.o.Redraw()
	}
}

func (f *Feature) hint() string {
	if !f.Ready() {
		return say.T("lovelace.hint.integration")
	}
	user := f.o.Settings.Read().User
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.list.Users) == 0 {
		return say.T("lovelace.hint.allow")
	}
	for _, u := range f.list.Users {
		if u.ID != user {
			continue
		}
		if f.denied {
			return say.T("lovelace.hint.allow")
		}
		return ""
	}
	return say.T("lovelace.hint.user")
}

func (f *Feature) token(ctx context.Context) (Token, error) {
	t, err := f.o.Controller.DashboardToken(ctx, f.o.Node(), f.o.Settings.Read().User)
	var refused *homeassistant.Failed
	f.mu.Lock()
	was := f.denied
	f.denied = errors.As(err, &refused)
	f.mu.Unlock()
	if f.denied && !was {
		safe.Go("dashboard lists", f.refresh)
	}
	if was != f.denied && f.o.Redraw != nil {
		f.o.Redraw()
	}
	if err != nil {
		return Token{}, err
	}
	return Token{URL: t.URL, Access: t.Access, Expires: t.Expires}, nil
}

func (f *Feature) store(s Setting, v string) error {
	err := setting.Store(f.o.Settings, s, v)
	if err == nil && s.Name == "user" {
		f.mu.Lock()
		f.denied = false
		f.mu.Unlock()
	}
	if err == nil && (s.Name == "user" || s.Name == "theme") {
		f.Home.Restart()
		f.Saver.Restart()
	}
	if f.o.Redraw != nil {
		f.o.Redraw()
	}
	return err
}

func (f *Feature) users() []setting.Option {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]setting.Option, 0, len(f.list.Users))
	for _, u := range f.list.Users {
		out = append(out, setting.Option{Value: u.ID, Label: u.Name})
	}
	return out
}

func (f *Feature) dashboards() []setting.Option {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]setting.Option, 0, len(f.list.Dashboards))
	for _, d := range f.list.Dashboards {
		out = append(out, setting.Option{Value: d.Path, Label: d.Title})
	}
	return out
}

func (f *Feature) themes() []setting.Option {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]setting.Option, 0, len(f.list.Themes))
	for _, t := range f.list.Themes {
		out = append(out, setting.Option{Value: t.Name, Label: t.Name})
	}
	return out
}

func (f *Feature) views(
	dashboard func(schema.Lovelace) schema.LovelaceDashboard,
) func() []setting.Option {
	return func() []setting.Option {
		want := string(dashboard(f.o.Settings.Read()))
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, d := range f.list.Dashboards {
			if d.Path != want {
				continue
			}
			out := make([]setting.Option, 0, len(d.Views))
			for _, v := range d.Views {
				out = append(out, setting.Option{Value: v.Path, Label: v.Title})
			}
			return out
		}
		return nil
	}
}

func (f *Feature) table() *setting.Table[schema.Lovelace] {
	boards := []schema.LovelaceDashboard{""}
	first := []schema.LovelaceView{""}
	rows := []Setting{
		setting.Pick(
			Setting{Name: "user", Group: Group, Icon: "mdi:account", More: f.users},
			[]schema.LovelaceUser{},
			func(c *schema.Lovelace) *schema.LovelaceUser { return (*schema.LovelaceUser)(&c.User) },
		),
		setting.Pick(
			Setting{
				Name:  "dashboard",
				Group: Group,
				Icon:  "mdi:view-dashboard",
				More:  f.dashboards,
			},
			boards,
			func(c *schema.Lovelace) *schema.LovelaceDashboard { return &c.Dashboard },
		),
		setting.Pick(
			Setting{
				Name:  "view",
				Group: Group,
				Icon:  "mdi:view-carousel",
				More: f.views(
					func(c schema.Lovelace) schema.LovelaceDashboard { return c.Dashboard },
				),
			},
			first,
			func(c *schema.Lovelace) *schema.LovelaceView { return &c.View },
		),
		setting.Pick(
			Setting{
				Name:  "saver_dashboard",
				Group: Group,
				Icon:  "mdi:monitor-dashboard",
				More:  f.dashboards,
			},
			boards,
			func(c *schema.Lovelace) *schema.LovelaceDashboard { return &c.SaverDashboard },
		),
		setting.Pick(
			Setting{
				Name:  "saver_view",
				Group: Group,
				Icon:  "mdi:view-carousel-outline",
				More: f.views(
					func(c schema.Lovelace) schema.LovelaceDashboard { return c.SaverDashboard },
				),
			},
			first,
			func(c *schema.Lovelace) *schema.LovelaceView { return &c.SaverView },
		),
		setting.Pick(
			Setting{Name: "theme", Group: Group, Icon: "mdi:palette", More: f.themes},
			[]schema.LovelaceTheme{""},
			func(c *schema.Lovelace) *schema.LovelaceTheme { return &c.Theme },
		),
	}
	return setting.NewTable("lovelace", []setting.Group{Group}, rows,
		setting.Messages{Text: say.T, Missing: say.Missing})
}

func path(dashboard schema.LovelaceDashboard, view schema.LovelaceView) string {
	d := string(dashboard)
	if d == "" {
		d = "lovelace"
	}
	if view == "" {
		return d
	}
	return d + "/" + string(view)
}
