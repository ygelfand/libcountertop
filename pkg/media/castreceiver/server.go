package castreceiver

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/fetch"
	"github.com/ygelfand/libcountertop/pkg/media/cast"
	"github.com/ygelfand/libcountertop/pkg/media/cast/preferences"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
	"github.com/ygelfand/libcountertop/pkg/media/urlplayer"
	mdns "github.com/ygelfand/libcountertop/pkg/network/advertise"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
)

const describeWait = 5 * time.Second

type Config struct {
	Name         func() string
	Manufacturer string
	Model        string
	Version      string
	Hardware     func() string
	Host         func(name string) string
	Logo         func() []byte
	Address      func() string
	Timezone     func() string

	CredentialsPath string
	BundlePath      string
	AuthorityPath   string
	AppDir          string
	Oracle          func() string
	Bundle          func() string
	Synced          func(do func()) (stop func())
	Credentials     func(*cast.Credentials)
	Notice          func(title, body string)

	Volume   Volume
	Video    func() playback.Target
	Output   func(ended func()) playback.Output
	Resample func(from int) func(stereo []int16) []int16
	Apps     []cast.Maker

	Preferences preferences.Provider
}

type Server struct {
	config Config

	mu       sync.Mutex
	service  *cast.Service
	listener net.Listener
	setup    *http.Server
	advert   *cast.Advertiser
	stop     context.CancelFunc

	loud    *loudness
	refused *refused

	keys      *keys
	protocols []cast.Protocol
	player    *urlplayer.Player
}

func New(c Config) *Server {
	return &Server{config: c, loud: newLoudness(c.Volume), refused: newRefused()}
}

func (s *Server) current() *cast.Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.service
}

func (s *Server) Setup() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		setup := s.setup
		s.mu.Unlock()
		if setup == nil {
			http.NotFound(w, r)
			return
		}
		setup.Handler.ServeHTTP(w, r)
	})
}

func (s *Server) Retarget() {
	s.mu.Lock()
	k := s.keys
	s.mu.Unlock()
	if k != nil {
		k.Retarget()
	}
}

func (s *Server) keyed(name string) error {
	if s.keys != nil {
		return nil
	}
	k, err := newKeys(
		name,
		s.config.CredentialsPath,
		s.config.BundlePath,
		s.config.AuthorityPath,
	)
	if err != nil {
		return err
	}
	k.oracle = s.config.Oracle
	k.bundle = s.config.Bundle
	k.synced = s.config.Synced
	k.changed = s.config.Credentials
	s.config.Credentials(k.Current())
	s.keys = k
	return nil
}

func (s *Server) Prepare() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keyed(s.config.Name())
}

func (s *Server) Up() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener != nil {
		return nil
	}

	name := s.config.Name()
	if err := s.keyed(name); err != nil {
		return err
	}

	listener, err := tls.Listen("tcp", fmt.Sprintf(":%d", cast.Port), cast.TLS(s.keys.Current))
	if err != nil {
		return fmt.Errorf("cast: listening on %d: %w", cast.Port, err)
	}

	profile := cast.SmartDisplay
	if s.config.Video == nil {
		profile = cast.SmartSpeaker
	}
	device := cast.Identify(s.config.Hardware(), name, s.config.Model, profile)

	advert, err := cast.NewAdvertiser(device, cast.Port)
	if err != nil {
		_ = listener.Close()
		return err
	}

	service := cast.NewService(cast.NewReceiver(name))
	service.Fault = func(err error) { slog.Debug("a cast sender sent something unreadable", "err", err) }

	unspoken := map[string]bool{}
	describer := fetch.Client(describeWait)
	service.Receiver.Describe = func(app string) cast.AppConfig {
		c, err := cast.Describe(context.Background(), describer, app)
		if err != nil {
			slog.Info("a cast app has no config", "id", app, "err", err)
		}
		return c
	}
	service.Receiver.Launched = func(app cast.App) {
		unspoken = map[string]bool{}
		slog.Info("a cast app launched", "app", app.Name, "id", app.ID)
		advert.Playing(true, app.Name)
	}
	service.Receiver.Stopped = func(string) { advert.Playing(false, "") }
	service.Receiver.Loaded = func(m cast.Media) {
		slog.Info("a cast sender loaded", "type", m.ContentType, "stream", m.StreamType,
			"content", contentShape(m.ContentID), "custom", len(m.CustomData) > 0)
	}
	service.Receiver.Unspoken = func(namespace, kind string) {
		if unspoken[namespace] {
			return
		}
		unspoken[namespace] = true
		slog.Debug(
			"a cast sender used a namespace this does not speak",
			"namespace",
			namespace,
			"type",
			kind,
		)
	}
	service.Receiver.Volume = s.loud.asked
	service.Receiver.Credentials = s.keys.Current
	service.Receiver.CRL = s.keys.CRL
	service.Receiver.Identity = func() cast.Device { return device }
	service.Receiver.Available = s.refused.allows
	service.Receiver.Refused = s.tellRefused
	if s.protocols == nil {
		env := s.env(name, device)
		s.protocols = cast.Protocols(env, s.config.Apps)
		if env.Output != nil {
			s.player = urlplayer.New(urlplayer.Options{
				HTTP:     env.Long,
				Output:   env.Output,
				Resample: env.Resample,
				Screen:   env.Video != nil,
				Target:   env.Video,
			})
		}
	}
	for _, p := range s.protocols {
		service.Receiver.Register(p)
	}
	if s.player != nil {
		service.Receiver.Player = s.player
	}
	started := time.Now()
	service.Receiver.Eureka = func() cast.Eureka {
		e := cast.NewEureka(device, s.config.Manufacturer, s.config.Version)
		e.IP = s.config.Address()
		e.Timezone = s.config.Timezone()
		e.Uptime = time.Since(started).Seconds()
		return e
	}
	service.Receiver.Setup = func(kind string, data json.RawMessage) {
		slog.Debug("a cast sender sent a setup request", "type", kind, "data", string(data))
	}
	service.Receiver.Challenged = func(c cast.Challenge, answered bool) {
		slog.Debug(
			"a cast sender asked for device authentication",
			"hash",
			c.Hash,
			"algorithm",
			c.Algorithm,
			"nonce",
			len(c.Nonce) > 0,
			"answered",
			answered,
		)
	}

	ctx, stop := context.WithCancel(context.Background())
	keys := s.keys
	safe.Go("cast credentials", func() { keys.run(ctx) })
	for _, p := range s.protocols {
		safe.Go("cast protocol "+p.Name(), func() { p.Run(ctx) })
	}

	s.setup = &http.Server{
		Addr:              fmt.Sprintf(":%d", cast.SetupPort),
		ReadHeaderTimeout: 5 * time.Second,
		Handler: cast.SetupHandler(
			service.Receiver.Eureka,
			s.config.Logo,
			func(req *http.Request, status int) {
				slog.Debug(
					"a cast sender asked the setup endpoint",
					"peer",
					req.RemoteAddr,
					"method",
					req.Method,
					"url",
					req.URL.String(),
					"status",
					status,
				)
			},
		),
	}
	setup := s.setup
	safe.Go("cast setup", func() {
		if err := setup.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("the cast setup endpoint stopped", "port", cast.SetupPort, "err", err)
		}
	})

	s.listener, s.advert, s.service, s.stop = listener, advert, service, stop
	s.loud.serve(service)
	safe.Go("cast accept", func() { accept(ctx, listener, service) })

	host := s.config.Host(name)
	safe.Go("cast advertise", func() {
		mdns.Advertise(ctx, "cast", func(ips []net.IP) (func(), error) {
			if err := advert.On(host, mdns.Strings(ips)); err != nil {
				return nil, err
			}
			return advert.Close, nil
		})
	})

	slog.Info("the cast receiver is up", "name", name, "port", cast.Port)
	return nil
}

