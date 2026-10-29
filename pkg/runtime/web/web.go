package web

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ygelfand/libcountertop/pkg/hook"
	netaddress "github.com/ygelfand/libcountertop/pkg/network/address"
)

const (
	shutdown = 2 * time.Second
	watch    = 5 * time.Second
)

const AdoptURL = "https://my.home-assistant.io/redirect/config_flow_start/?domain=esphome"

type Options struct {
	Port    int
	APIPort int
	Adopted func() bool
}

type side struct {
	name   string
	port   int
	tls    func() *tls.Config
	routes map[string]http.Handler
	open   map[string]http.Handler
}

type Server struct {
	opts Options

	mu     sync.Mutex
	shown  string
	pages  side
	peers  side
	public side
	root   http.Handler

	Offered hook.Hook[string]
}

func New(opts Options) *Server {
	return &Server{
		opts:  opts,
		pages: side{name: "pages", port: opts.Port, routes: map[string]http.Handler{}},
		peers: side{
			name:   "peers",
			routes: map[string]http.Handler{},
			open:   map[string]http.Handler{},
		},
		public: side{name: "secure pages", routes: map[string]http.Handler{}},
	}
}

func (s *Server) Name() string { return "web" }

func (s *Server) Offering() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shown
}

func (s *Server) Handle(pattern string, h http.Handler) { s.add(&s.pages, pattern, h) }

func (s *Server) HandlePeer(pattern string, h http.Handler) {
	s.add(&s.peers, pattern, certified(h))
}

func (s *Server) HandlePeerOpen(pattern string, h http.Handler) {
	s.mu.Lock()
	s.peers.open[pattern] = h
	s.mu.Unlock()
}

func (s *Server) HandlePublic(pattern string, h http.Handler) { s.add(&s.public, pattern, h) }

func (s *Server) add(to *side, pattern string, h http.Handler) {
	s.mu.Lock()
	to.routes[pattern] = h
	s.mu.Unlock()
}

func (s *Server) Secure(port int, cfg func() *tls.Config) {
	s.mu.Lock()
	s.peers.port, s.peers.tls = port, cfg
	s.mu.Unlock()
}

func (s *Server) PublicRoot(h http.Handler) {
	s.mu.Lock()
	s.root = h
	s.mu.Unlock()
}

func (s *Server) Public(port int, cfg func() *tls.Config) {
	s.mu.Lock()
	s.public.port, s.public.tls = port, cfg
	s.mu.Unlock()
}

func (s *Server) Run(ctx context.Context) error {
	s.mu.Lock()
	pages, peers, public, root := s.pages, s.peers, s.public, s.root
	s.mu.Unlock()

	var servers []*http.Server
	var names []side
	for _, sd := range []side{pages, peers, public} {
		if sd.port == 0 || (sd.name != pages.name && sd.tls == nil) {
			continue
		}
		r := chi.NewRouter()
		switch sd.name {
		case pages.name:
			if public.port != 0 && public.tls != nil {
				r.Get("/", front(public.port))
			}
		case public.name:
			if root != nil {
				r.Handle("/*", root)
			}
		}
		for pattern, h := range sd.routes {
			route(r, pattern, h)
		}
		for pattern, h := range sd.open {
			route(r, pattern, h)
		}
		srv := &http.Server{
			Addr:              ":" + strconv.Itoa(sd.port),
			Handler:           r,
			ReadHeaderTimeout: 5 * time.Second,
		}
		if sd.tls != nil {
			srv.TLSConfig = sd.tls()
		}
		servers = append(servers, srv)
		names = append(names, sd)
	}

	done := make(chan error, len(servers))
	for i, srv := range servers {
		go func() {
			slog.Info("serving "+names[i].name, "port", names[i].port)
			var err error
			if srv.TLSConfig != nil {
				err = srv.ListenAndServeTLS("", "")
			} else {
				err = srv.ListenAndServe()
			}
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			done <- err
		}()
	}

	t := time.NewTicker(watch)
	defer t.Stop()

	for {
		s.offer()

		select {
		case err := <-done:
			closeAll(servers)
			return err
		case <-ctx.Done():
			s.hide()

			stop, cancel := context.WithTimeout(context.Background(), shutdown)
			defer cancel()
			var errs []error
			for _, srv := range servers {
				errs = append(errs, srv.Shutdown(stop))
			}
			return errors.Join(errs...)
		case <-t.C:
		}
	}
}

func route(r chi.Router, pattern string, h http.Handler) {
	if method, path, ok := strings.Cut(pattern, " "); ok {
		r.Method(method, path, h)
		return
	}
	r.Handle(pattern, h)
}

func certified(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func front(port int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(r.Host); err == nil {
			host = h
		}
		if port != 443 {
			host = net.JoinHostPort(host, strconv.Itoa(port))
		}
		http.Redirect(w, r, "https://"+host+"/", http.StatusFound)
	}
}

func closeAll(servers []*http.Server) {
	for _, srv := range servers {
		_ = srv.Close()
	}
}

func (s *Server) offer() {
	if s.opts.Adopted() {
		s.hide()
		return
	}

	ip := address()
	if ip == "" {
		return
	}
	addr := net.JoinHostPort(ip, strconv.Itoa(s.opts.APIPort))

	s.mu.Lock()
	if s.shown == addr {
		s.mu.Unlock()
		return
	}
	s.shown = addr
	s.mu.Unlock()
	slog.Info("waiting to be adopted", "at", addr)
	s.Offered.Emit(addr)
}

func (s *Server) hide() {
	s.mu.Lock()
	if s.shown == "" {
		s.mu.Unlock()
		return
	}
	s.shown = ""
	s.mu.Unlock()
	s.Offered.Emit("")
}

func address() string {
	for _, ip := range netaddress.Addresses() {
		if ip.To4() != nil {
			return ip.String()
		}
	}
	return ""
}
