package firmwarecmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	harness "github.com/ygelfand/libcountertop/pkg/runtime/control"
	"github.com/ygelfand/libcountertop/pkg/system/firmware"
)

func Command[M any](get func() *firmware.Firmware[M]) *cobra.Command {
	cmd := harness.Group("firmware", "List and install firmware releases", "")
	cmd.AddCommand(
		harness.ContextSays(&cobra.Command{
			Use: "list", Short: "List releases for this device", Args: cobra.NoArgs,
		}, func(ctx context.Context, _ []string) (string, error) {
			u := get()
			if err := u.List(ctx); err != nil {
				return "", err
			}
			var out strings.Builder
			for _, build := range u.Catalog().Builds {
				channel := "stable"
				if build.Prerelease {
					channel = "development"
				}
				fmt.Fprintf(&out, "%s\t%s", build.Tag, channel)
				if build.Tag == u.Version() {
					out.WriteString("\tinstalled")
				}
				out.WriteByte('\n')
			}
			return out.String(), nil
		}),
		harness.ContextSays(&cobra.Command{
			Use:   "install VERSION",
			Short: "Install a release and restart, including older versions",
			Args:  cobra.ExactArgs(1),
		}, func(ctx context.Context, args []string) (string, error) {
			u := get()
			if args[0] == u.Version() {
				return "already installed " + args[0], nil
			}
			if err := u.InstallBuild(ctx, args[0]); err != nil {
				return "", err
			}
			return "installed " + args[0] + "; restarting", nil
		}),
	)
	return cmd
}
