package screencmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ygelfand/libcountertop/pkg/display/apps"
)

func App(list func() []apps.App) *cobra.Command {
	return &cobra.Command{
		Use:   "app [ID]",
		Short: "List the apps, or open one",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			all := list()
			if len(args) == 0 {
				for _, a := range all {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", a.ID, a.Label())
				}
				return nil
			}
			for _, a := range all {
				if a.ID == args[0] {
					a.Open()
					return nil
				}
			}
			return fmt.Errorf("app: %q is not an app", args[0])
		},
	}
}
