//go:build !unix

package payload

import (
	"os"
	"syscall"
)

func attrs() *syscall.SysProcAttr { return nil }

func killGroup(p *os.Process) { _ = p.Kill() }
