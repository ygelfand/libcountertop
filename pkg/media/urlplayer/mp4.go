package urlplayer

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/pmp4"

	"github.com/ygelfand/libcountertop/pkg/fetch"
)

type mp4Track struct {
	samples []*pmp4.Sample
	scale   int64
	offset  int64
	video   bool
	i       int
	dts     int64
}

func newMP4Track(t *pmp4.Track, video bool) *mp4Track {
	return &mp4Track{
		samples: t.Samples,
		scale:   int64(t.TimeScale),
		offset:  int64(t.TimeOffset),
		video:   video,
	}
}

func (m *mp4Track) time(ticks int64) time.Duration {
	return time.Duration(ticks * int64(time.Second) / m.scale)
}

func (m *mp4Track) done() bool { return m.i >= len(m.samples) }

func (m *mp4Track) length() time.Duration {
	var ticks int64
	for _, s := range m.samples {
		ticks += int64(s.Duration)
	}
	return m.time(ticks)
}

func (m *mp4Track) seek(to time.Duration) {
	var dts, keyDTS int64
	key := 0
	for i, s := range m.samples {
		if m.time(dts) >= to {
			break
		}
		if !s.IsNonSyncSample {
			key, keyDTS = i, dts
		}
		dts += int64(s.Duration)
		m.i, m.dts = i+1, dts
	}
	if m.video {
		m.i, m.dts = key, keyDTS
	}
}

func (m *mp4Track) take() (sample, error) {
	s := m.samples[m.i]
	data, err := s.GetPayload()
	if err != nil {
		return sample{}, err
	}
	out := sample{
		video: m.video,
		data:  data,
		at:    m.time(m.dts + int64(s.PTSOffset) + m.offset),
		key:   !s.IsNonSyncSample,
	}
	m.i++
	m.dts += int64(s.Duration)
	return out, nil
}

func mp4Sound(c codecs.Codec) (string, []byte, int, error) {
	switch c := c.(type) {
	case *codecs.MPEG4Audio:
		config, err := c.Config.Marshal()
		if err != nil {
			return "", nil, 0, fmt.Errorf("urlplayer: mp4: %w", err)
		}
		return codecAACName, config, c.Config.SampleRate, nil
	case *codecs.Opus:
		return codecOpusName, nil, opusRate, nil
	case *codecs.MPEG1Audio:
		return codecMP3Name, nil, c.SampleRate, nil
	}
	return "", nil, 0, fmt.Errorf("%w: mp4 sound %T", errUnplayable, c)
}

func interleaved(tracks []*mp4Track) func() (sample, error) {
	return func() (sample, error) {
		var next *mp4Track
		for _, t := range tracks {
			if !t.done() && (next == nil || t.time(t.dts) < next.time(next.dts)) {
				next = t
			}
		}
		if next == nil {
			return sample{}, io.EOF
		}
		return next.take()
	}
}

type span struct {
	off  int64
	data []byte
}

const (
	mp4Window = 16 << 20
	mp4Reach  = 4 << 20
)

type mp4File struct {
	r      *fetch.Ranged
	pos    int64
	held   []span
	recent span
}

func readMP4(r *fetch.Ranged) (*mp4File, error) {
	f := &mp4File{r: r}
	size := r.Size()
	for off := int64(0); size <= 0 || off < size; {
		if _, err := r.Seek(off, io.SeekStart); err != nil {
			return nil, err
		}
		head := make([]byte, 16)
		if _, err := io.ReadFull(r, head[:8]); err != nil {
			if errors.Is(err, io.EOF) && size <= 0 {
				break
			}
			return nil, err
		}
		n, kind, header := int64(binary.BigEndian.Uint32(head)), string(head[4:8]), int64(8)
		switch n {
		case 0:
			n = size - off
		case 1:
			if _, err := io.ReadFull(r, head[8:]); err != nil {
				return nil, err
			}
			n, header = int64(binary.BigEndian.Uint64(head[8:])), 16
		}
		if n < header {
			return nil, fmt.Errorf("urlplayer: mp4: a %q box of %d bytes", kind, n)
		}
		if kind != "mdat" {
			if n > 64<<20 {
				return nil, fmt.Errorf("urlplayer: mp4: a %q box of %d bytes", kind, n)
			}
			box := make([]byte, n)
			copy(box, head[:header])
			if _, err := io.ReadFull(r, box[header:]); err != nil {
				return nil, err
			}
			f.held = append(f.held, span{off: off, data: box})
		}
		off += n
	}
	return f, nil
}

