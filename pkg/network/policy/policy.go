package policy

import (
	"log/slog"
	"net/http"
	"slices"
	"sync"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/fetch"
	"github.com/ygelfand/libcountertop/pkg/hook"
	"github.com/ygelfand/libcountertop/pkg/network/releases"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

type (
	Setting = setting.Setting[schema.Network]
	Table   = setting.Table[schema.Network]
)

const Group setting.Group = "Network"

const keyMost = 256

func NewTable() *Table {
	home := setting.Words(
		Setting{Name: "home_key", Group: Group, Icon: "mdi:home-lock"},
		keyMost,
		func(c *schema.Network) *string { return &c.HomeKey },
	)
	home.Kind = setting.Secret
	return setting.NewTable("network", []setting.Group{Group}, []Setting{
		setting.Flag(Setting{
			Name: "verify", Group: Group, ID: "verify_certificates", Icon: "mdi:certificate",
		}, func(c *schema.Network) *bool { return &c.Verify }),
		setting.Flag(Setting{
			Name: "via_home", Group: Group, Icon: "mdi:home-import-outline",
		}, func(c *schema.Network) *bool { return &c.ViaHome }),
		home,
	}, setting.Messages{Text: say.T, Missing: say.Missing})
}

type Policy struct {
	settings storage.Section[schema.Network]
	knobs    *setting.Controls[schema.Network]

	mu   sync.Mutex
	home http.RoundTripper

	Changed hook.Hook[schema.Network]
}

func New(settings storage.Section[schema.Network]) *Policy {
	p := &Policy{settings: settings}
	p.knobs = &setting.Controls[schema.Network]{
		Table: NewTable(),
		Read:  settings.Read,
		Save:  p.save,
	}
	return p
}

func (p *Policy) Name() string                                { return "network" }
func (p *Policy) Entities() []esphome.Entity                  { return p.knobs.Entities() }
func (p *Policy) Settings() *setting.Controls[schema.Network] { return p.knobs }
func (p *Policy) Restore(verify bool)                         { p.apply(verify) }
func (p *Policy) Verifying() bool                             { return !fetch.Skipping() }

func (p *Policy) HomeKey() string { return p.settings.Read().HomeKey }

func (p *Policy) DownloadThrough(home http.RoundTripper) {
	p.mu.Lock()
	p.home = home
	p.mu.Unlock()
	p.route()
}

func (p *Policy) route() {
	p.mu.Lock()
	home := p.home
	p.mu.Unlock()
	if home == nil || !p.settings.Read().ViaHome {
		fetch.Through(nil)
		return
	}
	hosts := releases.Hosts()
	fetch.Through(func(r *http.Request) http.RoundTripper {
		if slices.Contains(hosts, r.URL.Hostname()) {
			return home
		}
		return nil
	})
}

func (p *Policy) SetHomeKey(key string) error {
	return p.save(p.knobs.Table.MustRow("home_key"), key)
}

func (p *Policy) SetVerify(verify bool) {
	if err := p.save(p.knobs.Table.MustRow("verify"), setting.OnOff(verify)); err != nil {
		slog.Error("saving certificate policy failed", "err", err)
	}
}

func (p *Policy) SetViaHome(on bool) {
	if err := p.save(p.knobs.Table.MustRow("via_home"), setting.OnOff(on)); err != nil {
		slog.Error("saving the download route failed", "err", err)
	}
}

func (p *Policy) save(s Setting, v string) error {
	err := setting.Store(p.settings, s, v)
	p.apply(p.settings.Read().Verify)
	p.Changed.Emit(p.settings.Read())
	return err
}

func (p *Policy) apply(verify bool) {
	fetch.Verify(verify)
	p.route()
	at := p.settings.Read()
	at.Verify = verify
	p.knobs.PublishFrom(at)
	if !verify {
		slog.Warn("certificates are not being verified")
	}
}
