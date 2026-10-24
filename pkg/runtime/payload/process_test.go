//go:build unix

package payload

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func childPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(b)) > 0 {
			pid, err := strconv.Atoi(string(bytes.TrimSpace(b)))
			if err != nil {
				t.Fatal(err)
			}
			return pid
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("child pid never written")
	return 0
}

func gone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("child %d still running", pid)
}

func TestWaitEndsWhatTheHelperLeftBehind(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	p, err := Start(Command{
		Path: "/bin/sh",
		Args: []string{"-c", "sleep 30 & echo $! > " + pidFile},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	gone(t, childPID(t, pidFile))
}

func TestStopEndsTheWholeGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	p, err := Start(Command{
		Path: "/bin/sh",
		Args: []string{"-c", "sleep 30 & echo $! > " + pidFile + "; wait"},
	})
	if err != nil {
		t.Fatal(err)
	}
	child := childPID(t, pidFile)
	p.Stop()
	done := make(chan error, 1)
	go func() { done <- p.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a stopped helper reported a clean exit")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after Stop")
	}
	gone(t, child)
}

func TestLogTagsEachLineWithoutColorCodes(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(
		slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	)
	defer slog.SetDefault(old)
	logLines("helper", io.NopCloser(strings.NewReader("\x1b[32mready\x1b[0m\nsecond\n")))
	out := buf.String()
	for _, want := range []string{"msg=helper line=ready", "msg=helper line=second"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log %q missing %q", out, want)
		}
	}
}

func TestStartReportsAMissingBinary(t *testing.T) {
	if _, err := Start(Command{Path: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("started a binary that does not exist")
	}
}
