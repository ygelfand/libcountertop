package webapi

import (
	"cmp"
	"encoding/json"
	"errors"
	"image"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/input/textentry"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const (
	Prefix   = "/api/v1"
	Group    = setting.Group("Frontend")
	bodyMost = 64 << 10
)

type Device struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Model   string   `json:"model"`
	Board   string   `json:"board"`
	Version string   `json:"version"`
	Pages   []string `json:"pages"`
	Screen  bool     `json:"screen"`
}

type States interface {
	Listen(fn func(esphome.State)) (cancel func())
	States() []esphome.State
	Descriptions() []esphome.Description
	Press(object string) error
	Actions() []esphome.Action
	RunAction(name string, c esphome.Call) (any, error)
}

type Logo struct {
	Light []byte
	Dark  []byte
}

type Options struct {
	Settings storage.Section[schema.Frontend]
	Logo     Logo
	Registry *setting.Registry
	States   States
	Device   func() Device
	Pages    map[string]http.Handler
	LogTag   string
	Screen   func() (image.Image, error)
	DeviceID uint32
	Now      func() time.Time
	Address  func() string
	Input    *textentry.Controller
}

type API struct {
	o       Options
	knobs   *setting.Controls[schema.Frontend]
	s       sessions
	shot    screenCache
	control controlAccess
}

func New(o Options) *API {
	if o.Now == nil {
		o.Now = time.Now
	}
	a := &API{o: o, s: sessions{revoked: map[string]time.Time{}, failed: map[string][]time.Time{}}}
	a.knobs = &setting.Controls[schema.Frontend]{
		Table:  NewTable(),
		Device: o.DeviceID,
		Read:   o.Settings.Read,
		Save: func(s setting.Setting[schema.Frontend], v string) error {
			return setting.Store(o.Settings, s, v)
		},
	}
	return a
}

func NewTable() *setting.Table[schema.Frontend] {
	password := setting.Words(
		setting.Setting[schema.Frontend]{
			Name: "password", Group: Group, ID: "frontend_password",
			Icon: "mdi:form-textbox-password",
		},
		passwordMost,
		func(c *schema.Frontend) *string { return &c.Password },
	)
	password.Kind = setting.Credential
	return setting.NewTable(
		"access",
		[]setting.Group{Group},
		[]setting.Setting[schema.Frontend]{password},
		setting.Messages{Text: say.T, Missing: say.Missing},
	)
}

const passwordMost = 128

func (a *API) Name() string { return "webapi" }

func (a *API) Controls() *setting.Controls[schema.Frontend] { return a.knobs }

func (a *API) Password() string { return a.o.Settings.Read().Password }

func (a *API) Input() *textentry.Controller { return a.o.Input }

func (a *API) SetPassword(v string) error { return a.knobs.Change("password", v) }

func (a *API) Entities() []esphome.Entity { return a.knobs.Entities() }

func (a *API) Restore(c schema.Frontend) {
	if c.Password == "" {
		password, err := newPassword()
		if err != nil {
			slog.Error("making the admin password failed", "err", err)
			return
		}
		if err := a.knobs.Change("password", password); err != nil {
			slog.Error("saving the admin password failed", "err", err)
			return
		}
		slog.Info("admin password made")
	}
	a.knobs.PublishFrom(a.o.Settings.Read())
}

