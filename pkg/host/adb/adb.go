package adb

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const Binary = "adb"

const PlatformTools = "https://developer.android.com/tools/releases/platform-tools"

const (
	StateOnline   = "device"
	StateRecovery = "recovery"
)

var ErrUnreachable = errors.New("no device adb can drive")

func Require() error {
	if _, err := exec.LookPath(Binary); err != nil {
		return fmt.Errorf("adb is not on PATH: install Android platform-tools and try again\n"+
			"  %s\n"+
			"  macOS: brew install --cask android-platform-tools\n"+
			"  Debian/Ubuntu: apt install android-sdk-platform-tools", PlatformTools)
	}
	return nil
}

type Info struct {
	Serial  string
	Model   string
	Product string
}

func (i Info) String() string {
	switch {
	case i.Model != "" && i.Product != "":
		return fmt.Sprintf("%s (%s)", i.Model, i.Product)
	case i.Model != "":
		return i.Model
	}
	return i.Serial
}

func List() ([]Info, error) { return list(StateOnline) }

func ListAny() ([]Info, error) { return list(StateOnline, StateRecovery) }

func list(states ...string) ([]Info, error) {
	serials, err := listing(states...)
	if err != nil {
		return nil, err
	}

	out := make([]Info, 0, len(serials))
	for _, serial := range serials {
		out = append(out, probe(serial))
	}
	return out, nil
}

// An adb shell into some devices never returns.
const probeTimeout = 3 * time.Second

func probe(serial string) Info {
	d := &Device{serial: serial}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	model, _ := d.run(ctx, "shell", "getprop ro.product.model")
	product, _ := d.run(ctx, "shell", "getprop ro.product.device")
	return Info{
		Serial:  serial,
		Model:   strings.TrimSpace(model),
		Product: strings.TrimSpace(product),
	}
}

// adb devices prints "serial<tab>state" per line after a header.
func listing(states ...string) ([]string, error) {
	out, err := run(context.Background(), "devices")
	if err != nil {
		return nil, err
	}

	var serials []string
	for line := range strings.SplitSeq(out, "\n") {
		serial, state, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok {
			continue
		}
		for _, want := range states {
			if strings.TrimSpace(state) == want {
				serials = append(serials, serial)
			}
		}
	}
	return serials, nil
}

func Attach(serial string) (*Device, error) {
	return attach(serial, StateOnline)
}

func AttachAny(serial string) (*Device, error) {
	return attach(serial, StateOnline, StateRecovery)
}

func attach(serial string, states ...string) (*Device, error) {
	if err := Require(); err != nil {
		return nil, err
	}
	serials, err := listing(states...)
	if err != nil {
		return nil, err
	}

	switch {
	case len(serials) == 0:
		return nil, ErrUnreachable
	case serial != "":
		for _, s := range serials {
			if s == serial {
				return &Device{serial: s}, nil
			}
		}
		return nil, fmt.Errorf("adb: no device with serial %q", serial)
	case len(serials) > 1:
		return nil, fmt.Errorf("adb: %d devices connected, name one: %s",
			len(serials), strings.Join(serials, ", "))
	}
	return &Device{serial: serials[0]}, nil
}

func run(ctx context.Context, args ...string) (string, error) {
	return output(exec.CommandContext(ctx, Binary, args...))
}

// adb explains refusals on stderr.
func output(cmd *exec.Cmd) (string, error) {
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil {
		said := strings.Join(strings.Fields(text), " ")
		return text, fmt.Errorf("%s: %w: %s", strings.Join(cmd.Args, " "), err, said)
	}
	return text, nil
}

var errCutOff = errors.New("the shell ended before its exit status")

func Dropped(err error) bool {
	if errors.Is(err, errCutOff) {
		return true
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	if exit.ExitCode() == 255 {
		return true
	}
	msg := err.Error()
	return exit.ExitCode() == 1 &&
		(strings.Contains(msg, "adb: device '") && strings.Contains(msg, "' not found") ||
			strings.Contains(msg, "device offline") ||
			strings.Contains(msg, "no devices/emulators found") ||
			strings.Contains(msg, "error: closed"))
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
