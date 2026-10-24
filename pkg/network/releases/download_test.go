package releases

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func serve(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/asset"
}

func sha(body string) string {
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:])
}

func TestDownloadKeepsAMatchingAsset(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "engine.tar.xz")
	if err := Download(
		context.Background(),
		serve(t, "engine"),
		sha("engine"),
		dest,
		1<<20,
	); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(dest); err != nil || string(b) != "engine" {
		t.Fatalf("downloaded %q, %v", b, err)
	}
}

func TestDownloadRefusesAWrongChecksum(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "engine.tar.xz")
	if err := Download(
		context.Background(),
		serve(t, "tampered"),
		sha("engine"),
		dest,
		1<<20,
	); err == nil {
		t.Fatal("kept an asset whose checksum does not match")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("left %d files behind", len(entries))
	}
}

func TestDownloadRefusesAnAssetOverTheLimit(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "engine.tar.xz")
	if err := Download(
		context.Background(),
		serve(t, "too large"),
		sha("too large"),
		dest,
		3,
	); err == nil {
		t.Fatal("kept an asset larger than the limit")
	}
}

func TestAssetURL(t *testing.T) {
	got := Asset(
		"https://github.com/ygelfand/libcountertop/",
		"cast-engine-1",
		"cast-engine-armhf.tar.xz",
	)
	want := "https://github.com/ygelfand/libcountertop/releases/download/cast-engine-1/cast-engine-armhf.tar.xz"
	if got != want {
		t.Fatalf("Asset = %q, want %q", got, want)
	}
}
