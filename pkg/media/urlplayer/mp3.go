package urlplayer

import (
	"bufio"
	"errors"
)

const syncWindow = 64 << 10

var (
	mpeg1Layer3 = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	mpeg2Layer3 = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
	mp3Rates    = [4][3]int{{11025, 12000, 8000}, {}, {22050, 24000, 16000}, {44100, 48000, 32000}}
)

func mp3FrameLen(h []byte) int {
	if len(h) < 4 || h[0] != 0xff || h[1]&0xe0 != 0xe0 || (h[1]>>1)&3 != 1 {
		return 0
	}
	version, bitrate, rate := (h[1]>>3)&3, int(h[2]>>4), int(h[2]>>2)&3
	if version == 1 || bitrate == 0 || bitrate == 15 || rate == 3 {
		return 0
	}
	pad := int(h[2]>>1) & 1
	if version == 3 {
		return 144*mpeg1Layer3[bitrate]*1000/mp3Rates[version][rate] + pad
	}
	return 72*mpeg2Layer3[bitrate]*1000/mp3Rates[version][rate] + pad
}

func mp3Sync(r *bufio.Reader) error {
	for range syncWindow {
		h, err := r.Peek(4)
		if err != nil {
			return err
		}
		if n := mp3FrameLen(h); n > 0 {
			if next, err := r.Peek(n + 4); err == nil && mp3FrameLen(next[n:]) > 0 {
				return nil
			}
		}
		if _, err := r.Discard(1); err != nil {
			return err
		}
	}
	return errors.New("urlplayer: mp3: no frame sync")
}

func mp3Pace(r *bufio.Reader) (tag, bytesPerSecond int64) {
	head, err := r.Peek(10)
	if err != nil {
		return 0, 0
	}
	if string(head[:3]) == "ID3" {
		tag = 10 + (int64(head[6]&0x7f)<<21 | int64(head[7]&0x7f)<<14 |
			int64(head[8]&0x7f)<<7 | int64(head[9]&0x7f))
		if head[5]&0x10 != 0 {
			tag += 10
		}
	}
	frame, err := r.Peek(int(tag) + 4)
	if err != nil {
		return 0, 0
	}
	h := frame[tag:]
	if h[0] != 0xff || h[1]&0xe0 != 0xe0 || (h[1]>>1)&3 != 1 {
		return 0, 0
	}
	table := mpeg2Layer3
	if (h[1]>>3)&3 == 3 {
		table = mpeg1Layer3
	}
	kbps := table[h[2]>>4]
	if kbps == 0 {
		return 0, 0
	}
	return tag, int64(kbps) * 1000 / 8
}
