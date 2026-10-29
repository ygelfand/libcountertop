package blecmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

type radio struct{ lines []string }

func (r radio) Sniff(ctx context.Context, saw func(string)) error {
	for _, l := range r.lines {
		saw(l)
	}
	<-ctx.Done()
	return nil
}

func TestSniffStampsEachLineAndStopsAtTheWindow(t *testing.T) {
	var out bytes.Buffer
	start := time.Now()
	err := Sniff(
		context.Background(),
		radio{lines: []string{"event 0x3e  02", "data handle 0x0002  01"}},
		50*time.Millisecond,
		&out,
	)
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took < 50*time.Millisecond || took > time.Second {
		t.Errorf("sniff ran %v, want about the 50 ms window", took)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "  event 0x3e  02") ||
		!strings.HasSuffix(lines[1], "  data handle 0x0002  01") {
		t.Fatalf("output %q", out.String())
	}
}

func TestSniffRefusesANonsenseWindow(t *testing.T) {
	cmd := Command(radio{})
	cmd.SetArgs([]string{"sniff", "soon"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("a window of \"soon\" was accepted")
	}
}
