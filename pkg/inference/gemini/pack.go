package gemini

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

const hotwordResource = 33800

var errNoDetector = errors.New("gemini: no detector model in the hotword pack")

// A Chromium resource pack, version 5: header, then (id uint16, offset uint32) entries ending in a sentinel.
func resource(pak []byte, id uint16) ([]byte, error) {
	if len(pak) < 12 || binary.LittleEndian.Uint32(pak) != 5 {
		return nil, errors.New("gemini: not a version 5 resource pack")
	}
	count := int(binary.LittleEndian.Uint16(pak[8:]))
	if len(pak) < 12+6*(count+1) {
		return nil, errors.New("gemini: truncated resource pack")
	}
	for i := 0; i < count; i++ {
		e := pak[12+6*i:]
		if binary.LittleEndian.Uint16(e) != id {
			continue
		}
		start := binary.LittleEndian.Uint32(e[2:])
		end := binary.LittleEndian.Uint32(e[8:])
		if start > end || int(end) > len(pak) {
			return nil, fmt.Errorf("gemini: resource %d runs past the pack", id)
		}
		return pak[start:end], nil
	}
	return nil, fmt.Errorf("gemini: resource %d missing", id)
}

// The hotword resource wraps TFLite flatbuffers, each after a 02 f8 tag and its uint32 length; the detector is the first.
func detector(res []byte) ([]byte, error) {
	at := bytes.Index(res, []byte("TFL3"))
	if at < 4 {
		return nil, errNoDetector
	}
	start := at - 4
	tag := bytes.LastIndex(res[max(0, start-48):start], []byte{0x02, 0xf8})
	if tag < 0 {
		return nil, errNoDetector
	}
	tag += max(0, start-48)
	if tag+6 > len(res) {
		return nil, errNoDetector
	}
	n := int(binary.LittleEndian.Uint32(res[tag+2:]))
	if start+n > len(res) {
		return nil, errNoDetector
	}
	return res[start : start+n], nil
}