func (s span) holds(at int64) bool { return at >= s.off && at < s.off+int64(len(s.data)) }

func (s span) end() int64 { return s.off + int64(len(s.data)) }

func (f *mp4File) Read(p []byte) (int, error) {
	for _, s := range f.held {
		if s.holds(f.pos) {
			n := copy(p, s.data[f.pos-s.off:])
			f.pos += int64(n)
			return n, nil
		}
	}
	if !f.recent.holds(f.pos) {
		if err := f.fill(f.pos + int64(len(p))); err != nil {
			return 0, err
		}
		if !f.recent.holds(f.pos) {
			return 0, io.EOF
		}
	}
	n := copy(p, f.recent.data[f.pos-f.recent.off:])
	f.pos += int64(n)
	return n, nil
}

func (f *mp4File) fill(upto int64) error {
	if len(f.recent.data) == 0 || f.pos < f.recent.off || f.pos > f.recent.end()+mp4Reach {
		if _, err := f.r.Seek(f.pos, io.SeekStart); err != nil {
			return err
		}
		f.recent = span{off: f.pos}
	}
	for f.recent.end() < upto {
		buf := make([]byte, upto-f.recent.end())
		n, err := f.r.Read(buf)
		f.recent.data = append(f.recent.data, buf[:n]...)
		if err != nil {
			if n > 0 || f.recent.holds(f.pos) {
				break
			}
			return err
		}
	}
	if over := len(f.recent.data) - mp4Window; over > 0 {
		f.recent.data = f.recent.data[over:]
		f.recent.off += int64(over)
	}
	return nil
}

func (f *mp4File) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekCurrent:
		offset += f.pos
	case io.SeekEnd:
		offset += f.r.Size()
	}
	if offset < 0 {
		return f.pos, fmt.Errorf("urlplayer: mp4: seeking to %d", offset)
	}
	f.pos = offset
	return offset, nil
}

func (t *track) openMP4(ctx context.Context, s stream) error {
	if s.ranged == nil {
		return fmt.Errorf("%w: an MP4 from a server that refuses range requests", errUnplayable)
	}
	file, err := readMP4(s.ranged)
	if err != nil {
		return err
	}
	var p pmp4.Presentation
	if err := p.Unmarshal(file); err != nil {
		return fmt.Errorf("urlplayer: mp4: %w", err)
	}

	var (
		frame  func([]byte) ([]int16, error)
		hz     int
		tracks []*mp4Track
		pic    *picture
	)
	for _, tr := range p.Tracks {
		switch c := tr.Codec.(type) {
		case *codecs.MPEG4Audio, *codecs.Opus, *codecs.MPEG1Audio:
			if frame != nil {
				continue
			}
			name, private, rate, err := mp4Sound(c)
			if err != nil {
				return err
			}
			if frame, hz, err = audioFrames(ctx, name, private, rate); err != nil {
				return err
			}
			tracks = append(tracks, newMP4Track(tr, false))
		case *codecs.H264:
			if !t.screen || pic != nil {
				continue
			}
			var sps h264.SPS
			if err := sps.Unmarshal(c.SPS); err != nil {
				continue
			}
			pic = newPicture(sps.Width(), sps.Height(), avcConfig{
				lengthSize: 4, sps: [][]byte{c.SPS}, pps: [][]byte{c.PPS},
			})
			tracks = append(tracks, newMP4Track(tr, true))
		}
	}
	if frame == nil {
		return fmt.Errorf("%w: an MP4 without AAC or Opus sound", errUnplayable)
	}
	for _, tr := range tracks {
		if !tr.video {
			t.length = tr.length()
		}
		tr.seek(t.at)
	}
	if pic != nil {
		t.picture = pic.playback()
	}
	t.seekable = true
	return t.use(startMuxed(ctx, interleaved(tracks), frame, hz, t.at, pic))
}
