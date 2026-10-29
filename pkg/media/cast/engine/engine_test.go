package engine

import (
	"archive/tar"
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ulikunitz/xz"
)

type entry struct {
	name string
	body string
	mode int64
	kind byte
	link string
}

func archive(t *testing.T, entries []entry) string {
	t.Helper()
	var buf bytes.Buffer
	zw, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		h := &tar.Header{
			Name:     e.name,
			Mode:     e.mode,
			Typeflag: e.kind,
			Linkname: e.link,
			Size:     int64(len(e.body)),
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "engine.tar.xz")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUnpackDropsTheTopDirectoryAndKeepsModes(t *testing.T) {
	src := archive(t, []entry{
		{name: "cast-engine/", mode: 0o755, kind: tar.TypeDir},
		{
			name: "cast-engine/chromium.sh",
			body: "#!/system/bin/sh\n",
			mode: 0o755,
			kind: tar.TypeReg,
		},
		{name: "cast-engine/lib/libz.so.1", body: "elf", mode: 0o644, kind: tar.TypeReg},
		{name: "cast-engine/lib/libz.so", mode: 0o777, kind: tar.TypeSymlink, link: "libz.so.1"},
	})
	dest := filepath.Join(t.TempDir(), "out")
	if err := unpack(src, dest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dest, "chromium.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("chromium.sh mode %v, want executable", info.Mode())
	}
	if b, err := os.ReadFile(
		filepath.Join(dest, "lib", "libz.so"),
	); err != nil ||
		string(b) != "elf" {
		t.Fatalf("symlinked library read %q, %v", b, err)
	}
}

func TestUnpackRefusesEntriesOutsideTheInstall(t *testing.T) {
	for _, e := range []entry{
		{name: "cast-engine/../../evil", body: "x", mode: 0o644, kind: tar.TypeReg},
		{name: "cast-engine/lib/escape", mode: 0o777, kind: tar.TypeSymlink, link: "../../../etc/passwd"},
	} {
		if err := unpack(archive(t, []entry{e}), filepath.Join(t.TempDir(), "out")); err == nil {
			t.Fatalf("unpacked %q", e.name)
		}
	}
}

func TestInstallKeepsAnExistingVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, Tag), 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := Install(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if root != filepath.Join(dir, Tag) {
		t.Fatalf("Install returned %q, want the existing %q", root, filepath.Join(dir, Tag))
	}
}

func TestPruneRemovesOnlyOtherEngineVersions(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{Tag, "engine-full-0", Tag + ".partial", "profile"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	prune(dir, Tag)
	for name, want := range map[string]bool{Tag: true, "engine-full-0": false, Tag + ".partial": false, "profile": true} {
		_, err := os.Stat(filepath.Join(dir, name))
		if (err == nil) != want {
			t.Fatalf("%s present=%v, want %v", name, err == nil, want)
		}
	}
}

func TestTidyKeepsTheSignInsAndDropsTheRest(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "profile")
	dirs := []string{
		"component_crx_cache/abc", "WasmTtsEngine/1", "OnDeviceHeadSuggestModel",
		"Default/Cache", "Default/Code Cache/js", "Default/GPUCache", "Default/Local Storage",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(profile, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"Local State", "BrowserMetrics-spare.pma", "Default/Cookies"} {
		if err := os.WriteFile(filepath.Join(profile, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tidy(profile)
	want := map[string]bool{
		"Local State": true, "Default/Cookies": true, "Default/Local Storage": true,
		"component_crx_cache": false, "WasmTtsEngine": false, "OnDeviceHeadSuggestModel": false,
		"BrowserMetrics-spare.pma": false, "Default/Cache": false, "Default/Code Cache": false,
		"Default/GPUCache": false,
	}
	for name, keep := range want {
		_, err := os.Stat(filepath.Join(profile, name))
		if (err == nil) != keep {
			t.Errorf("%s present=%v, want %v", name, err == nil, keep)
		}
	}
	tidy(filepath.Join(t.TempDir(), "missing"))
}

func TestWriteEtcListsEveryNameserver(t *testing.T) {
	dir, err := writeEtc(filepath.Join(t.TempDir(), "etc"), []string{"10.0.0.1", "1.1.1.1"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "resolv.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "nameserver 10.0.0.1\nnameserver 1.1.1.1\n"; string(b) != want {
		t.Fatalf("resolv.conf %q, want %q", b, want)
	}
	if _, err := writeEtc(filepath.Join(t.TempDir(), "etc"), nil); err == nil {
		t.Fatal("wrote a resolv.conf with no nameservers")
	}
}

func TestReadyWaitsForTheDebuggingPort(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Ready(ctx, port) }()
	time.Sleep(300 * time.Millisecond)
	srv := httptest.NewUnstartedServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/json/version" {
				http.NotFound(w, r)
			}
		}),
	)
	srv.Listener, err = net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	defer srv.Close()
	if err := <-done; err != nil {
		t.Fatalf("Ready returned %v once the port answered", err)
	}
}
