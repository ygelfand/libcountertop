package certwarden

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/fetch"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const (
	chainPath    = "/certwarden/api/v1/download/privatecertchains/"
	refreshEvery = 6 * time.Hour
	retryEvery   = 5 * time.Minute
	fetchWait    = 30 * time.Second
	bodyMost     = 1 << 20
)

var (
	ErrRefused = errors.New("certwarden: the server refused the API key")
	ErrExpired = errors.New("certwarden: the certificate has expired")
)

type Options struct {
	Settings storage.Section[schema.Certs]
	Path     string
	Client   *http.Client
	DeviceID uint32
	Now      func() time.Time
}

type Warden struct {
	o      Options
	knobs  *setting.Controls[schema.Certs]
	status *esphome.TextSensor
	again  chan struct{}

	mu   sync.Mutex
	cert *tls.Certificate
	err  error
}

func New(o Options) *Warden {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Client == nil {
		o.Client = fetch.Client(fetchWait)
	}
	w := &Warden{o: o, again: make(chan struct{}, 1)}
	w.knobs = &setting.Controls[schema.Certs]{
		Table:  NewTable(),
		Device: o.DeviceID,
		Read:   o.Settings.Read,
		Save: func(s Setting, v string) error {
			err := setting.Store(o.Settings, s, v)
			w.kick()
			return err
		},
	}
	w.status = &esphome.TextSensor{Base: esphome.Base{
		ObjectID: "certificate_status", Name: "Certificate", Icon: "mdi:certificate",
		Category: esphome.CategoryDiagnostic, DeviceID: o.DeviceID,
	}}
	w.load()
	return w
}

func (w *Warden) Name() string { return "certwarden" }

func (w *Warden) Controls() *setting.Controls[schema.Certs] { return w.knobs }

func (w *Warden) Entities() []esphome.Entity { return append(w.knobs.Entities(), w.status) }

func (w *Warden) Restore(c schema.Certs) {
	w.knobs.PublishFrom(c)
	w.status.Set(w.Says())
}

func (w *Warden) Run(ctx context.Context) error {
	for {
		wait := refreshEvery
		if w.o.Settings.Read().Configured() {
			if err := w.fetch(ctx); err != nil {
				slog.Warn("fetching the certificate from Cert Warden failed", "err", err)
				wait = retryEvery
			}
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case <-w.again:
		case <-t.C:
		}
		t.Stop()
	}
}

func (w *Warden) Certificate() (*tls.Certificate, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cert == nil || !w.o.Settings.Read().Configured() || w.o.Now().After(w.cert.Leaf.NotAfter) {
		return nil, false
	}
	return w.cert, true
}

func (w *Warden) Says() string {
	cfg := w.o.Settings.Read()
	w.mu.Lock()
	cert, err := w.cert, w.err
	w.mu.Unlock()
	switch {
	case !cfg.Configured():
		return say.T("certs.status.unset")
	case errors.Is(err, ErrRefused):
		return say.T("certs.status.refused")
	case err != nil && cert == nil:
		return say.T("certs.status.unreachable")
	case cert == nil:
		return say.T("certs.status.fetching")
	case w.o.Now().After(cert.Leaf.NotAfter):
		return say.T("certs.status.expired")
	}
	return say.F("certs.status.valid", map[string]any{
		"Name": cert.Leaf.Subject.CommonName,
		"Date": cert.Leaf.NotAfter.Format(time.DateOnly),
	})
}

func (w *Warden) kick() {
	select {
	case w.again <- struct{}{}:
	default:
	}
}

func (w *Warden) fetch(ctx context.Context) error {
	cfg := w.o.Settings.Read()
	url := base(cfg.Server) + chainPath + cfg.Cert
	bundle, err := w.get(ctx, url, cfg.CertAPIKey+"."+cfg.KeyAPIKey)
	if err == nil {
		err = w.install(bundle)
	}
	if err != nil {
		w.fail(err)
	}
	return err
}

func (w *Warden) get(ctx context.Context, url, apiKey string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchWait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", apiKey)
	resp, err := w.o.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, bodyMost))
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, ErrRefused
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("certwarden: %s answered %d", url, resp.StatusCode)
	}
	return body, nil
}

func (w *Warden) install(bundle []byte) error {
	cert, err := tls.X509KeyPair(bundle, bundle)
	if err != nil {
		return err
	}
	if cert.Leaf == nil {
		if cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0]); err != nil {
			return err
		}
	}
	if w.o.Now().After(cert.Leaf.NotAfter) {
		return ErrExpired
	}
	w.mu.Lock()
	same := w.cert != nil && bytes.Equal(w.cert.Certificate[0], cert.Certificate[0])
	w.cert, w.err = &cert, nil
	w.mu.Unlock()
	w.status.Set(w.Says())
	if same {
		return nil
	}
	slog.Info(
		"certificate installed",
		"name", cert.Leaf.Subject.CommonName,
		"until", cert.Leaf.NotAfter.Format(time.DateOnly),
	)
	if w.o.Path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(w.o.Path), 0o700); err != nil {
		return err
	}
	return safe.WriteFile(w.o.Path, bundle, 0o600)
}

func (w *Warden) fail(err error) {
	w.mu.Lock()
	w.err = err
	w.mu.Unlock()
	w.status.Set(w.Says())
}

func (w *Warden) load() {
	if w.o.Path == "" {
		return
	}
	raw, err := os.ReadFile(w.o.Path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("the kept certificate is unreadable", "path", w.o.Path, "err", err)
		}
		return
	}
	cert, err := tls.X509KeyPair(raw, raw)
	if err != nil {
		slog.Warn("the kept certificate is unreadable", "path", w.o.Path, "err", err)
		return
	}
	if cert.Leaf == nil {
		if cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0]); err != nil {
			return
		}
	}
	w.cert = &cert
}

func base(server string) string {
	server = strings.TrimRight(strings.TrimSpace(server), "/")
	if !strings.Contains(server, "://") {
		server = "https://" + server
	}
	return server
}
