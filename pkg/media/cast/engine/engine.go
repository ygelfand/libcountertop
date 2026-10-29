package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ygelfand/libcountertop/pkg/network/releases"
	"github.com/ygelfand/libcountertop/pkg/runtime/payload"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/runtime/service"
)

const (
	repo       = "https://github.com/ygelfand/libcountertop"
	Tag        = "engine-full-1"
	asset      = "engine-full-armhf.tar.xz"
	sum        = "ee70db08ab23ffa6b0b05e33dfdac769f3f729e8d213c2c3ee3e3461257d455a"
	maxArchive = 256 << 20
	firstToGo  = 1000
	diskCache  = 16 << 20
	prefix     = "engine-full-"
)

type Options struct {
	Root        string
	State       string
	Port        int
	Display     string
	Args        []string
	Nameservers func() []string
}

func Install(ctx context.Context, dir string) (string, error) {
	root := filepath.Join(dir, Tag)
	if _, err := os.Stat(root); err == nil {
		return root, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("engine: %w", err)
	}
	archive := filepath.Join(dir, asset)
	defer os.Remove(archive)
	if err := releases.Download(
		ctx,
		releases.Asset(repo, Tag, asset),
		sum,
		archive,
		maxArchive,
	); err != nil {
		return "", err
	}
	partial := root + ".partial"
	if err := os.RemoveAll(partial); err != nil {
		return "", fmt.Errorf("engine: %w", err)
	}
	if err := unpack(archive, partial); err != nil {
		return "", errors.Join(err, os.RemoveAll(partial))
	}
	if err := os.Rename(partial, root); err != nil {
		return "", fmt.Errorf("engine: %w", err)
	}
	prune(dir, Tag)
	return root, nil
}

func Run(ctx context.Context, o Options) error {
	return service.Keep(
		ctx,
		"cast engine",
		func(ctx context.Context) error { return session(ctx, o) },
	)
}

func Ready(ctx context.Context, port int) error {
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/json/version"
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		if up(ctx, url) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func session(ctx context.Context, o Options) error {
	etc, err := writeEtc(filepath.Join(o.State, "etc"), o.Nameservers())
	if err != nil {
		return err
	}
	target, err := filepath.EvalSymlinks("/etc")
	if err != nil {
		return fmt.Errorf("engine: %w", err)
	}
	tidy(filepath.Join(o.State, "profile"))
	args := append([]string{
		"--remote-debugging-address=127.0.0.1",
		"--remote-debugging-port=" + strconv.Itoa(o.Port),
		"--ozone-platform=wayland",
		"--kiosk",
		"--no-first-run",
		"--disable-component-update",
		"--disk-cache-size=" + strconv.Itoa(diskCache),
	}, o.Args...)
	p, err := payload.Start(payload.Command{
		Path: filepath.Join(o.Root, "chromium.sh"),
		Args: append(args, "about:blank"),
		Env: []string{
			"CAST_ENGINE_STATE=" + o.State,
			"WAYLAND_DISPLAY=" + o.Display,
			"XDG_RUNTIME_DIR=" + filepath.Dir(o.Display),
		},
		Log:      "cast engine",
		Binds:    []payload.Bind{{Source: etc, Target: target}},
		OOMScore: firstToGo,
	})
	if err != nil {
		return fmt.Errorf("engine: %w", err)
	}
	stop := context.AfterFunc(ctx, p.Stop)
	defer stop()
	if err := p.Wait(); err != nil {
		return fmt.Errorf("engine: %w", err)
	}
	return errors.New("engine: chromium exited")
}

func writeEtc(dir string, nameservers []string) (string, error) {
	if len(nameservers) == 0 {
		return "", errors.New("engine: the device reports no nameservers")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("engine: %w", err)
	}
	var resolv strings.Builder
	for _, ns := range nameservers {
		fmt.Fprintf(&resolv, "nameserver %s\n", ns)
	}
	files := map[string]string{
		"resolv.conf":   resolv.String(),
		"hosts":         "127.0.0.1 localhost\n::1 localhost\n",
		"nsswitch.conf": "hosts: files dns\npasswd: files\ngroup: files\n",
	}
	for name, body := range files {
		if err := safe.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return "", fmt.Errorf("engine: %w", err)
		}
	}
	return dir, nil
}

func up(ctx context.Context, url string) bool {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

var (
	profileKeeps = []string{"Default", "Local State"}
	pageCaches   = []string{
		"Cache",
		"Code Cache",
		"GPUCache",
		"DawnWebGPUCache",
		"DawnGraphiteCache",
	}
)

func tidy(profile string) {
	entries, err := os.ReadDir(profile)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !slices.Contains(profileKeeps, e.Name()) {
			_ = os.RemoveAll(filepath.Join(profile, e.Name()))
		}
	}
	for _, name := range pageCaches {
		_ = os.RemoveAll(filepath.Join(profile, "Default", name))
	}
}

func prune(dir, keep string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name() != keep && strings.HasPrefix(e.Name(), prefix) {
			_ = os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}
