package hci

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"sync"
)

const (
	hciLEScanParams = 0x200b
	hciLEScanEnable = 0x200c
)

const (
	eventLEMeta = 0x3e

	leAdvertisingReport = 0x02
)

// 0.625 ms units: a 200 ms interval, and the 2.5 ms shortest window the spec allows.
const (
	scanInterval    = 320
	scanWindowLeast = 4
)

type Scanning struct {
	Active  bool
	Percent int
}

func (sc Scanning) window() uint16 {
	return uint16(min(max(scanInterval*sc.Percent/100, scanWindowLeast), scanInterval))
}

type Advertisement struct {
	Address     [6]byte
	AddressType byte
	RSSI        int8
	Data        []byte
}

func (a Advertisement) Addr() uint64 {
	var v uint64
	for _, b := range a.Address {
		v = v<<8 | uint64(b)
	}
	return v
}

type scanState struct {
	mu     sync.Mutex
	on     *Scanning
	paused int
}

func Scan(ctx context.Context, s *Session, sc Scanning, found func(Advertisement)) error {
	got, done := s.Reports()
	defer done()

	if err := s.startScan(sc); err != nil {
		return err
	}

	defer s.stopScan(sc)

	for {
		select {
		case <-ctx.Done():
			return nil
		case g, ok := <-got:
			if !ok {
				return fmt.Errorf("ble: scanning: %w", s.Err())
			}
			reports(g.event.Params, found)
		}
	}
}

func (s *Session) startScan(sc Scanning) error {
	s.scan.mu.Lock()
	defer s.scan.mu.Unlock()
	if s.scan.paused == 0 {
		if err := scanning(s, sc, true); err != nil {
			return err
		}
	}
	s.scan.on = &sc
	return nil
}

func (s *Session) stopScan(sc Scanning) {
	s.scan.mu.Lock()
	defer s.scan.mu.Unlock()
	s.scan.on = nil
	if s.scan.paused == 0 {
		_ = scanning(s, sc, false)
	}
}

// This controller accepts a connection while it scans, but the link then fails to establish (0x3e).
func (s *Session) Pause() func() {
	s.scan.mu.Lock()
	if s.scan.paused == 0 && s.scan.on != nil {
		if err := scanning(s, *s.scan.on, false); err != nil {
			slog.Warn("bluetooth scan did not pause", "err", err)
		}
	}
	s.scan.paused++
	s.scan.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			s.scan.mu.Lock()
			defer s.scan.mu.Unlock()
			s.scan.paused--
			if s.scan.paused == 0 && s.scan.on != nil {
				if err := scanning(s, *s.scan.on, true); err != nil {
					slog.Warn("bluetooth scan did not resume", "err", err)
				}
			}
		})
	}
}

func scanning(s *Session, sc Scanning, on bool) error {
	if on {
		params := make([]byte, 7)
		if sc.Active {
			params[0] = 0x01
		}
		binary.LittleEndian.PutUint16(params[1:], scanInterval)
		binary.LittleEndian.PutUint16(params[3:], sc.window())

		if _, err := s.Command(hciLEScanParams, params...); err != nil {
			return fmt.Errorf("ble: the scan parameters: %w", err)
		}
	}

	enable := byte(0x00)
	if on {
		enable = 0x01
	}
	if _, err := s.Command(hciLEScanEnable, enable, 0x00); err != nil {
		return fmt.Errorf("ble: turning the scan %v: %w", on, err)
	}
	return nil
}

// Event type, address type, address, data length, data, RSSI.
func reports(p []byte, found func(Advertisement)) {
	if len(p) < 2 || p[0] != leAdvertisingReport {
		return
	}

	at := 2
	for range int(p[1]) {
		if at+9 > len(p) {
			return
		}

		length := int(p[at+8])
		end := at + 9 + length
		if end >= len(p) {
			return
		}

		a := Advertisement{AddressType: p[at+1], RSSI: int8(p[end])}

		// HCI carries the address least significant octet first.
		for i := range a.Address {
			a.Address[i] = p[at+7-i]
		}
		a.Data = append([]byte(nil), p[at+9:end]...)
		found(a)

		at = end + 1
	}
}
