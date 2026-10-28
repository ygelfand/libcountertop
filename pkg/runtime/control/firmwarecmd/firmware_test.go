package firmwarecmd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/ygelfand/libcountertop/pkg/network/releases"
	harness "github.com/ygelfand/libcountertop/pkg/runtime/control"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/system/firmware"
)

type source struct {
	listErr    error
	fetchErr   error
	installErr error
	fetched    string
	installed  string
	restarted  string
}

func (s *source) Fetch(context.Context, string) (string, error) { return "2.0", nil }
func (s *source) Describe(version string) firmware.Release {
	return firmware.Release{Version: version}
}
func (s *source) Install(ctx context.Context, version string, progress func(float32)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.installed = version
	progress(1)
	return s.installErr
}
func (s *source) Restart(why string) { s.restarted = why }
func (s *source) Builds(ctx context.Context) ([]releases.Release, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []releases.Release{
		{Tag: "2.0-dev.1", Prerelease: true},
		{Tag: "1.0"},
		{Tag: "0.9"},
	}, s.listErr
}
func (s *source) FetchBuild(ctx context.Context, version string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.fetched = version
	return version, s.fetchErr
}

type settings struct{ schema.Update }

func (s *settings) Read() schema.Update        { return s.Update }
func (s *settings) Channel(v string) error     { s.Update.Channel = v; return nil }
func (s *settings) LastVersion(v string) error { s.Update.LastVersion = v; return nil }

func execute(ctx context.Context, src *source, args ...string) (string, error) {
	u := firmware.New(firmware.Dependencies[string]{
		Source: src, Settings: &settings{}, Version: "1.0", Channels: []string{"stable"},
	})
	return harness.Execute(ctx, func() *cobra.Command {
		tree := harness.NewTree()
		tree.Top(Command(func() *firmware.Firmware[string] { return u }))
		return tree.Root()
	}, args)
}

func TestList(t *testing.T) {
	src := &source{}
	out, err := execute(t.Context(), src, "firmware", "list")
	if err != nil {
		t.Fatal(err)
	}
	if want := "2.0-dev.1\tdevelopment\n1.0\tstable\tinstalled\n0.9\tstable\n"; out != want {
		t.Fatalf("list = %q, want %q", out, want)
	}
	if src.fetched != "" || src.installed != "" || src.restarted != "" {
		t.Fatal("listing releases changed the device")
	}
}

func TestInstall(t *testing.T) {
	src := &source{}
	out, err := execute(t.Context(), src, "firmware", "install", "0.9")
	if err != nil {
		t.Fatal(err)
	}
	if src.fetched != "0.9" || src.installed != "0.9" || src.restarted != "update to 0.9" {
		t.Fatalf("install = %+v", src)
	}
	if out != "installed 0.9; restarting\n" {
		t.Fatalf("output = %q", out)
	}
}

func TestInstallCurrentVersion(t *testing.T) {
	src := &source{}
	out, err := execute(t.Context(), src, "firmware", "install", "1.0")
	if err != nil || out != "already installed 1.0\n" {
		t.Fatalf("output = %q, error = %v", out, err)
	}
	if src.fetched != "" || src.installed != "" || src.restarted != "" {
		t.Fatal("reinstalled the running version")
	}
}

func TestFailures(t *testing.T) {
	failure := errors.New("offline")
	for _, tc := range []struct {
		name string
		src  *source
		args []string
	}{
		{"list", &source{listErr: failure}, []string{"firmware", "list"}},
		{"fetch", &source{fetchErr: failure}, []string{"firmware", "install", "0.9"}},
		{"install", &source{installErr: failure}, []string{"firmware", "install", "0.9"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := execute(t.Context(), tc.src, tc.args...)
			if !errors.Is(err, failure) || out != "" || tc.src.restarted != "" {
				t.Fatalf("output = %q, error = %v, restart = %q", out, err, tc.src.restarted)
			}
		})
	}
}

func TestCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, args := range [][]string{{"firmware", "list"}, {"firmware", "install", "0.9"}} {
		if _, err := execute(ctx, &source{}, args...); !errors.Is(err, context.Canceled) {
			t.Fatalf("%v returned %v", args, err)
		}
	}
}

func TestArgumentsAndHelp(t *testing.T) {
	for _, args := range [][]string{
		{"firmware", "list", "extra"},
		{"firmware", "install"},
		{"firmware", "install", "0.9", "extra"},
		{"firmware", "unknown"},
	} {
		src := &source{}
		if _, err := execute(t.Context(), src, args...); err == nil {
			t.Fatalf("%v accepted", args)
		}
		if src.fetched != "" || src.installed != "" {
			t.Fatal("invalid command started an install")
		}
	}
	out, err := execute(t.Context(), &source{}, "firmware", "--help")
	if err != nil || !strings.Contains(out, "install") || !strings.Contains(out, "list") {
		t.Fatalf("help = %q, error = %v", out, err)
	}
}
