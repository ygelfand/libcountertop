package payload

import (
	"slices"
	"testing"
)

func TestNamespaceArgumentsRoundTrip(t *testing.T) {
	binds := []Bind{{Source: "/data/etc", Target: "/system/etc"}, {Source: "/a", Target: "/b"}}
	args := namespaceArgs(
		binds,
		"/engine/chromium.sh",
		[]string{"--remote-debugging-port=9222", "--"},
	)
	if args[0] != marker {
		t.Fatalf("first argument %q, want the marker", args[0])
	}
	gotBinds, argv, err := parseNamespace(args[1:])
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(gotBinds, binds) {
		t.Fatalf("binds %v, want %v", gotBinds, binds)
	}
	want := []string{"/engine/chromium.sh", "--remote-debugging-port=9222", "--"}
	if !slices.Equal(argv, want) {
		t.Fatalf("command %v, want %v", argv, want)
	}
}

func TestNamespaceArgumentsRejectMalformedInput(t *testing.T) {
	for _, args := range [][]string{
		{"/only-source", "--", "/bin/true"},
		{"/a", "/b", "--"},
		{"/a", "/b", "/bin/true"},
	} {
		if _, _, err := parseNamespace(args); err == nil {
			t.Fatalf("parseNamespace(%q) accepted malformed input", args)
		}
	}
}
