package castcmd

import (
	"context"
	"strings"
	"testing"

	"github.com/ygelfand/libcountertop/pkg/runtime/control"
)

type fakePage struct {
	tracing bool
	asked   string
}

func (f *fakePage) Describe() string { return "app Plex" }
func (f *fakePage) Eval(_ context.Context, expr string) (string, error) {
	f.asked = expr
	return "42", nil
}
func (f *fakePage) Trace(on bool) { f.tracing = on }
func (f *fakePage) Tracing() bool { return f.tracing }

func run(t *testing.T, page func() Page, args ...string) (string, error) {
	t.Helper()
	tree := control.NewTree()
	tree.Top(Command(page))
	return control.Execute(context.Background(), tree.Root, args)
}

func TestTheCastGroupReadsAndDrivesThePage(t *testing.T) {
	p := &fakePage{}
	page := func() Page { return p }

	if out, err := run(t, page, "cast", "page"); err != nil || !strings.Contains(out, "app Plex") {
		t.Fatalf("cast page said %q, %v", out, err)
	}
	if out, err := run(t, page, "cast", "page", "eval", "6", "*", "7"); err != nil ||
		strings.TrimSpace(out) != "42" || p.asked != "6 * 7" {
		t.Fatalf("cast page eval said %q, %v, and asked the page %q", out, err, p.asked)
	}
	if out, err := run(t, page, "cast", "trace", "on"); err != nil || !p.tracing ||
		strings.TrimSpace(out) != "on" {
		t.Fatalf("cast trace on said %q, %v; tracing %t", out, err, p.tracing)
	}
	if _, err := run(t, page, "cast", "trace", "sideways"); err == nil {
		t.Error("cast trace took a word that is neither on nor off")
	}
}

func TestTheCastGroupSaysWhenNoReceiverRuns(t *testing.T) {
	if _, err := run(t, func() Page { return nil }, "cast", "page"); err == nil {
		t.Error("cast page answered with no receiver running")
	}
}
