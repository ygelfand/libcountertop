package urlplayer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"time"

	gomp3 "github.com/hajimehoshi/go-mp3"
	"github.com/mewkiz/flac"
	"github.com/pion/opus"
	aacpcm "github.com/tphakala/go-aac/pcm"

	"github.com/ygelfand/libcountertop/pkg/media/webm"
)

const (
	MP3      = "mp3"
	AAC      = "aac"
	FLAC     = "flac"
	WAV      = "wav"
	Matroska = "matroska"
	MP4      = "mp4"

	opusRate   = 48000
	opusFrame  = 5760
	readFrames = 4096
)

var errUnplayable = errors.New("urlplayer: not a format this plays")

type decoder interface {
	rate() int
	next() ([]int16, error)
}

func sniff(head []byte, contentType string) string {
	switch {
	case bytes.HasPrefix(head, []byte("#EXTM3U")):
		return HLS
	case bytes.HasPrefix(head, []byte("fLaC")):
		return FLAC
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WAVE":
		return WAV
	case bytes.HasPrefix(head, []byte{0x1a, 0x45, 0xdf, 0xa3}):
		return Matroska
	case len(head) >= 8 && string(head[4:8]) == "ftyp":
		return MP4
	case bytes.HasPrefix(head, []byte("ID3")):
		return MP3
	case len(head) >= 2 && head[0] == 0xff && head[1]&0xf6 == 0xf0:
		return AAC
	case len(head) >= 2 && head[0] == 0xff && head[1]&0xe0 == 0xe0:
		return MP3
	}
	kind, _, _ := mime.ParseMediaType(contentType)
	switch kind {
	case "audio/mpeg", "audio/mp3":
		return MP3
	case "audio/aac", "audio/aacp", "audio/x-aac":
		return AAC
	case "audio/flac", "audio/x-flac":
		return FLAC
	case "audio/wav", "audio/x-wav", "audio/wave":
		return WAV
	case "audio/webm", "video/webm", "audio/x-matroska", "video/x-matroska":
		return Matroska
	case "video/mp4", "audio/mp4", "audio/x-m4a", "audio/m4a", "video/quicktime":
		return MP4
	case "application/vnd.apple.mpegurl",
		"application/x-mpegurl",
		"audio/mpegurl",
		"audio/x-mpegurl":
		return HLS
	}
	return ""
}

func newDecoder(ctx context.Context, format string, r *bufio.Reader) (decoder, error) {
	switch format {
	case MP3:
		return mp3Decoder(r)
	case AAC:
		return aacDecoder(r)
	case FLAC:
		return flacDecoder(r)
	case WAV:
		return wavDecoder(r)
	case Matroska:
		return matroskaDecoder(ctx, r)
	}
	return nil, errUnplayable
}

type s16 struct {
	r        io.Reader
	channels int
	hz       int
	buf      []byte
	held     int
}

func (d *s16) rate() int { return d.hz }

func (d *s16) next() ([]int16, error) {
	frame := 2 * d.channels
	if d.buf == nil {
		d.buf = make([]byte, readFrames*frame)
	}
	n, err := d.r.Read(d.buf[d.held:])
	n += d.held
	whole := n - n%frame
	out := stereo16(d.buf[:whole], d.channels)
	d.held = copy(d.buf, d.buf[whole:n])
	if len(out) > 0 {
		return out, nil
	}
	if err == nil {
		return nil, nil
	}
	return nil, err
}

func stereo16(b []byte, channels int) []int16 {
	frames := len(b) / (2 * channels)
	out := make([]int16, 2*frames)
	for i := range frames {
		l := int16(binary.LittleEndian.Uint16(b[i*2*channels:]))
		r := l
		if channels > 1 {
			r = int16(binary.LittleEndian.Uint16(b[i*2*channels+2:]))
		}
		out[2*i], out[2*i+1] = l, r
	}
	return out
}

func mp3Decoder(r io.Reader) (decoder, error) {
	d, err := gomp3.NewDecoder(r)
	if err != nil {
		return nil, fmt.Errorf("urlplayer: mp3: %w", err)
	}
	return &s16{r: d, channels: 2, hz: d.SampleRate()}, nil
}

func aacDecoder(r io.Reader) (decoder, error) {
	d, err := aacpcm.NewDecoder(r)
	if err != nil {
		return nil, fmt.Errorf("urlplayer: aac: %w", err)
	}
	info := d.Info()
	return &s16{r: d, channels: info.Channels, hz: info.SampleRate}, nil
}

type wav struct {
	s16
	data int64
}

func wavDecoder(r *bufio.Reader) (decoder, error) {
	f, err := wavHeader(r)
	if err != nil {
		return nil, err
	}
	return &wav{s16: s16{r: r, channels: f.channels, hz: f.rate}, data: f.data}, nil
}

type wavFormat struct {
	channels int
	rate     int
	data     int64
}

