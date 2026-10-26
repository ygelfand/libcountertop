package doorbellcmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ygelfand/libcountertop/pkg/camera/doorbell"
	"github.com/ygelfand/libcountertop/pkg/runtime/control/cameracmd"
)

const stillWait = 20 * time.Second

func Command(bell func() *doorbell.Doorbell) *cobra.Command {
	group := &cobra.Command{
		Use:   "doorbell",
		Short: "The doorbells this device watches",
	}
	group.AddCommand(
		&cobra.Command{
			Use:   "state",
			Short: "What the doorbells are doing and which slots are set",
			Args:  cobra.NoArgs,
			RunE: func(c *cobra.Command, _ []string) error {
				c.Print(State(bell()))
				return nil
			},
		},
		&cobra.Command{
			Use:   "ring SLOT [" + strings.Join(kindNames(), "|") + "]",
			Short: "Act as if a doorbell saw something: ring by default, with a fresh picture",
			Args:  cobra.RangeArgs(1, 2),
			RunE: func(_ *cobra.Command, args []string) error {
				slot, err := slotOf(args[0])
				if err != nil {
					return err
				}
				kind := doorbell.Visitor
				if len(args) > 1 {
					kind = doorbell.Kind(args[1])
				}
				return bell().Rehearse(slot, kind)
			},
		},
		&cobra.Command{
			Use:   "still SLOT [PATH]",
			Short: "Take a picture from a doorbell",
			Args:  cobra.RangeArgs(1, 2),
			RunE: func(c *cobra.Command, args []string) error {
				slot, err := slotOf(args[0])
				if err != nil {
					return err
				}
				out, err := cameracmd.Still(func() ([]byte, error) {
					ctx, cancel := context.WithTimeout(context.Background(), stillWait)
					defer cancel()
					return bell().Snapshot(ctx, slot)
				}, args[1:])
				c.Print(out)
				return err
			},
		},
		&cobra.Command{
			Use:   "recent",
			Short: "What the doorbells saw lately, newest first",
			Args:  cobra.NoArgs,
			RunE: func(c *cobra.Command, _ []string) error {
				c.Print(Recent(bell().Recent()))
				return nil
			},
		},
	)
	return group
}

func State(b *doorbell.Doorbell) string {
	var s strings.Builder
	fmt.Fprintf(&s, "  %-9s %s\n", "state", b.State().Says())
	for _, d := range b.Doors() {
		fmt.Fprintf(&s, "  %-9s %d %s\n", "door", d.Slot+1, d.Name)
	}
	return s.String()
}

func Recent(events []doorbell.Event) string {
	if len(events) == 0 {
		return "  nothing yet\n"
	}
	var s strings.Builder
	for _, e := range events {
		fmt.Fprintf(&s, "  %s  door %d  %-8s picture %d bytes\n",
			e.At.Format(time.TimeOnly), e.Slot+1, e.Kind, len(e.Snapshot))
	}
	return s.String()
}

func slotOf(arg string) (int, error) {
	n, err := strconv.Atoi(arg)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("doorbell: SLOT is a number from 1")
	}
	return n - 1, nil
}

func kindNames() []string {
	var out []string
	for _, k := range doorbell.Kinds() {
		out = append(out, string(k))
	}
	return out
}
