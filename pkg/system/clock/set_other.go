//go:build !linux

package clock

import (
	"errors"
	"time"
)

func setClock(time.Time) error { return errors.New("clock: only works on the device") }

func slew(time.Duration) error { return errors.New("clock: only works on the device") }

func writeRTC(string, time.Time) error { return errors.New("clock: only works on the device") }
