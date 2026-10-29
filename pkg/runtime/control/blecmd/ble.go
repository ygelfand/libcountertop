package blecmd

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/ygelfand/libcountertop/pkg/runtime/control"
)

const window = 2 * time.Second

type Radio interface {
	Sniff(ctx context.Context, saw func(string)) error
}

func Command(radio Radio) *cobra.Command {
	ble := control.Group("ble", "The low energy radio", "")
	ble.AddCommand(&cobra.Command{
		Use:   "sniff [MILLISECONDS]",
		Short: "Print every packet the controller sends up, as it arrives",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			within := window
			if len(args) == 1 {
				ms, err := strconv.Atoi(args[0])
				if err != nil || ms <= 0 {
					return fmt.Errorf("ble: %q is not a number of milliseconds", args[0])
				}
				within = time.Duration(ms) * time.Millisecond
			}
			return Sniff(cmd.Context(), radio, within, cmd.OutOrStdout())
		},
	})
	return ble
}

func Sniff(ctx context.Context, radio Radio, within time.Duration, out io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	start := time.Now()
	return radio.Sniff(ctx, func(line string) {
		fmt.Fprintf(out, "%7.3f  %s\n", time.Since(start).Seconds(), line)
	})
}
