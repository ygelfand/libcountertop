package adb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

// `adb shell` on older adb returns adb's status, not the command's.
const rcMarker = "__adb_rc="

const backTimeout = 90 * time.Second

type Device struct {
	serial string
}

type Error struct {
	Cmd    string
	Output string
	Code   int
}

func (e *Error) Error() string {
	out := strings.TrimSpace(e.Output)
	if out == "" {
		return fmt.Sprintf("adb: %q exited %d", e.Cmd, e.Code)
	}
	return fmt.Sprintf("adb: %q exited %d: %s", e.Cmd, e.Code, out)
}

func (d *Device) Serial() string { return d.serial }

func (d *Device) InRecovery() (bool, error) {
	serials, err := listing(StateRecovery)
	if err != nil {
		return false, err
	}
	return slices.Contains(serials, d.serial), nil
}

func (d *Device) Reboot(target string) error {
	args := []string{"reboot"}
	if target != "" {
		args = append(args, target)
	}
	_, err := d.run(context.Background(), args...)
	return err
}

func (d *Device) Remount() error {
	_, err := d.run(context.Background(), "remount")
	return err
}

func (d *Device) Root(ctx context.Context) error {
	if ok, err := d.IsRoot(); err == nil && ok {
		return nil
	}
	if _, err := d.run(ctx, "root"); err != nil {
		return err
	}
	time.Sleep(time.Second)
	if _, err := d.run(ctx, "wait-for-device"); err != nil {
		return err
	}

	ok, err := d.IsRoot()
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("adb root did not take: adbd is still unprivileged")
	}
	return nil
}

func (d *Device) DisableVerity(ctx context.Context) (reboot bool, err error) {
	out, err := d.run(ctx, "disable-verity")
	if err != nil {
		return false, fmt.Errorf("disable-verity: %w", err)
	}
	return !strings.Contains(strings.ToLower(out), "already disabled"), nil
}

// adb reports a device present a moment before it will run anything.
func (d *Device) Wait(ctx context.Context, states ...string) error {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		serials, err := listing(states...)
		if err == nil && slices.Contains(serials, d.serial) {
			if _, err := d.rawShell("true"); err == nil {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("adb: waiting for %s in %v: %w", d.serial, states, ctx.Err())
		case <-tick.C:
		}
	}
}

func (d *Device) WaitRooted(ctx context.Context) error {
	if err := d.Wait(ctx, StateOnline); err != nil {
		return err
	}

	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		if root, err := d.IsRoot(); err == nil && root {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("adb: %s did not come back as root: %w", d.serial, ctx.Err())
		case <-tick.C:
		}
	}
}

// sys.boot_completed is set by system_server.
func (d *Device) WaitBooted(ctx context.Context) error {
	if err := d.Wait(ctx, StateOnline); err != nil {
		return err
	}

	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		if done, _ := d.Getprop("sys.boot_completed"); done == "1" {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("adb: %s did not finish booting: %w", d.serial, ctx.Err())
		case <-tick.C:
		}
	}
}

func (d *Device) Shell(cmd string) (string, error) {
	out, code, err := d.ShellCode(cmd)
	if err != nil {
		return out, err
	}
	if code != 0 {
		return out, &Error{Cmd: cmd, Output: out, Code: code}
	}
	return out, nil
}

func (d *Device) ShellCode(cmd string) (string, int, error) {
	raw, err := d.withRetry(func() (string, error) {
		raw, err := d.rawShell(cmd)
		if err == nil && !strings.Contains(raw, rcMarker) {
			return raw, errCutOff
		}
		return raw, err
	})
	if err != nil {
		return "", 0, fmt.Errorf("adb: running %q: %w", cmd, err)
	}
	out, code, err := splitRC(raw)
	if err != nil {
		return out, 0, fmt.Errorf("adb: running %q: %w", cmd, err)
	}
	return out, code, nil
}

func splitRC(raw string) (string, int, error) {
	out := strings.ReplaceAll(raw, "\r\n", "\n")

	i := strings.LastIndex(out, rcMarker)
	if i < 0 {
		return out, 0, fmt.Errorf("no %s marker in output %q", rcMarker, out)
	}
	status, trailing, _ := strings.Cut(out[i+len(rcMarker):], "\n")
	code, err := strconv.Atoi(strings.TrimSpace(status))
	if err != nil {
		return out, 0, fmt.Errorf("unparseable exit status in %q", out[i:])
	}
	text := strings.TrimSuffix(out[:i], "\n")
	if trailing = strings.TrimSuffix(trailing, "\n"); trailing != "" {
		if text != "" {
			text += "\n"
		}
		text += trailing
	}
	return text, code, nil
}

