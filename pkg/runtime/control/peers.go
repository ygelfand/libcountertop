package control

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ygelfand/libcountertop/pkg/network/peer"
)

func PeersCommand(peers func() []peer.Peer) *cobra.Command {
	return Says(&cobra.Command{
		Use:   "peers",
		Short: "Other devices on the network",
		Args:  cobra.NoArgs,
	}, func([]string) (string, error) { return Peers(peers()), nil })
}

func Peers(peers []peer.Peer) string {
	var out strings.Builder
	for _, p := range peers {
		addrs := make([]string, 0, len(p.Addrs))
		for _, a := range p.Addrs {
			addrs = append(addrs, a.String())
		}
		fmt.Fprintf(&out, "%s\t%s\t%s\t%s\t%s:%d\t%s\t%s\tv%s\t%s ago\n",
			p.ID, p.Name, p.Model, p.Board, strings.Join(addrs, ","), p.Port, p.Standing,
			strings.Join(p.Caps, ","), p.Version, time.Since(p.Seen).Round(time.Second))
	}
	if out.Len() == 0 {
		return "none"
	}
	return out.String()
}
