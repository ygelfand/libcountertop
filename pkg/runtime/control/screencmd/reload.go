package screencmd

import (
	"github.com/spf13/cobra"

	"github.com/ygelfand/libcountertop/pkg/homeassistant/lovelaceview"
)

func Reload(dashboards func() *lovelaceview.Feature) *cobra.Command {
	return &cobra.Command{
		Use:   "reload",
		Short: "Reload the Home Assistant dashboard, clearing what the browser cached for it",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := dashboards().Reload(c.Context()); err != nil {
				return err
			}
			c.Println("reloaded")
			return nil
		},
	}
}
