package thermostatcmd

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ygelfand/libcountertop/pkg/display/thermostatview"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/thermostat"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

var errNoView = errors.New("the thermostat screen is not running")

func Command(
	thermostats func() *thermostat.Thermostats,
	view func() *thermostatview.View,
) *cobra.Command {
	group := &cobra.Command{
		Use:   "thermostat",
		Short: "The Home Assistant thermostats this device shows",
	}
	withView := func(run func(v *thermostatview.View) error) func(*cobra.Command, []string) error {
		return func(*cobra.Command, []string) error {
			v := view()
			if v == nil {
				return errNoView
			}
			return run(v)
		}
	}
	group.AddCommand(
		&cobra.Command{
			Use:   "state",
			Short: "Each chosen thermostat as Home Assistant reports it",
			Args:  cobra.NoArgs,
			RunE: func(c *cobra.Command, _ []string) error {
				t := thermostats()
				c.Printf("unit %q chosen %v\n", t.Unit(), t.Chosen())
				for _, s := range t.States() {
					c.Print(State(s))
				}
				if v := view(); v != nil {
					if list, ok := v.Rehearsing(); ok {
						c.Printf("rehearsing %d\n", len(list))
						for _, s := range list {
							c.Print(State(s))
						}
					}
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "rehearse 1|2|3|4",
			Short: "Show sample thermostats without touching the real ones; changes stay on the screen",
			Args:  cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				n, err := strconv.Atoi(args[0])
				if err != nil || n < 1 || n > schema.ThermostatsMost {
					return fmt.Errorf(
						"a count from 1 to %d, not %q",
						schema.ThermostatsMost,
						args[0],
					)
				}
				return withView(
					func(v *thermostatview.View) error { v.Rehearse(n); return nil },
				)(
					c,
					args,
				)
			},
		},
		&cobra.Command{
			Use:   "open",
			Short: "Open the thermostat app",
			Args:  cobra.NoArgs,
			RunE:  withView(func(v *thermostatview.View) error { v.Open(); return nil }),
		},
		&cobra.Command{
			Use:   "stop",
			Short: "End a rehearsal and show the real thermostats again",
			Args:  cobra.NoArgs,
			RunE:  withView(func(v *thermostatview.View) error { v.StopRehearsal(); return nil }),
		},
	)
	return group
}

func State(s thermostat.State) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s): mode %s action %s fan %q\n", s.Entity, s.Name, orNone(string(s.Mode)),
		orNone(string(s.Action)), s.Fan)
	fmt.Fprintf(&b, "  current %s target %s low %s high %s humidity %s\n", value(s.Current),
		value(s.Target), value(s.Low), value(s.High), value(s.Humidity))
	fmt.Fprintf(
		&b,
		"  modes %v fans %v range %v..%v step %v ranged %v\n",
		s.Modes,
		s.FanModes,
		s.Min,
		s.Max,
		s.Step,
		s.Ranged(),
	)
	return b.String()
}

func value(v *float64) string {
	if v == nil {
		return "-"
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func orNone(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
