package castcmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ygelfand/libcountertop/pkg/runtime/control"
)

type Page interface {
	Describe() string
	Eval(ctx context.Context, expr string) (string, error)
	Trace(on bool)
	Tracing() bool
}

var errNoReceiver = errors.New("cast: no web receiver is running")

func Command(page func() Page) *cobra.Command {
	cmd := control.Group("cast", "Inspect the cast receiver page", "")
	show := control.Says(&cobra.Command{
		Use:   "page",
		Short: "The app, page, media session and playback the receiver page reports",
		Args:  cobra.NoArgs,
	}, func([]string) (string, error) {
		p := page()
		if p == nil {
			return "", errNoReceiver
		}
		return p.Describe(), nil
	})
	show.AddCommand(control.ContextSays(&cobra.Command{
		Use:   "eval EXPRESSION",
		Short: "Run a JavaScript expression in the receiver page and print its value",
		Args:  cobra.MinimumNArgs(1),
	}, func(ctx context.Context, args []string) (string, error) {
		p := page()
		if p == nil {
			return "", errNoReceiver
		}
		return p.Eval(ctx, strings.Join(args, " "))
	}))
	cmd.AddCommand(
		show,
		control.Says(&cobra.Command{
			Use:       "trace [on|off]",
			Short:     "Log every message between the page, its senders and the lounge at info level",
			Args:      cobra.MaximumNArgs(1),
			ValidArgs: []string{"on", "off"},
		}, func(args []string) (string, error) {
			p := page()
			if p == nil {
				return "", errNoReceiver
			}
			return Trace(p, args)
		}),
	)
	return cmd
}

func Trace(p Page, args []string) (string, error) {
	if len(args) == 1 {
		switch args[0] {
		case "on":
			p.Trace(true)
		case "off":
			p.Trace(false)
		default:
			return "", fmt.Errorf("want on or off, got %q", args[0])
		}
	}
	if p.Tracing() {
		return "on", nil
	}
	return "off", nil
}
