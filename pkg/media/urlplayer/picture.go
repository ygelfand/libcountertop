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

	"github.com/ygelfand/libcountertop/pkg/media/cenc"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

const (
	AVC        = "V_MPEG4/ISO/AVC"
	heldFrames = 300
	heldAudio  = 256
)

type avcConfig struct {
	lengthSize int
	sps, pps   [][]byte
}

func parseAVCConfig(b []byte) (avcConfig, error) {
	var c avcConfig
	if len(b) < 7 || b[0] != 1 {
		return c, errors.New("urlplayer: not an avcC record")
	}
	c.lengthSize = int(b[4]&3) + 1
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
		return c, err
	}
	if at >= len(b) {
		return c, errors.New("urlplayer: avcC record cut short")
	}
	pps, _, err := read(at+1, int(b[at]))
	if err != nil {
		return c, err
	}
	c.sps, c.pps = sps, pps
	return c, nil
}

func (c avcConfig) units(data []byte) ([][]byte, error) {
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

func (c avcConfig) idr(data []byte) bool {
	units, err := c.units(data)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(units, func(u []byte) bool {
		return len(u) > 0 && h264.NALUType(u[0]&0x1f) == h264.NALUTypeIDR
	})
}

type sample struct {
	video  bool
	data   []byte
	at     time.Duration
	key    bool
	config *avcConfig
	sealed *cenc.Sample
	track  *cenc.Track
}

type videoFrame struct {
	data []byte
	at   time.Duration
}

type picture struct {
	width, height int
	config        avcConfig
	frames        chan videoFrame
	keyed         bool
}

func newPicture(width, height int, c avcConfig) *picture {
	return &picture{
		width: width, height: height, config: c,
		frames: make(chan videoFrame, heldFrames),
	}
}

func matroskaPicture(codec string, private []byte, width, height int) (*picture, error) {
	if codec != AVC {
		return nil, fmt.Errorf("%w: video %s", errUnplayable, codec)
	}
	c, err := parseAVCConfig(private)
	if err != nil {
		return nil, err
	}
	return newPicture(width, height, c), nil
}

func (p *picture) playback() playback.Picture {
	return playback.Picture{H264: true, Width: p.width, Height: p.height, Next: p.next}
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
		au = append(append(append([][]byte(nil), c.sps...), c.pps...), au...)
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
