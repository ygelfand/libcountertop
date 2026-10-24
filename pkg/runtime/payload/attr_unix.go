//go:build unix && !linux

package payload

import (
	"os"
	"syscall"
)

func attrs() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

func killGroup(p *os.Process) { _ = syscall.Kill(-p.Pid, syscall.SIGKILL) }
