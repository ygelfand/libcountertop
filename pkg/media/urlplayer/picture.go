package urlplayer

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h265"

	"github.com/ygelfand/libcountertop/pkg/media/cenc"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

const (
	AVC        = "V_MPEG4/ISO/AVC"
	HEVC       = "V_MPEGH/ISO/HEVC"
	heldFrames = 300
	heldAudio  = 256
	hevcMain   = 1
)

type nalConfig struct {
	codec      uint32
	lengthSize int
	params     [][]byte
}

func avcConfig(lengthSize int, sps, pps [][]byte) nalConfig {
	return nalConfig{
		codec:      playback.H264,
		lengthSize: lengthSize,
		params:     append(slices.Clone(sps), pps...),
	}
}

func parseAVCConfig(b []byte) (nalConfig, error) {
	if len(b) < 7 || b[0] != 1 {
		return nalConfig{}, errors.New("urlplayer: not an avcC record")
	}
	lengthSize := int(b[4]&3) + 1
	read := func(at int, count int) ([][]byte, int, error) {
		var out [][]byte
		for range count {
			if at+2 > len(b) {
				return nil, at, errors.New("urlplayer: avcC record cut short")
			}
			n := int(binary.BigEndian.Uint16(b[at:]))
			at += 2
			if at+n > len(b) {
				return nil, at, errors.New("urlplayer: avcC record cut short")
			}
			out = append(out, b[at:at+n])
			at += n
		}
		return out, at, nil
	}
	sps, at, err := read(6, int(b[5]&0x1f))
	if err != nil {
		return nalConfig{}, err
	}
	if at >= len(b) {
		return nalConfig{}, errors.New("urlplayer: avcC record cut short")
	}
	pps, _, err := read(at+1, int(b[at]))
	if err != nil {
		return nalConfig{}, err
	}
	return avcConfig(lengthSize, sps, pps), nil
}

func hevcConfig(vps, sps, pps []byte) (nalConfig, int, int, error) {
	var s h265.SPS
	if err := s.Unmarshal(sps); err != nil {
		return nalConfig{}, 0, 0, err
	}
	if s.ProfileTierLevel.GeneralProfileIdc != hevcMain || s.BitDepthLumaMinus8 != 0 {
		return nalConfig{}, 0, 0, fmt.Errorf(
			"%w: HEVC profile %d at %d bits",
			errUnplayable,
			s.ProfileTierLevel.GeneralProfileIdc,
			s.BitDepthLumaMinus8+8,
		)
	}
	c := nalConfig{codec: playback.HEVC, lengthSize: 4, params: [][]byte{vps, sps, pps}}
	return c, s.Width(), s.Height(), nil
}

func parseHEVCConfig(b []byte) (nalConfig, error) {
	if len(b) < 23 || b[0] != 1 {
		return nalConfig{}, errors.New("urlplayer: not an hvcC record")
	}
	if b[1]&0x1f != hevcMain {
		return nalConfig{}, fmt.Errorf("%w: HEVC profile %d", errUnplayable, b[1]&0x1f)
	}
	sets := map[h265.NALUType][][]byte{}
	at := 23
	for range int(b[22]) {
		if at+3 > len(b) {
			return nalConfig{}, errors.New("urlplayer: hvcC record cut short")
		}
		kind := h265.NALUType(b[at] & 0x3f)
		count := int(binary.BigEndian.Uint16(b[at+1:]))
		at += 3
		for range count {
			if at+2 > len(b) {
				return nalConfig{}, errors.New("urlplayer: hvcC record cut short")
			}
			n := int(binary.BigEndian.Uint16(b[at:]))
			at += 2
			if at+n > len(b) {
				return nalConfig{}, errors.New("urlplayer: hvcC record cut short")
			}
			sets[kind] = append(sets[kind], b[at:at+n])
			at += n
		}
	}
	vps, sps, pps := sets[h265.NALUType_VPS_NUT], sets[h265.NALUType_SPS_NUT], sets[h265.NALUType_PPS_NUT]
	if len(vps) == 0 || len(sps) == 0 || len(pps) == 0 {
		return nalConfig{}, errors.New("urlplayer: hvcC record without VPS, SPS and PPS")
	}
	return nalConfig{
		codec:      playback.HEVC,
		lengthSize: int(b[21]&3) + 1,
		params:     slices.Concat(vps, sps, pps),
	}, nil
}

func (c nalConfig) units(data []byte) ([][]byte, error) {
	var out [][]byte
	for len(data) > 0 {
		if len(data) < c.lengthSize {
			return nil, errors.New("urlplayer: a NAL unit length cut short")
		}
		n := 0
		for _, b := range data[:c.lengthSize] {
			n = n<<8 | int(b)
		}
		data = data[c.lengthSize:]
		if n > len(data) {
			return nil, errors.New("urlplayer: a NAL unit runs past its frame")
		}
		out = append(out, data[:n])
		data = data[n:]
	}
	return out, nil
}

