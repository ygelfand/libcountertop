package screencmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ygelfand/libcountertop/pkg/display/poster"
)

func Wallpaper(wallpaper func() *poster.Poster) *cobra.Command {
	return &cobra.Command{
		Use:       "wallpaper next|previous",
		Short:     "Show the next or the previous wallpaper",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"next", "previous"},
		RunE: func(_ *cobra.Command, args []string) error {
			switch args[0] {
			case "next":
				wallpaper().Next()
			case "previous":
				wallpaper().Previous()
			default:
				return fmt.Errorf("wallpaper: %q is not next or previous", args[0])
			}
			return nil
		},
	}
}
