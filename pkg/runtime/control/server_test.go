package control

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func socket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ctl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "s")
}

func TestCloseRevokesExistingClientsAndCanRestart(t *testing.T) {
	var calls atomic.Int32
	s := NewServer(socket(t), func(_ context.Context, _ []string, out io.Writer) error {
		calls.Add(1)
		_, err := io.WriteString(out, "answered")
		return err
	})
	t.Cleanup(s.Close)
	for range 2 {
		if err := s.Start(); err != nil {
			t.Fatal(err)
		}
		conn, err := net.Dial("unix", s.address)
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		in := bufio.NewScanner(conn)
		if _, err := fmt.Fprintln(conn, "ping"); err != nil {
			t.Fatal(err)
		}
		if err := Answer(in, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		s.Close()
		s.Wait()
		_, _ = fmt.Fprintln(conn, "ping")
		if in.Scan() {
			t.Errorf("revoked connection remained usable: %q", in.Text())
		}
		_ = conn.Close()
	}
	if calls.Load() != 2 {
		t.Errorf("revoked clients dispatched extra requests: %d calls", calls.Load())
	}
}

func TestCloseCancelsCommandContext(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	s := NewServer(socket(t), func(ctx context.Context, _ []string, _ io.Writer) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	conn, err := net.Dial("unix", s.address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = fmt.Fprintln(conn, "wait")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("command did not start")
	}
	s.Close()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("closing the harness did not cancel the command")
	}
	s.Wait()
}

func TestSendStopsAfterFailureAndKeepsPartialOutput(t *testing.T) {
	var calls atomic.Int32
	s := NewServer(socket(t), func(_ context.Context, _ []string, out io.Writer) error {
		calls.Add(1)
		_, _ = io.WriteString(out, "step completed")
		return errors.New("later step failed")
	})
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close(); s.Wait() }()
	var out bytes.Buffer
	err := Send(t.Context(), s.address, []string{"first", "second"}, &out)
	if err == nil || !strings.Contains(err.Error(), "later step failed") {
		t.Fatalf("Send error = %v", err)
	}
	if out.String() != "step completed\n" || calls.Load() != 1 {
		t.Errorf(
			"partial output or sequencing lost: output=%q calls=%d",
			out.String(),
			calls.Load(),
		)
	}
}

func TestALineReachesTheClientBeforeTheCommandEnds(t *testing.T) {
	release := make(chan struct{})
	ours, theirs := net.Pipe()
	t.Cleanup(func() { _ = ours.Close() })
	go Serve(
		context.Background(),
		theirs,
		func(_ context.Context, _ []string, out io.Writer) error {
			_, _ = io.WriteString(out, "first\nhal")
			<-release
			_, _ = io.WriteString(out, "f")
			return nil
		},
	)
	_ = ours.SetDeadline(time.Now().Add(2 * time.Second))
	go func() { _, _ = fmt.Fprintln(ours, "go") }()
	in := bufio.NewScanner(ours)
	if !in.Scan() || in.Text() != markOutput+"first" {
		t.Fatalf("first line %q, want it before the command ends", in.Text())
	}
	close(release)
	var out bytes.Buffer
	if err := Answer(in, &out); err != nil || out.String() != "half\n" {
		t.Fatalf("rest %q, %v; want the partial line finished", out.String(), err)
	}
}

func TestOutputThatLooksLikeATerminatorStaysOutput(t *testing.T) {
	answers := map[string]struct {
		out string
		err error
	}{
		"first":  {out: "ok\nerror: not really\n"},
		"second": {out: "after", err: errors.New("one\ntwo")},
		"third":  {out: "last"},
	}
	ours, theirs := net.Pipe()
	t.Cleanup(func() { _ = ours.Close() })
	go Serve(
		context.Background(),
		theirs,
		func(_ context.Context, args []string, out io.Writer) error {
			a := answers[args[0]]
			_, _ = io.WriteString(out, a.out)
			return a.err
		},
	)
	_ = ours.SetDeadline(time.Now().Add(2 * time.Second))
	in := bufio.NewScanner(ours)
	for _, tc := range []struct{ cmd, out, err string }{
		{"first", "ok\nerror: not really\n", ""},
		{"second", "after\n", "one\ntwo"},
		{"third", "last\n", ""},
	} {
		go func() { _, _ = fmt.Fprintln(ours, tc.cmd) }()
		var out bytes.Buffer
		err := Answer(in, &out)
		if out.String() != tc.out {
			t.Errorf("%s: output %q, want %q", tc.cmd, out.String(), tc.out)
		}
		if got := fmt.Sprint(err); (tc.err == "" && err != nil) || (tc.err != "" && got != tc.err) {
			t.Errorf("%s: error %v, want %q", tc.cmd, err, tc.err)
		}
	}
}
