package homealarmcmd

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ygelfand/libcountertop/pkg/display/homealarmview"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/homealarm"
)

var errNoView = errors.New("the home alarm screen is not running")

func Command(alarm func() *homealarm.Alarm, view func() *homealarmview.View) *cobra.Command {
	group := &cobra.Command{
		Use:   "homealarm",
		Short: "The Home Assistant alarm panel this device shows",
	}
	states := make([]string, 0, len(homealarmview.Rehearsable()))
	for _, s := range homealarmview.Rehearsable() {
		states = append(states, string(s))
	}
	group.AddCommand(
		&cobra.Command{
			Use:   "state",
			Short: "The panel, its state and what was found on its device",
			Args:  cobra.NoArgs,
			RunE: func(c *cobra.Command, _ []string) error {
				c.Print(State(alarm()))
				if v := view(); v != nil {
					if s, open, ok := v.Rehearsing(); ok {
						c.Printf("rehearsing %s, open %v\n", s, open)
					}
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "rehearse " + strings.Join(states, "|") + " [ZONE…] [sound]",
			Short: "Show a state and open zones without touching the real alarm; buttons send nothing",
			Args:  cobra.MinimumNArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				v := view()
				if v == nil {
					return errNoView
				}
				var open []int
				sound := false
				for _, a := range args[1:] {
					if a == "sound" {
						sound = true
						continue
					}
					n, err := strconv.Atoi(a)
					if err != nil || n <= 0 {
						return fmt.Errorf("zones are numbers, not %q", a)
					}
					open = append(open, n)
				}
				return v.Rehearse(homealarm.Status(args[0]), open, sound)
			},
		},
		&cobra.Command{
			Use:   "open",
			Short: "Open the alarm panel app",
			Args:  cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				v := view()
				if v == nil {
					return errNoView
				}
				v.Open()
				return nil
			},
		},
		&cobra.Command{
			Use:   "stop",
			Short: "End a rehearsal and follow the real alarm again",
			Args:  cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				v := view()
				if v == nil {
					return errNoView
				}
				v.StopRehearsal()
				return nil
			},
		},
	)
	return group
}

func State(a *homealarm.Alarm) string {
	var b strings.Builder
	s := a.State()
	fmt.Fprintf(&b, "panel %s (%s): %s\n", s.Entity, s.Name, orNone(string(s.Status)))
	fmt.Fprintf(
		&b,
		"modes %v code %q to arm %v trigger %v\n",
		s.Modes(),
		s.Code,
		s.CodeToArm,
		s.CanTrigger(),
	)
	d := a.Device()
	for i, z := range d.Zones {
		n := z.Number
		if n == 0 {
			n = i + 1
		}
		fmt.Fprintf(
			&b,
			"zone %02d %s [%s] open=%v known=%v bypassed=%v battery=%v tamper=%v fault=%v\n",
			n,
			z.Name,
			z.Class,
			z.Open,
			z.Known,
			z.Bypassed,
			z.LowBattery,
			z.Tamper,
			z.Fault,
		)
	}
	for _, i := range d.Indicators {
		fmt.Fprintf(&b, "indicator %s [%s] on=%v bad=%v\n", i.Name, i.Class, i.On, i.Bad())
	}
	for _, c := range d.Buttons {
		fmt.Fprintf(&b, "button %s %s\n", c.Name, c.Entity)
	}
	for _, c := range d.Toggles {
		fmt.Fprintf(&b, "toggle %s on=%v\n", c.Name, c.On)
	}
	if d.Message != "" {
		fmt.Fprintf(&b, "message %q\n", d.Message)
	}
	return b.String()
}

func orNone(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
