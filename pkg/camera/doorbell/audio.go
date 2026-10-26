package doorbell

import (
	"encoding/binary"
	"strings"

	aacpcm "github.com/tphakala/go-aac/pcm"
	"github.com/ygelfand/reolinkproxy/pkg/codec"

	"github.com/ygelfand/libcountertop/pkg/audio/resample"
	"github.com/ygelfand/libcountertop/pkg/media/rtc"
	"github.com/ygelfand/libcountertop/pkg/media/rtsp"
)

const (
	adpcmRate  = 8000
	adtsHeader = 7
)

type ear struct {
	out func([]int16)

	aac    *aacpcm.FrameDecoder
	aacKey string
	aacBuf []byte
	adpcm  codec.ADPCMDecoder
	up     *resample.Rational
	upFrom int
	stereo []int16
}

func newEar(out func([]int16)) *ear { return &ear{out: out} }

func (e *ear) rtsp(s rtsp.Sound) {
	switch strings.ToUpper(s.Codec) {
	case "PCMU":
		e.mono(codec.DecodePCMU(s.Data), s.Rate)
	case "PCMA":
		e.mono(codec.DecodePCMA(s.Data), s.Rate)
	case "MPEG4-GENERIC":
		for _, au := range rfc3640(s.Data) {
			e.raw(s.Config, au)
		}
	}
}

func (e *ear) adts(b []byte) {
	for len(b) >= adtsHeader && b[0] == 0xff && b[1]&0xf0 == 0xf0 {
		size := int(b[3]&0x03)<<11 | int(b[4])<<3 | int(b[5])>>5
		head := adtsHeader
		if b[1]&0x01 == 0 {
			head += 2
		}
		if size < head || size > len(b) {
			return
		}
		profile := b[2]>>6 + 1
		freq := (b[2] >> 2) & 0x0f
		channels := (b[2]&0x01)<<2 | b[3]>>6
		config := []byte{profile<<3 | freq>>1, freq<<7 | channels<<3}
		e.raw(config, b[head:size])
		b = b[size:]
	}
}

func (e *ear) raw(config, au []byte) {
	if len(config) < 2 || len(au) == 0 {
		return
	}
	if key := string(config); e.aac == nil || key != e.aacKey {
		d, err := aacpcm.NewRawDecoder(config)
		if err != nil {
			return
		}
		e.aac, e.aacKey = d, key
	}
	out, _, err := e.aac.DecodeFrame(e.aacBuf[:0], au)
	if err != nil {
		return
	}
	e.aacBuf = out
	channels := max(e.aac.Channels(), 1)
	frames := len(out) / (2 * channels)
	mono := make([]int16, frames)
	for i := range frames {
		var sum int
		for c := range channels {
			sum += int(int16(binary.LittleEndian.Uint16(out[(i*channels+c)*2:])))
		}
		mono[i] = int16(sum / channels)
	}
	e.mono(mono, e.aac.SampleRate())
}

func (e *ear) adpcmBlock(b []byte) { e.mono(e.adpcm.Decode(b), adpcmRate) }

func (e *ear) mono(pcm []int16, rate int) {
	if len(pcm) == 0 || rate <= 0 {
		return
	}
	if e.up == nil || e.upFrom != rate {
		e.up, e.upFrom = resample.NewRational(rate, rtc.PlayRate, 1), rate
	}
	up := e.up.Run(pcm)
	e.stereo = e.stereo[:0]
	for _, v := range up {
		e.stereo = append(e.stereo, v, v)
	}
	e.out(e.stereo)
}

func rfc3640(p []byte) [][]byte {
	if len(p) < 2 {
		return nil
	}
	headerBits := int(binary.BigEndian.Uint16(p))
	headerBytes := (headerBits + 7) / 8
	if len(p) < 2+headerBytes {
		return nil
	}
	headers, data := p[2:2+headerBytes], p[2+headerBytes:]
	var out [][]byte
	for i := 0; i+1 < len(headers) && i*8 < headerBits; i += 2 {
		size := int(binary.BigEndian.Uint16(headers[i:])) >> 3
		if size > len(data) {
			break
		}
		out = append(out, data[:size])
		data = data[size:]
	}
	return out
}

func encodeMuLaw(pcm []int16) []byte {
	out := make([]byte, len(pcm))
	for i, s := range pcm {
		out[i] = muLaw(s)
	}
	return out
}

func muLaw(s int16) byte {
	const bias, clip = 0x84, 32635
	v := int(s)
	sign := 0
	if v < 0 {
		v, sign = -v, 0x80
	}
	v = min(v, clip) + bias
	exponent := 7
	for mask := 0x4000; v&mask == 0 && exponent > 0; mask >>= 1 {
		exponent--
	}
	mantissa := (v >> (exponent + 3)) & 0x0f
	return ^byte(sign | exponent<<4 | mantissa)
}
