package widevine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/surface"
	"github.com/ygelfand/libcountertop/pkg/fetch"
	"github.com/ygelfand/libcountertop/pkg/runtime/startup"
)

const (
	checkSession = 1 << 31
	provisionFor = 30 * time.Second
	drmWait      = 30 * time.Second
	drmRetry     = 2 * time.Second
)

var probe = []byte{
	0x00, 0x00, 0x00, 0x3e, 0x70, 0x73, 0x73, 0x68, 0x00, 0x00, 0x00, 0x00, 0xed, 0xef, 0x8b, 0xa9,
	0x79, 0xd6, 0x4a, 0xce, 0xa3, 0xc8, 0x27, 0xdc, 0xd5, 0x1d, 0x21, 0xed, 0x00, 0x00, 0x00, 0x1e,
	0x22, 0x16, 0x73, 0x68, 0x61, 0x6b, 0x61, 0x5f, 0x63, 0x65, 0x63, 0x32, 0x66, 0x36, 0x34, 0x61,
	0x61, 0x37, 0x38, 0x39, 0x30, 0x61, 0x31, 0x31, 0x48, 0xe3, 0xdc, 0x95, 0x9b, 0x06,
}

type Network interface {
	Wait(ctx context.Context) bool
}

type Level string

const (
	L1 Level = "L1"
	L3 Level = "L3"
)

type Provisioner struct {
	helper  func() *surface.Client
	network Network
	floor   Level
	boot    atomic.Pointer[startup.Progress]
}

func New(helper func() *surface.Client, network Network, floor Level) *Provisioner {
	return &Provisioner{helper: helper, network: network, floor: floor}
}

func (d *Provisioner) Name() string { return "drm" }

func (d *Provisioner) Startup() startup.Progress {
	if p := d.boot.Load(); p != nil {
		return *p
	}
	return startup.Progress{Doing: "checking"}
}

func (d *Provisioner) set(p startup.Progress) { d.boot.Store(&p) }

func (d *Provisioner) Run(ctx context.Context) error {
	d.set(d.bring(ctx))
	<-ctx.Done()
	return nil
}

func (d *Provisioner) bring(ctx context.Context) startup.Progress {
	c := d.helper()
	if c == nil {
		return startup.Progress{Failed: true, Doing: "no display helper"}
	}
	began := time.Now()
	level, ready, err := provisioned(c)
	for err != nil && time.Since(began) < drmWait {
		d.set(startup.Progress{Doing: "waiting for DRM"})
		select {
		case <-ctx.Done():
			return startup.Progress{Failed: true, Doing: err.Error()}
		case <-time.After(drmRetry):
		}
		level, ready, err = provisioned(c)
	}
	slog.Info(
		"widevine check",
		"level",
		level,
		"provisioned",
		ready,
		"took",
		time.Since(began),
		"err",
		err,
	)
	if err != nil {
		return startup.Progress{Failed: true, Doing: err.Error()}
	}
	if Level(level) != L1 && Level(level) != d.floor {
		return startup.Progress{
			Failed: true,
			Doing:  "Widevine " + level + ", no hardware decryption",
		}
	}
	if ready {
		return startup.Progress{Done: true, Doing: "provisioned"}
	}

	d.set(startup.Progress{Doing: "waiting for the network"})
	if !d.network.Wait(ctx) {
		return startup.Progress{Failed: true, Doing: "no network"}
	}
	d.set(startup.Progress{Doing: "provisioning"})
	if err := provision(ctx, c); err != nil {
		slog.Warn("widevine provisioning failed", "err", err)
		return startup.Progress{Failed: true, Doing: err.Error()}
	}
	slog.Info("widevine provisioned")
	return startup.Progress{Done: true, Doing: "provisioned"}
}

func provisioned(c *surface.Client) (string, bool, error) {
	if err := c.DRMOpen(checkSession, surface.Widevine, false, nil); err != nil {
		return "", false, fmt.Errorf("widevine: %w", err)
	}
	defer c.DRMClose(checkSession)
	level, err := c.DRMProperty(checkSession, "securityLevel")
	if err != nil {
		return "", false, fmt.Errorf("widevine security level: %w", err)
	}
	_, err = c.DRMRequest(checkSession, probe)
	return level, err == nil, nil
}

func provision(ctx context.Context, c *surface.Client) error {
	req, url, err := c.DRMProvision(surface.Widevine)
	if err != nil {
		return fmt.Errorf("provision request: %w", err)
	}
	if url == "" {
		return errors.New("provision request: no server")
	}
	ctx, cancel := context.WithTimeout(ctx, provisionFor)
	defer cancel()
	hr, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		url+"&signedRequest="+string(req),
		http.NoBody,
	)
	if err != nil {
		return err
	}
	resp, err := fetch.Client(0).Do(hr)
	if err != nil {
		return fmt.Errorf("provisioning server: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("provisioning server: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("provisioning server: %s", resp.Status)
	}
	return c.DRMProvisioned(body)
}
