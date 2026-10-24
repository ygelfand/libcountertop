//go:build !linux

package payload

import (
	"errors"
	"syscall"
)

func Enter() {}

func isolated(Command) (string, []string, *syscall.SysProcAttr, error) {
	return "", nil, nil, errors.New("payload: mount namespaces need Linux")
}
