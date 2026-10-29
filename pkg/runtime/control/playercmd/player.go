package playercmd

import (
	"github.com/spf13/cobra"

	"github.com/ygelfand/libcountertop/pkg/media/nowplaying"
	"github.com/ygelfand/libcountertop/pkg/runtime/control"
)

type Player interface {
	Sourced() (string, bool)
	Now() nowplaying.Now
}

func Command(player func() Player) *cobra.Command {
	return control.Says(&cobra.Command{
		Use:   "player",
		Short: "What is playing, and on what",
		Args:  cobra.NoArgs,
	}, func(args []string) (string, error) {
		p := player()
		name, external := p.Sourced()
		return nowplaying.Report(p.Now(), name, external, args)
	})
}
