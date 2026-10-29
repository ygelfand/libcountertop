package control

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/network/peer"
)

func TestNoPeersSaysNone(t *testing.T) {
	if got := Peers(nil); got != "none" {
		t.Errorf("no peers reads %q", got)
	}
}

func TestAPeerIsOneLine(t *testing.T) {
	got := Peers([]peer.Peer{{
		ID:      "abc",
		Name:    "Kitchen",
		Addrs:   []net.IP{net.IPv4(10, 0, 0, 2)},
		Port:    6053,
		Caps:    []string{"video"},
		Version: "1.2",
		Seen:    time.Now(),
	}})
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("%q is not one line", got)
	}
	for _, want := range []string{"abc", "Kitchen", "10.0.0.2:6053", "video", "v1.2"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q does not mention %s", got, want)
		}
	}
}