func (a *API) Routes() map[string]http.Handler {
	auth := func(h http.HandlerFunc) http.Handler { return a.require(h) }
	routes := map[string]http.Handler{
		"POST " + Prefix + "/login":                   http.HandlerFunc(a.login),
		"POST " + Prefix + "/logout":                  http.HandlerFunc(a.logout),
		"GET " + Prefix + "/logo/{tone}":              http.HandlerFunc(a.logo),
		"GET " + Prefix + "/name":                     http.HandlerFunc(a.name),
		"GET " + Prefix + "/device":                   auth(a.device),
		"GET " + Prefix + "/settings":                 auth(a.list),
		"GET " + Prefix + "/settings/{domain}/{name}": auth(a.read),
		"PUT " + Prefix + "/settings/{domain}/{name}": auth(a.write),
		"GET " + Prefix + "/states":                   auth(a.states),
		"GET " + Prefix + "/entities":                 auth(a.entities),
		"GET " + Prefix + "/events":                   auth(a.events),
		"GET " + Prefix + "/actions":                  auth(a.listActions),
		"POST " + Prefix + "/actions/{name}":          auth(a.runAction),
		"POST " + Prefix + "/buttons/{id}":            auth(a.press),
		"GET " + Prefix + "/logs":                     auth(a.logs),
		"GET " + Prefix + "/diagnostics":              auth(a.diagnostics),
		"GET " + Prefix + "/screen":                   a.requireControl(http.HandlerFunc(a.screen)),
		"POST " + Prefix + "/control/pairing":         auth(a.pairControl),
		"POST " + Prefix + "/control/pairing/redeem":  http.HandlerFunc(a.redeemControl),
		"GET " + Prefix + "/control/session": a.requireInput(
			http.HandlerFunc(a.controlInfo),
		),
		"DELETE " + Prefix + "/control/session": a.requireInput(
			http.HandlerFunc(a.endControl),
		),
		"GET " + Prefix + "/control/keyboard":  a.requireInput(http.HandlerFunc(a.keyboard)),
		"POST " + Prefix + "/control/keyboard": a.requireInput(http.HandlerFunc(a.keyboard)),
		"GET " + Prefix + "/devices/{board}":   auth(a.picture),
	}
	for name, h := range a.o.Pages {
		at := Prefix + "/" + name
		inner := a.require(http.StripPrefix(at, h))
		if name == "desktop" {
			inner = a.requireControl(http.StripPrefix(at, h))
		}
		for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
			routes[method+" "+at] = inner
			routes[method+" "+at+"/*"] = inner
		}
	}
	return routes
}

func (a *API) Handler() http.Handler {
	r := chi.NewRouter()
	for pattern, h := range a.Routes() {
		method, path, _ := strings.Cut(pattern, " ")
		r.Method(method, path, h)
	}
	return r
}

type domainFields struct {
	Domain     string          `json:"domain"`
	SectionKey string          `json:"section_key"`
	Section    string          `json:"section"`
	Fields     []setting.Field `json:"fields"`
	order      int
}

func (a *API) device(w http.ResponseWriter, _ *http.Request) {
	d := a.o.Device()
	d.Pages = slices.Sorted(maps.Keys(a.o.Pages))
	d.Screen = a.o.Screen != nil
	reply(w, d)
}

func (a *API) name(w http.ResponseWriter, _ *http.Request) {
	reply(w, map[string]string{"name": a.o.Device().Name})
}

func (a *API) logo(w http.ResponseWriter, r *http.Request) {
	png := a.o.Logo.Dark
	if chi.URLParam(r, "tone") == "light" {
		png = a.o.Logo.Light
	}
	if len(png) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(png)
}

func (a *API) list(w http.ResponseWriter, _ *http.Request) {
	var out []domainFields
	for _, b := range a.o.Registry.All() {
		d, ok := b.(setting.Described)
		if !ok {
			continue
		}
		order, key := sectionOf(b.Domain())
		out = append(out, domainFields{
			Domain:     b.Domain(),
			SectionKey: key,
			Section:    say.T(key),
			Fields:     d.Fields(),
			order:      order,
		})
	}
	slices.SortStableFunc(out, func(x, y domainFields) int { return cmp.Compare(x.order, y.order) })
	reply(w, out)
}

func (a *API) find(r *http.Request) (setting.Bound, setting.Field, bool) {
	domain, name := chi.URLParam(r, "domain"), chi.URLParam(r, "name")
	for _, b := range a.o.Registry.All() {
		if b.Domain() != domain {
			continue
		}
		d, ok := b.(setting.Described)
		if !ok {
			return nil, setting.Field{}, false
		}
		for _, f := range d.Fields() {
			if f.Name == name {
				return b, f, true
			}
		}
	}
	return nil, setting.Field{}, false
}

func (a *API) read(w http.ResponseWriter, r *http.Request) {
	_, f, ok := a.find(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	reply(w, f)
}

func (a *API) write(w http.ResponseWriter, r *http.Request) {
	b, f, ok := a.find(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Value *string `json:"value"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, bodyMost)).Decode(&body); err != nil ||
		body.Value == nil {
		http.Error(w, "send a value", http.StatusBadRequest)
		return
	}
	if err := b.Change(f.Name, *body.Value); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	_, f, _ = a.find(r)
	reply(w, f)
}

func (a *API) states(w http.ResponseWriter, _ *http.Request) {
	if a.o.States == nil {
		reply(w, []esphome.State{})
		return
	}
	reply(w, a.o.States.States())
}

func (a *API) entities(w http.ResponseWriter, _ *http.Request) {
	if a.o.States == nil {
		reply(w, []esphome.Description{})
		return
	}
	reply(w, a.o.States.Descriptions())
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil && !errors.Is(err, http.ErrHandlerTimeout) {
		slog.Debug("writing an API reply failed", "err", err)
	}
}
