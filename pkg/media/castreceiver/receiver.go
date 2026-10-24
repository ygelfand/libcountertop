package castreceiver

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync/atomic"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/media/cast"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/runtime/startup"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

type ReceiverOptions struct {
	Settings storage.Section[schema.Cast]
	DeviceID uint32
	Network  func(ctx context.Context) bool
	Server   Config
}

type Receiver struct {
	settings storage.Section[schema.Cast]
	network  func(ctx context.Context) bool

	knobs *Controls

	server *Server

	boot atomic.Pointer[startup.Progress]

	oracle  *esphome.Text
	bundle  *esphome.Text
	source  *esphome.TextSensor
	expires *esphome.Sensor
}

func NewReceiver(opts ReceiverOptions) *Receiver {
	r := &Receiver{settings: opts.Settings, network: opts.Network}
	c := opts.Settings.Read()

	r.knobs = &Controls{
		Table:  NewTable(),
		Device: opts.DeviceID,
		Read:   opts.Settings.Read,
		Save:   r.save,
	}
	r.oracle = &esphome.Text{
		Base: esphome.Base{
			ObjectID: "cast_oracle",
			Name:     "Cast oracle",
			Icon:     "mdi:certificate",
			Category: esphome.CategoryConfig,
		},
		MaxLength: 255,
	}
	r.bundle = &esphome.Text{
		Base: esphome.Base{
			ObjectID: "cast_bundle",
			Name:     "Cast bundle",
			Icon:     "mdi:certificate",
			Category: esphome.CategoryConfig,
		},
		MaxLength: 255,
	}
	r.source = &esphome.TextSensor{
		Base: esphome.Base{
			ObjectID: "cast_credentials",
			Name:     "Cast credentials",
			Icon:     "mdi:certificate",
			Category: esphome.CategoryDiagnostic,
		},
	}
	r.expires = &esphome.Sensor{
		Base: esphome.Base{
			ObjectID: "cast_credentials_expire",
			Name:     "Cast credentials expire",
			Icon:     "mdi:certificate-outline",
			Category: esphome.CategoryDiagnostic,
		},
		DeviceClass: "timestamp",
	}
	for _, b := range []*esphome.Base{
		&r.oracle.Base, &r.bundle.Base, &r.source.Base, &r.expires.Base,
	} {
		b.DeviceID = opts.DeviceID
	}

	r.oracle.Set(c.Oracle)
	r.oracle.OnCommand = r.SetOracle
	r.bundle.Set(c.Bundle)
	r.bundle.OnCommand = r.SetBundle

	server := opts.Server
	server.Oracle = func() string { return r.settings.Read().Oracle }
	server.Bundle = func() string { return r.settings.Read().Bundle }
	server.Credentials = r.credentials
	r.server = New(server)
	return r
}

func (r *Receiver) write() schema.CastWriter { return schema.NewCastWriter(r.settings) }

func (r *Receiver) Name() string { return "cast receiver" }

func (r *Receiver) Entities() []esphome.Entity {
	return append(r.knobs.Entities(), r.oracle, r.bundle, r.source, r.expires)
}

func (r *Receiver) Settings() *Controls { return r.knobs }

func (r *Receiver) save(s Setting, v string) error {
	was := r.settings.Read()
	if err := setting.Store(r.settings, s, v); err != nil {
		return err
	}
	r.apply(was, r.settings.Read())
	return nil
}

func (r *Receiver) apply(was, now schema.Cast) {
	switch {
	case was.Receiver != now.Receiver:
		r.turn(now.Receiver)
	case was.YouTube.OnDemand != now.YouTube.OnDemand && now.Receiver:
		r.server.Down()
		if err := r.server.Up(); err != nil {
			slog.Error("the cast receiver would not start", "err", err)
		}
	}
}

func (r *Receiver) change(name, v string) {
	if err := r.knobs.Change(name, v); err != nil {
		slog.Error("saving a setting failed", "setting", "cast "+name, "err", err)
	}
}

func (r *Receiver) SetSkip(text string) { r.change("youtube.skip", text) }

func (r *Receiver) SetOracle(url string) {
	url = strings.TrimSpace(url)
	if err := r.write().Oracle(url); err != nil {
		slog.Error("saving the cast oracle failed", "err", err)
		return
	}
	r.oracle.Set(url)
	r.server.Retarget()
}

func (r *Receiver) SetBundle(url string) {
	url = strings.TrimSpace(url)
	if err := r.write().Bundle(url); err != nil {
		slog.Error("saving the cast bundle failed", "err", err)
		return
	}
	r.bundle.Set(url)
	r.server.Retarget()
}

func (r *Receiver) credentials(c *cast.Credentials) {
	if !c.Remote {
		r.source.Set("self-signed")
		r.expires.Set(float32(math.NaN()))
		return
	}
	r.source.Set("remote-signed")
	r.expires.Set(float32(c.NotAfter.Unix()))
}

func (r *Receiver) Enabled() bool { return r.settings.Read().Receiver }

func (r *Receiver) Listening() bool { return r.server.Listening() }

func (r *Receiver) Start(context.Context) error {
	if !r.Enabled() {
		r.booted(startup.Progress{Done: true, Doing: "off"})
		return nil
	}
	r.booted(startup.Progress{Doing: "waiting for the network"})
	return nil
}

func (r *Receiver) Run(ctx context.Context) error {
	if !r.Enabled() {
		<-ctx.Done()
		return nil
	}
	keys := make(chan error, 1)
	safe.Go("cast keys", func() { keys <- r.server.Prepare() })
	if r.network(ctx) && r.Enabled() {
		var err error
		select {
		case err = <-keys:
		default:
			r.booted(startup.Progress{Doing: "making keys"})
			err = <-keys
		}
		if err == nil {
			err = r.server.Up()
		}
		if err != nil {
			slog.Error("the cast receiver would not start", "err", err)
			r.booted(startup.Progress{Failed: true, Doing: err.Error()})
		} else {
			r.booted(startup.Progress{Done: true, Doing: fmt.Sprintf("port %d", cast.Port)})
		}
	}
	<-ctx.Done()
	return nil
}

func (r *Receiver) booted(p startup.Progress) { r.boot.Store(&p) }

func (r *Receiver) Startup() startup.Progress {
	if p := r.boot.Load(); p != nil {
		return *p
	}
	return startup.Progress{Doing: "waiting for the network"}
}

func (r *Receiver) Close() error {
	r.server.Down()
	return nil
}

func (r *Receiver) SetReceiver(on bool) { r.change("receiver", setting.OnOff(on)) }

func (r *Receiver) turn(on bool) {
	if !on {
		r.server.Down()
		r.booted(startup.Progress{Done: true, Doing: "off"})
		return
	}
	if err := r.server.Up(); err != nil {
		slog.Error("the cast receiver would not start", "err", err)
		r.booted(startup.Progress{Failed: true, Doing: err.Error()})
		return
	}
	r.booted(startup.Progress{Done: true, Doing: fmt.Sprintf("port %d", cast.Port)})
}

func (r *Receiver) Playing() *cast.Media { return r.server.Playing() }
