package nativeapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/runtime/component"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

// Home Assistant drops the connection after 4.5 keepalive intervals.
const writeTimeout = 20 * time.Second

const dialDelay = 10 * time.Second

type Registry interface {
	Entities() []esphome.Entity
	Actions() []*esphome.Action
	Handlers() []esphome.Handler
}

type Adoption interface {
	Adopted() bool
	SetAdopted(on bool) error
}

type Identity struct {
	Addr     string
	Platform string
	Board    string
	Info     esphome.Info
}

type Dependencies struct {
	KeyPath      string
	DialPath     string
	Identity     func() (Identity, error)
	Bluetooth    func() esphome.BluetoothFeature
	Registry     Registry
	Adoption     Adoption
	Logs         LogSource
	OnSubscribed func()
}

type Server struct {
	deps Dependencies

	srv  *esphome.Server
	id   Identity
	ents atomic.Pointer[esphome.Entities]

	reconnect chan struct{}
	announced sync.Once
	port      atomic.Int32

	knobs *setting.Controls[schema.API]
}

func New(deps Dependencies) *Server {
	a := &Server{deps: deps, reconnect: make(chan struct{}, 1)}
	a.knobs = a.controls()
	return a
}

func (a *Server) Name() string { return "api" }

func (a *Server) Listen(fn func(esphome.State)) (cancel func()) {
	if ents := a.ents.Load(); ents != nil {
		return ents.Listen(fn)
	}
	return func() {}
}

func (a *Server) States() []esphome.State {
	if ents := a.ents.Load(); ents != nil {
		return ents.States()
	}
	return nil
}

func (a *Server) Actions() []esphome.Action {
	if ents := a.ents.Load(); ents != nil {
		return ents.Actions()
	}
	return nil
}

func (a *Server) RunAction(name string, c esphome.Call) (any, error) {
	ents := a.ents.Load()
	if ents == nil {
		return nil, errors.New("nativeapi: not serving yet")
	}
	return ents.Run(name, c)
}

func (a *Server) Press(object string) error {
	ents := a.ents.Load()
	if ents == nil {
		return errors.New("nativeapi: not serving yet")
	}
	return ents.Press(object)
}

func (a *Server) Descriptions() []esphome.Description {
	if ents := a.ents.Load(); ents != nil {
		return ents.Descriptions()
	}
	return nil
}

func (a *Server) Startup() component.Progress {
	port := a.port.Load()
	if port == 0 {
		return component.Progress{Doing: "opening the port"}
	}
	return component.Progress{Done: true, Doing: fmt.Sprintf("port %d", port)}
}

func (a *Server) Start(context.Context) error {
	psk, err := loadPSK(a.deps.KeyPath)
	if err != nil {
		return err
	}
	id, err := a.deps.Identity()
	if err != nil {
		return err
	}
	a.id = id
	a.srv = &esphome.Server{
		Addr:               id.Addr,
		WriteTimeout:       writeTimeout,
		PSK:                psk,
		Logger:             slog.Default(),
		OnSetEncryptionKey: func(k esphome.PSK) error { return writePSK(a.deps.KeyPath, k) },
		OnSubscribed:       a.subscribed,
	}
	if path := a.deps.DialPath; path != "" {
		a.srv.Dial = &esphome.Dial{
			Delay: dialDelay,
			Load:  func() (netip.Addr, bool) { return loadTarget(path) },
			Save:  func(addr netip.Addr) error { return writeTarget(path, addr) },
		}
	}
	return a.register()
}

func (a *Server) register() error {
	registered := a.deps.Registry.Entities()
	ents := esphome.NewEntities()
	if err := ents.Add(registered...); err != nil {
		return err
	}
	if err := ents.AddActions(a.deps.Registry.Actions()...); err != nil {
		return err
	}
	a.ents.Store(ents)
	info := a.id.Info
	info.Devices = SubDevices(info.Devices, registered)
	if a.deps.Bluetooth != nil {
		info.BluetoothFeatures = a.deps.Bluetooth()
	}
	a.srv.Info = info
	a.srv.Handler = esphome.Chain(append([]esphome.Handler{ents}, a.deps.Registry.Handlers()...)...)
	return nil
}

func (a *Server) Run(ctx context.Context) error {
	defer a.port.Store(0)
	if a.deps.Logs != nil {
		safe.Go("logs", func() { a.pipeLogs(ctx) })
	}

	for {
		select {
		case <-a.reconnect:
		default:
		}
		if err := a.register(); err != nil {
			return err
		}
		ln, err := net.Listen("tcp", a.srv.Addr)
		if err != nil {
			return fmt.Errorf("api: listen %s: %w", a.srv.Addr, err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		a.port.Store(int32(port))
		slog.Info("serving", "addr", ln.Addr(), "node", a.srv.Info.Name,
			"bluetooth", a.srv.Info.BluetoothFeatures)

		a.announced.Do(func() {
			safe.Go("mdns", func() { a.advertise(ctx, port) })
		})

		serving, stop := context.WithCancel(ctx)
		go func() {
			select {
			case <-a.reconnect:
			case <-serving.Done():
			}
			stop()
		}()

		err = a.srv.Serve(serving, ln)
		stop()
		a.port.Store(0)

		if err != nil || ctx.Err() != nil {
			return err
		}
	}
}

func (a *Server) Fire(e component.Event) {
	if a.srv == nil {
		return
	}
	if err := a.srv.FireEvent(e.Name, e.Data); err != nil {
		slog.Debug("firing an event failed", "event", e.Name, "err", err)
	}
}

func (a *Server) Reconnect() {
	select {
	case a.reconnect <- struct{}{}:
	default:
	}
}

func (a *Server) Adopted() bool { return a.deps.Adoption.Adopted() }

func (a *Server) subscribed() {
	if !a.deps.Adoption.Adopted() {
		if err := a.deps.Adoption.SetAdopted(true); err != nil {
			slog.Error("recording adoption failed", "err", err)
		} else {
			slog.Info("adopted")
		}
	}
	if a.deps.OnSubscribed != nil {
		a.deps.OnSubscribed()
	}
}
