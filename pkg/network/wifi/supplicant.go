package wifi

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ygelfand/libcountertop/pkg/system/metrics"
)

type Control struct{ conn net.Conn }

const dialsNoisy = 1000

// wpa_supplicant replies to whatever address it was asked from.
func (r *Radio) Dial() (*Control, error) {
	n := r.dials.Add(1)
	if n%dialsNoisy == 0 {
		slog.Warn("opening control sockets in bulk", "opened", n)
	}

	local := fmt.Sprintf("@%s-%d-%d", r.opts.Client, os.Getpid(), n)

	conn, err := net.DialUnix("unixgram",
		&net.UnixAddr{Name: local, Net: "unixgram"},
		&net.UnixAddr{Name: filepath.Join(r.opts.SocketDir, r.opts.Interface), Net: "unixgram"})
	if err != nil {
		return nil, fmt.Errorf("wpa_supplicant control socket: %w", err)
	}
	return &Control{conn: conn}, nil
}

func (c *Control) Close() error { return c.conn.Close() }

// A datagram reply that does not fit is truncated silently; SCAN_RESULTS can run well past 4 KB.
const reply = 32 << 10

func (c *Control) Cmd(cmd string) (string, error) {
	if err := c.conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return "", err
	}
	if _, err := c.conn.Write([]byte(cmd)); err != nil {
		return "", fmt.Errorf("%s: %w", cmd, err)
	}

	buf := make([]byte, reply)
	n, err := c.conn.Read(buf)
	if err != nil {
		return "", fmt.Errorf("%s: %w", cmd, err)
	}
	return strings.TrimSpace(string(buf[:n])), nil
}

func (c *Control) Status() (map[string]string, error) {
	out, err := c.Cmd("STATUS")
	if err != nil {
		return nil, err
	}

	status := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			status[k] = v
		}
	}
	return status, nil
}

func (r *Radio) Signal() metrics.Reading {
	c, err := r.Dial()
	if err != nil {
		return metrics.Reading{}
	}
	defer c.Close()
	out, err := c.Cmd("SIGNAL_POLL")
	if err != nil {
		return metrics.Reading{}
	}
	return parseSignal(out)
}

// SIGNAL_POLL answers FAIL while not associated.
func parseSignal(out string) metrics.Reading {
	for line := range strings.SplitSeq(out, "\n") {
		v, ok := strings.CutPrefix(strings.TrimSpace(line), "RSSI=")
		if !ok {
			continue
		}
		dbm, err := strconv.Atoi(v)
		if err != nil || dbm >= 0 {
			return metrics.Reading{}
		}
		return metrics.Reading{Value: float64(dbm), Known: true}
	}
	return metrics.Reading{}
}

type eventSocket struct{ *Control }

func (c eventSocket) Read(buf []byte) (int, error)       { return c.conn.Read(buf) }
func (c eventSocket) SetReadDeadline(at time.Time) error { return c.conn.SetReadDeadline(at) }

// RECONNECT only acts on a supplicant told to DISCONNECT, and answers OK either way.
func (r *Radio) Connect() error {
	c, err := r.Dial()
	if err != nil {
		return err
	}
	defer c.Close()

	// REASSOCIATE tears down an association that is already good.
	if s, err := c.Status(); err == nil && s["wpa_state"] == "COMPLETED" {
		return nil
	}

	for _, cmd := range []string{"ENABLE_NETWORK all", "REASSOCIATE"} {
		out, err := c.Cmd(cmd)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(out, "OK") {
			return fmt.Errorf("wifi: %s said %q", cmd, out)
		}
	}
	return nil
}

func (r *Radio) awaitSupplicant() error {
	if c, err := r.Dial(); err == nil {
		_ = c.Close()
		return nil
	}

	dir, conf := r.opts.SocketDir, r.opts.ConfigPath
	if err := os.MkdirAll(dir, 0o770); err != nil {
		return fmt.Errorf("wifi: %w", err)
	}
	if _, err := os.Stat(conf); err != nil {
		minimal := fmt.Sprintf(
			"ctrl_interface=%s\nupdate_config=1\nbgscan=%q\n",
			dir,
			r.opts.Bgscan,
		)
		if err := os.WriteFile(conf, []byte(minimal), 0o660); err != nil {
			return fmt.Errorf("wifi: writing %s: %w", conf, err)
		}
	}
	for path, mode := range map[string]os.FileMode{
		dir:  0o770,
		conf: 0o660,
	} {
		if err := os.Chmod(path, mode); err != nil {
			return fmt.Errorf("wifi: %w", err)
		}
		if err := os.Chown(path, r.opts.User, r.opts.User); err != nil {
			return fmt.Errorf("wifi: %w", err)
		}
	}

	c, err := r.Dial()
	if err != nil {
		return fmt.Errorf("wifi: no supplicant on %s: %w", dir, err)
	}
	_ = c.Close()
	return nil
}
