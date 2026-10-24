package control

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func NewRoot() *cobra.Command {
	root := Group(
		"ctl",
		"Drive the running device",
		"Commands act on the live daemon. Input coordinates follow the viewed orientation.",
	)
	root.SilenceErrors, root.SilenceUsage = true, true
	root.CompletionOptions.DisableDefaultCmd = true
	return root
}

func Group(use, short, long string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Long:  long,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("%s: %q is not one of its commands", cmd.Name(), args[0])
			}
			return cmd.Help()
		},
	}
}

func Says(cmd *cobra.Command, run func([]string) (string, error)) *cobra.Command {
	cmd.RunE = func(c *cobra.Command, args []string) error {
		out, err := run(args)
		if out != "" {
			fmt.Fprintln(c.OutOrStdout(), strings.TrimRight(out, "\n"))
		}
		return err
	}
	return cmd
}

func ContextSays(
	cmd *cobra.Command,
	run func(context.Context, []string) (string, error),
) *cobra.Command {
	cmd.RunE = func(c *cobra.Command, args []string) error {
		out, err := run(c.Context(), args)
		if out != "" {
			fmt.Fprintln(c.OutOrStdout(), strings.TrimRight(out, "\n"))
		}
		return err
	}
	return cmd
}

func Does(cmd *cobra.Command, run func([]string) error) *cobra.Command {
	cmd.RunE = func(_ *cobra.Command, args []string) error { return run(args) }
	return cmd
}

func Literal(cmd *cobra.Command) {
	cmd.DisableFlagParsing = true
	for _, child := range cmd.Commands() {
		Literal(child)
	}
}

type Task string

const (
	DeviceTask  Task = "device"
	InputTask   Task = "input"
	DisplayTask Task = "display"
	AudioTask   Task = "audio"
)

type Tree struct {
	root  *cobra.Command
	tasks map[Task]*cobra.Command
}

func NewTree() *Tree { return &Tree{root: NewRoot(), tasks: make(map[Task]*cobra.Command)} }

func (t *Tree) Add(task Task, cmds ...*cobra.Command) {
	if len(cmds) == 0 {
		return
	}
	group := t.tasks[task]
	if group == nil {
		group = Group(string(task), "", "")
		t.tasks[task] = group
		t.root.AddCommand(group)
	}
	group.AddCommand(cmds...)
}

func (t *Tree) Top(cmds ...*cobra.Command) { t.root.AddCommand(cmds...) }

func (t *Tree) Root() *cobra.Command {
	for task, group := range t.tasks {
		var names []string
		for _, cmd := range group.Commands() {
			names = append(names, cmd.Name())
		}
		group.Short = string(task) + ": " + strings.Join(names, ", ")
	}
	Literal(t.root)
	return t.root
}

func Executes(makeTree func() *cobra.Command) Dispatch {
	return func(ctx context.Context, args []string) (string, error) {
		return Execute(ctx, makeTree, args)
	}
}

func Execute(ctx context.Context, makeTree func() *cobra.Command, args []string) (string, error) {
	root := makeTree()
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	if len(args) > 0 && (args[len(args)-1] == "--help" || args[len(args)-1] == "-h") {
		cmd, rest, err := root.Find(args[:len(args)-1])
		if err != nil {
			return "", err
		}
		if len(rest) > 0 && cmd.HasAvailableSubCommands() {
			return "", fmt.Errorf("%s: unknown command %q", cmd.Name(), rest[0])
		}
		err = cmd.Help()
		return out.String(), err
	}
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	return out.String(), err
}

func WaitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "wait MILLISECONDS",
		Short: "Wait between harness steps",
		Args:  cobra.ExactArgs(1),
		RunE:  wait,
	}
}

func wait(cmd *cobra.Command, args []string) error {
	ms, err := strconv.Atoi(args[0])
	if err != nil || ms < 0 || ms > 60_000 {
		return fmt.Errorf("%q is not a number of milliseconds up to a minute", args[0])
	}
	timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-cmd.Context().Done():
		return cmd.Context().Err()
	case <-timer.C:
		return nil
	}
}