func wavHeader(r *bufio.Reader) (wavFormat, error) {
	var f wavFormat
	riff := make([]byte, 12)
	if _, err := io.ReadFull(r, riff); err != nil {
		return f, fmt.Errorf("urlplayer: wav: %w", err)
	}
	f.data = 12
	for {
		hdr := make([]byte, 8)
		if _, err := io.ReadFull(r, hdr); err != nil {
			return f, fmt.Errorf("urlplayer: wav: %w", err)
		}
		size := int64(binary.LittleEndian.Uint32(hdr[4:]))
		f.data += 8
		switch string(hdr[:4]) {
		case "fmt ":
			body := make([]byte, size+size%2)
			if _, err := io.ReadFull(r, body); err != nil {
				return f, fmt.Errorf("urlplayer: wav: %w", err)
			}
			f.data += int64(len(body))
			if len(body) < 16 || binary.LittleEndian.Uint16(body) != 1 ||
				binary.LittleEndian.Uint16(body[14:]) != 16 {
				return f, fmt.Errorf("%w: wav that is not 16-bit PCM", errUnplayable)
			}
			f.channels = int(binary.LittleEndian.Uint16(body[2:]))
			f.rate = int(binary.LittleEndian.Uint32(body[4:]))
		case "data":
			if f.channels == 0 {
				return f, fmt.Errorf("urlplayer: wav: data before fmt")
			}
			return f, nil
		default:
			skip := size + size%2
			if _, err := r.Discard(int(skip)); err != nil {
				return f, fmt.Errorf("urlplayer: wav: %w", err)
			}
			f.data += skip
		}
	}
}

type flacFrames struct {
	s     *flac.Stream
	shift int
}

func flacDecoder(r io.Reader) (decoder, error) {
	s, err := flac.New(r)
	if err != nil {
		return nil, fmt.Errorf("urlplayer: flac: %w", err)
	}
	return &flacFrames{s: s, shift: int(s.Info.BitsPerSample) - 16}, nil
}

func (d *flacFrames) rate() int { return int(d.s.Info.SampleRate) }

func (d *flacFrames) next() ([]int16, error) {
	f, err := d.s.ParseNext()
	if err != nil {
		return nil, err
	}
	n := int(f.BlockSize)
	out := make([]int16, 2*n)
	right := min(1, len(f.Subframes)-1)
	for i := range n {
		l, r := f.Subframes[0].Samples[i], f.Subframes[right].Samples[i]
		if d.shift > 0 {
			l, r = l>>d.shift, r>>d.shift
		} else if d.shift < 0 {
			l, r = l<<-d.shift, r<<-d.shift
		}
		out[2*i], out[2*i+1] = int16(l), int16(r)
	}
	return out, nil
}

type matroska struct {
	demux *webm.Reader
	from  int64
	hz    int
	frame func(data []byte) ([]int16, error)
}

func matroskaDecoder(ctx context.Context, r io.Reader) (decoder, error) {
	demux := webm.NewReader(r)
	tr, err := demux.Track()
	if err != nil {
		return nil, fmt.Errorf("urlplayer: matroska: %w", err)
	}
	m := &matroska{demux: demux}
	slog.Debug("the url player found matroska sound", "codec", tr.Codec, "rate", tr.Rate,
		"channels", tr.Channels, "private", len(tr.Private))
	m.frame, m.hz, err = audioFrames(ctx, tr.Codec, tr.Private, int(tr.Rate))
	if err != nil {
		return nil, err
	}
	return m, nil
}

func opusFrames() (func([]byte) ([]int16, error), int, error) {
	dec, err := opus.NewDecoderWithOutput(opusRate, 2)
	if err != nil {
		return nil, 0, fmt.Errorf("urlplayer: opus: %w", err)
	}
	buf := make([]int16, opusFrame*2)
	return func(data []byte) ([]int16, error) {
		got, err := dec.DecodeToInt16(data, buf)
		if err != nil {
			return nil, err
		}
		return append([]int16(nil), buf[:got*2]...), nil
	}, opusRate, nil
}

func aacFrames(config []byte) (func([]byte) ([]int16, error), int, error) {
	dec, err := aacpcm.NewRawDecoder(config)
	if err != nil {
		return nil, 0, fmt.Errorf("urlplayer: aac: %w", err)
	}
	var buf []byte
	channels := dec.Channels()
	return func(data []byte) ([]int16, error) {
		out, _, err := dec.DecodeFrame(buf[:0], data)
		if err != nil {
			return nil, err
		}
		buf = out
		return stereo16(out, channels), nil
	}, dec.SampleRate(), nil
}

func (m *matroska) rate() int { return m.hz }

func (m *matroska) paired(video uint64) func() (sample, error) {
	return func() (sample, error) {
		f, err := m.demux.NextPaired()
		return sample{
			video: f.Track == video,
			data:  f.Data,
			at:    time.Duration(f.Time),
			key:   f.Key,
		}, err
	}
}

func (m *matroska) next() ([]int16, error) {
	for {
		f, err := m.demux.Next()
		if err != nil {
			return nil, err
		}
		if f.Time < m.from {
			continue
		}
		out, err := m.frame(f.Data)
		if err != nil {
			continue
		}
		return out, nil
	}
}
