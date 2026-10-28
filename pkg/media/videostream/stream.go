package videostream

import (
	"encoding/binary"

	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

func Split(stream []byte) (codec, width, height uint32, units [][]byte) {
	if len(stream) >= 32 && string(stream[:4]) == "DKIF" {
		codec = binary.LittleEndian.Uint32(stream[8:])
		width = uint32(binary.LittleEndian.Uint16(stream[12:]))
		height = uint32(binary.LittleEndian.Uint16(stream[14:]))
		return codec, width, height, IVFFrames(stream)
	}
	return playback.H264, 1920, 1080, AccessUnits(stream)
}

func AccessUnits(stream []byte) [][]byte {
	var starts []int
	for i := 0; i+4 < len(stream); i++ {
		if stream[i] == 0 && stream[i+1] == 0 && stream[i+2] == 1 && stream[i+3]&0x1f == 9 {
			at := i
			if at > 0 && stream[at-1] == 0 {
				at--
			}
			starts = append(starts, at)
		}
	}
	var out [][]byte
	for k, s := range starts {
		end := len(stream)
		if k+1 < len(starts) {
			end = starts[k+1]
		}
		out = append(out, stream[s:end])
	}
	return out
}

func IVFFrames(stream []byte) [][]byte {
	at := int(binary.LittleEndian.Uint16(stream[6:]))
	var out [][]byte
	for at+12 <= len(stream) {
		n := int(binary.LittleEndian.Uint32(stream[at:]))
		at += 12
		if n < 0 || at+n > len(stream) {
			break
		}
		out = append(out, stream[at:at+n])
		at += n
	}
	return out
}