func (c nalConfig) key(data []byte) bool {
	units, err := c.units(data)
	if err != nil {
		return false
	}
	units = slices.DeleteFunc(units, func(u []byte) bool { return len(u) == 0 })
	if c.codec == playback.HEVC {
		return h265.IsRandomAccess(units)
	}
	return slices.ContainsFunc(units, func(u []byte) bool {
		return h264.NALUType(u[0]&0x1f) == h264.NALUTypeIDR
	})
}

type sample struct {
	video  bool
	data   []byte
	at     time.Duration
	key    bool
	config *nalConfig
	sealed *cenc.Sample
	track  *cenc.Track
}

type videoFrame struct {
	data []byte
	at   time.Duration
}

type picture struct {
	width, height int
	config        nalConfig
	frames        chan videoFrame
	keyed         bool
}

func newPicture(width, height int, c nalConfig) *picture {
	return &picture{
		width: width, height: height, config: c,
		frames: make(chan videoFrame, heldFrames),
	}
}

func matroskaConfig(codec string, private []byte) (nalConfig, error) {
	switch codec {
	case AVC:
		return parseAVCConfig(private)
	case HEVC:
		return parseHEVCConfig(private)
	}
	return nalConfig{}, fmt.Errorf("%w: video %s", errUnplayable, codec)
}

func matroskaPicture(codec string, private []byte, width, height int) (*picture, error) {
	c, err := matroskaConfig(codec, private)
	if err != nil {
		return nil, err
	}
	return newPicture(width, height, c), nil
}

func (p *picture) playback() playback.Picture {
	return playback.Picture{Codec: p.config.codec, Width: p.width, Height: p.height, Next: p.next}
}

func (p *picture) push(ctx context.Context, s sample) {
	if !p.keyed && !s.key {
		return
	}
	c := p.config
	if s.config != nil {
		c = *s.config
	}
	au, err := c.units(s.data)
	if err != nil {
		return
	}
	if s.key {
		au = slices.Concat(c.params, au)
	}
	data, err := h264.AnnexB(au).Marshal()
	if err != nil {
		return
	}
	select {
	case p.frames <- videoFrame{data: data, at: s.at}:
		p.keyed = true
	case <-ctx.Done():
	}
}

func (p *picture) next(ctx context.Context) ([]byte, time.Duration, error) {
	select {
	case f, ok := <-p.frames:
		if !ok {
			return nil, 0, io.EOF
		}
		return f.data, f.at, nil
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}
}

type muxed struct {
	samples func() (sample, error)
	frame   func(data []byte) ([]int16, error)
	hz      int
	from    time.Duration
	pic     *picture
	audio   chan sample

	first    sample
	firstErr error

	mu  sync.Mutex
	err error

	failures int
}

func startMuxed(
	ctx context.Context,
	samples func() (sample, error),
	frame func([]byte) ([]int16, error),
	hz int,
	from time.Duration,
	pic *picture,
) *muxed {
	x := &muxed{
		samples: samples, frame: frame, hz: hz, from: from, pic: pic,
		audio: make(chan sample, heldAudio),
	}
	x.first, x.firstErr = samples()
	go x.run(ctx)
	return x
}

func (x *muxed) begins() time.Duration { return x.first.at }

func (x *muxed) run(ctx context.Context) {
	defer close(x.audio)
	if x.pic != nil {
		defer close(x.pic.frames)
	}
	for s, err := x.first, x.firstErr; ; s, err = x.samples() {
		if err != nil {
			slog.Debug("the url player's demux stopped", "err", err)
			x.mu.Lock()
			x.err = err
			x.mu.Unlock()
			return
		}
		if s.video {
			if x.pic != nil {
				x.pic.push(ctx, s)
			}
			continue
		}
		if s.at < x.from {
			continue
		}
		select {
		case x.audio <- s:
		case <-ctx.Done():
			return
		}
	}
}

func (x *muxed) rate() int { return x.hz }

func (x *muxed) next() ([]int16, error) {
	for {
		s, ok := <-x.audio
		if !ok {
			x.mu.Lock()
			defer x.mu.Unlock()
			if x.err == nil {
				return nil, io.EOF
			}
			return nil, x.err
		}
		out, err := x.frame(s.data)
		if err == nil {
			return out, nil
		}
		x.failures++
		if x.failures == 1 || x.failures%500 == 0 {
			slog.Info(
				"the url player could not decode sound",
				"failures",
				x.failures,
				"bytes",
				len(s.data),
				"err",
				err,
			)
		}
	}
}
