package clock

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func setClock(t time.Time) error {
	ts := unix.NsecToTimespec(t.UnixNano())
	if err := unix.ClockSettime(unix.CLOCK_REALTIME, &ts); err != nil {
		return fmt.Errorf("clock: setting the clock: %w", err)
	}
	return nil
}

func slew(by time.Duration) error {
	tx := unix.Timex{Modes: unix.ADJ_OFFSET_SINGLESHOT}
	setOffset(&tx.Offset, by.Microseconds())
	if _, err := unix.Adjtimex(&tx); err != nil {
		return fmt.Errorf("clock: adjusting the clock: %w", err)
	}
	return nil
}

func writeRTC(path string, t time.Time) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("clock: %w", err)
	}
	defer f.Close()

	u := t.UTC()
	rt := unix.RTCTime{
		Sec:  int32(u.Second()),
		Min:  int32(u.Minute()),
		Hour: int32(u.Hour()),
		Mday: int32(u.Day()),
		Mon:  int32(u.Month()) - 1,
		Year: int32(u.Year()) - 1900,
	}
	if err := unix.IoctlSetRTCTime(int(f.Fd()), &rt); err != nil {
		return fmt.Errorf("clock: setting %s: %w", path, err)
	}
	return nil
}

// Timex.Offset is 32 bits on arm and 64 on arm64.
func setOffset[T int32 | int64](field *T, us int64) { *field = T(us) }
