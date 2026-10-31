package nativeapi

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	esphome "github.com/ygelfand/go-esphome-device"
)

func ReadKey(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// ESPHome reserves the all-zero key for an unprovisioned device.
func loadPSK(path string) (*esphome.PSK, error) {
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		k, err := esphome.ParsePSK(strings.TrimSpace(string(b)))
		if err != nil {
			return nil, fmt.Errorf("api: key at %s: %w", path, err)
		}
		return &k, nil

	case !errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("api: key at %s: %w", path, err)
	}

	slog.Info("unprovisioned: the api takes any client until Home Assistant sets a key")
	return esphome.Unprovisioned(), nil
}

func writePSK(path string, k esphome.PSK) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(k.String()+"\n"), 0o600)
}

func loadTarget(path string) (netip.Addr, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(string(b)))
	if err != nil {
		slog.Warn("api: ignoring the saved dial target", "path", path, "err", err)
		return netip.Addr{}, false
	}
	return addr, true
}

func writeTarget(path string, addr netip.Addr) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(addr.String()+"\n"), 0o600)
}