func (s *Server) env(name string, device cast.Device) cast.Env {
	v := s.config.Volume
	return cast.Env{
		Name:      name,
		Model:     s.config.Model,
		Device:    device,
		HTTP:      fetch.Client(30 * time.Second),
		Long:      fetch.Client(0),
		Video:     s.config.Video,
		Volume:    v.Level,
		SetVolume: v.SetLevel,
		VolumeChanged: func(do func()) func() {
			return v.Listen(func(int) { do() })
		},
		Publish: func(control cast.Controls, p *cast.Published) {
			if svc := s.current(); svc != nil {
				svc.Publish(control, p)
			}
		},
		Advertise: func(app, name string, namespaces []string) {
			if svc := s.current(); svc != nil {
				svc.Advertise(app, name, namespaces)
			}
		},
		End: func() {
			if svc := s.current(); svc != nil {
				svc.End()
			}
		},
		Refuse: func(app string) {
			s.refused.add(app)
			s.tellRefused(app)
		},
		Keep: store(s.config.AppDir),
		Send: func(m cast.Message) {
			if svc := s.current(); svc != nil {
				svc.Send(m)
			}
		},
		Output: s.config.Output(func() {
			if svc := s.current(); svc != nil {
				svc.Done()
			}
		}),
		Resample:    s.config.Resample,
		Preferences: s.config.Preferences,
	}
}

func contentShape(id string) string {
	if u, err := url.Parse(id); err == nil && u.Host != "" &&
		(u.Scheme == "http" || u.Scheme == "https") {
		return u.Scheme + "://" + u.Host
	}
	return fmt.Sprintf("opaque, %d characters", len(id))
}

func accept(ctx context.Context, listener net.Listener, service *cast.Service) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			slog.Warn("a cast sender could not be accepted", "err", err)
			continue
		}

		safe.Go("cast sender", func() {
			c := cast.NewConn(conn)
			peer := c.Peer()
			c.Trace = func(in bool, m cast.Message) { trace(peer, in, m) }
			slog.Debug("cast connection opened", "peer", peer)
			err := service.Serve(c)
			slog.Debug("cast connection closed", "peer", peer, "err", err)
		})
	}
}

func trace(peer string, in bool, m cast.Message) {
	if m.Namespace == cast.NSHeartbeat || m.Namespace == cast.NSDeviceAuth {
		return
	}
	dir := "out"
	if in {
		dir = "in"
	}
	body := m.Payload
	if m.Binary != nil {
		body = hex.EncodeToString(m.Binary)
	}
	slog.Debug("cast "+dir, "peer", peer, "from", m.Source, "to", m.Destination,
		"namespace", m.Namespace, "body", body)
}

func (s *Server) Down() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.loud.serve(nil)
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
	if s.advert != nil {
		s.advert.Close()
		s.advert = nil
	}
	if s.listener != nil {
		_ = s.listener.Close()
		s.listener = nil
	}
	if s.setup != nil {
		_ = s.setup.Close()
		s.setup = nil
	}
	if s.service != nil {
		s.service.Receiver.Forget()
		s.service.Close()
		s.service = nil
	}
}

func (s *Server) Listening() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listener != nil
}

func (s *Server) Playing() *cast.Media {
	if svc := s.current(); svc != nil {
		return svc.Receiver.Media()
	}
	return nil
}