func (d *Device) Exists(path string) (bool, error) {
	_, code, err := d.ShellCode("ls -d " + quote(path))
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

func (d *Device) IsSymlink(path string) (bool, error) {
	_, code, err := d.ShellCode("[ -L " + quote(path) + " ]")
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

// exec-out is the only adb shell that does not translate line endings.
func (d *Device) ReadFile(path string) ([]byte, error) {
	out, err := d.withRetry(func() (string, error) {
		b, err := d.command(context.Background(), "exec-out", "cat "+quote(path)).Output()
		return string(b), err
	})
	if err != nil {
		return nil, fmt.Errorf("adb: reading %s: %w", path, err)
	}
	return []byte(out), nil
}

func (d *Device) PullFile(remote, local string) error {
	if _, err := d.run(context.Background(), "pull", remote, local); err != nil {
		return fmt.Errorf("adb: pulling %s: %w", remote, err)
	}
	return nil
}

func (d *Device) WriteFile(remote string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp("", "adb")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return d.PushFile(tmp.Name(), remote, mode)
}

// adb push widens a new file's owner bits to group and other, and leaves an overwritten one 0644.
func (d *Device) PushFile(local, remote string, mode os.FileMode) error {
	if _, err := d.withRetry(func() (string, error) {
		return d.run(context.Background(), "push", local, remote)
	}); err != nil {
		return fmt.Errorf("adb: pushing %s: %w", remote, err)
	}

	_, err := d.Shell(fmt.Sprintf("chmod %o %s", mode.Perm(), quote(remote)))
	return err
}

func (d *Device) Getprop(name string) (string, error) {
	out, err := d.Shell("getprop " + quote(name))
	return strings.TrimSpace(out), err
}

func (d *Device) Setprop(name, value string) error {
	_, err := d.Shell(fmt.Sprintf("setprop %s %s", quote(name), quote(value)))
	return err
}

func (d *Device) Label(path string) (string, error) {
	out, err := d.Shell("ls -Zd " + quote(path))
	if err != nil {
		return "", err
	}
	for f := range strings.FieldsSeq(out) {
		if strings.HasPrefix(f, "u:object_r:") {
			return f, nil
		}
	}
	return "", fmt.Errorf("adb: no SELinux label in %q", strings.TrimSpace(out))
}

func (d *Device) Chcon(label, path string) error {
	_, err := d.Shell(fmt.Sprintf("chcon %s %s", quote(label), quote(path)))
	return err
}

// id -u is unsupported on some Android builds.
func (d *Device) IsRoot() (bool, error) {
	out, err := d.Shell("id")
	if err != nil {
		return false, err
	}
	return strings.Contains(out, "uid=0("), nil
}

func (d *Device) Uptime() (float64, error) {
	out, err := d.Shell("cat /proc/uptime")
	if err != nil {
		return 0, err
	}
	first, _, _ := strings.Cut(strings.TrimSpace(out), " ")
	v, err := strconv.ParseFloat(first, 64)
	if err != nil {
		return 0, fmt.Errorf("adb: unparseable uptime %q", out)
	}
	return v, nil
}

func (d *Device) command(ctx context.Context, args ...string) *exec.Cmd {
	if d.serial != "" {
		args = append([]string{"-s", d.serial}, args...)
	}
	return exec.CommandContext(ctx, Binary, args...)
}

func (d *Device) run(ctx context.Context, args ...string) (string, error) {
	return output(d.command(ctx, args...))
}

func (d *Device) rawShell(cmd string) (string, error) {
	return d.run(context.Background(), "shell", cmd+"; echo "+rcMarker+"$?")
}

func (d *Device) withRetry(do func() (string, error)) (string, error) {
	out, err := do()
	if !Dropped(err) {
		return out, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), backTimeout)
	defer cancel()

	if err := d.back(ctx); err != nil {
		return out, err
	}
	return do()
}

func (d *Device) back(ctx context.Context) error {
	_ = exec.Command(Binary, "kill-server").Run()
	time.Sleep(time.Second)
	_ = exec.Command(Binary, "start-server").Run()
	time.Sleep(time.Second)

	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()

	for {
		if raw, err := d.rawShell("true"); err == nil && strings.Contains(raw, rcMarker) {
			time.Sleep(2 * time.Second)
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("adb: %s went away and did not come back: %w", d.serial, ctx.Err())
		case <-tick.C:
		}
	}
}
