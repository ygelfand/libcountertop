package webm

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/at-wat/ebml-go"
)

const (
	idSegment       = 0x18538067
	idTracks        = 0x1654AE6B
	idCues          = 0x1C53BB6B
	idInfo          = 0x1549A966
	idTimecodeScale = 0x2AD7B1
	idCluster       = 0x1F43B675
	idTimecode      = 0xE7
	idSimpleBlock   = 0xA3
	idBlockGroup    = 0xA0
)

const unknown = -1

type Track struct {
	Number   uint64
	Codec    string
	Private  []byte
	Rate     float64
	Channels int

	Width, Height int
}

type Frame struct {
	Track uint64
	Time  int64
	Data  []byte
	Key   bool
}

type Reader struct {
	r *bufio.Reader

	kind    byte
	scale   int64
	cluster int64
	track   *Track
	other   *Track
	pending []Frame

	src       *counter
	segStart  int64
	cues      []cue
	clustered bool
}

type cue struct{ time, pos int64 }

type counter struct {
	r io.Reader
	n int64
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func NewReader(r io.Reader) *Reader {
	c := &counter{r: r}
	return &Reader{r: bufio.NewReaderSize(c, 64*1024), src: c, kind: 'A', scale: 1_000_000}
}

func NewVideoReader(r io.Reader) *Reader {
	v := NewReader(r)
	v.kind = 'V'
	return v
}

func (r *Reader) pos() int64 { return r.src.n - int64(r.r.Buffered()) }

func (r *Reader) Index() error {
	for len(r.cues) == 0 && !r.clustered {
		if err := r.step(); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reader) Cue(at int64) (offset, time int64, ok bool) {
	for _, c := range r.cues {
		if c.time*r.scale > at {
			break
		}
		offset, time, ok = r.segStart+c.pos, c.time*r.scale, true
	}
	return offset, time, ok
}

func (r *Reader) Restart(src io.Reader, offset int64) {
	r.src.r, r.src.n = src, offset
	r.r.Reset(r.src)
	r.pending = nil
}

func (r *Reader) Track() (Track, error) {
	for r.track == nil {
		if err := r.step(); err != nil {
			return Track{}, err
		}
	}
	return *r.track, nil
}

func NewFeeder() *Reader {
	return &Reader{src: &counter{}, kind: 'A', scale: 1_000_000}
}

func (r *Reader) Feed(b []byte) (int, []Frame, error) {
	r.src.r, r.src.n = bytes.NewReader(b), 0
	r.r = bufio.NewReaderSize(r.src, 64*1024)
	used := 0
	for {
		if err := r.step(); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			frames := r.pending
			r.pending = nil
			return used, frames, err
		}
		used = int(r.pos())
	}
	frames := r.pending
	r.pending = nil
	return used, frames, nil
}

func (r *Reader) Tracks() []Track {
	var out []Track
	for _, t := range []*Track{r.track, r.other} {
		if t != nil {
			out = append(out, *t)
		}
	}
	return out
}

func (r *Reader) Paired() (Track, bool) {
	if r.other == nil {
		return Track{}, false
	}
	return *r.other, true
}

func (r *Reader) NextPaired() (Frame, error) {
	return r.next(func(f Frame) bool {
		return (r.track != nil && f.Track == r.track.Number) ||
			(r.other != nil && f.Track == r.other.Number)
	})
}

func (r *Reader) Next() (Frame, error) {
	return r.next(func(f Frame) bool { return r.track == nil || f.Track == r.track.Number })
}

func (r *Reader) next(want func(Frame) bool) (Frame, error) {
	for {
		for len(r.pending) > 0 {
			f := r.pending[0]
			r.pending = r.pending[1:]
			if want(f) {
				return f, nil
			}
		}
		if err := r.step(); err != nil {
			return Frame{}, err
		}
	}
}

func (r *Reader) step() error {
	id, err := r.id()
	if err != nil {
		return err
	}
	size, err := r.size()
	if err != nil {
		return noEOF(err)
	}

	switch id {
	case idSegment:
		r.segStart = r.pos()
		return nil
	case idCluster:
		r.clustered = true
		return nil
	case idInfo:
		return nil
	case idTimecodeScale:
		var v struct{ TimecodeScale uint64 }
		if err := r.element(id, size, &v); err != nil {
			return err
		}
		if v.TimecodeScale > 0 {
			r.scale = int64(v.TimecodeScale)
		}
		return nil
	case idTimecode:
		var v struct{ Timecode uint64 }
		err := r.element(id, size, &v)
		r.cluster = int64(v.Timecode)
		return err
	case idTracks:
		return r.tracks(size)
	case idCues:
		return r.readCues(size)
	case idSimpleBlock:
		body, err := r.bytes(size)
		if err != nil {
			return err
		}
		b, err := ebml.UnmarshalBlock(bytes.NewReader(body), size)
		if err != nil {
			return fmt.Errorf("webm: %w", err)
		}
		r.queue(*b, b.Keyframe)
		return nil
	case idBlockGroup:
		var v struct {
			BlockGroup struct {
				Block          ebml.Block
				ReferenceBlock []int64
			}
		}
		if err := r.element(id, size, &v); err != nil {
			return err
		}
		r.queue(v.BlockGroup.Block, len(v.BlockGroup.ReferenceBlock) == 0)
		return nil
	}
	return r.skip(size)
}

func (r *Reader) queue(b ebml.Block, key bool) {
	at := (r.cluster + int64(b.Timecode)) * r.scale
	for _, data := range b.Data {
		r.pending = append(r.pending, Frame{Track: b.TrackNumber, Time: at, Data: data, Key: key})
	}
}

type trackEntry struct {
	TrackNumber  uint64
	CodecID      string
	CodecPrivate []byte
	Audio        struct {
		SamplingFrequency float64
		Channels          uint64
	}
	Video struct {
		PixelWidth  uint64
		PixelHeight uint64
	}
}

func (r *Reader) tracks(size int64) error {
	var v struct {
		Tracks struct{ TrackEntry []trackEntry }
	}
	if err := r.element(idTracks, size, &v); err != nil {
		return err
	}
	for _, e := range v.Tracks.TrackEntry {
		if e.CodecID == "" {
			continue
		}
		t := &Track{
			Number:   e.TrackNumber,
			Codec:    e.CodecID,
			Private:  e.CodecPrivate,
			Rate:     e.Audio.SamplingFrequency,
			Channels: int(e.Audio.Channels),
			Width:    int(e.Video.PixelWidth),
			Height:   int(e.Video.PixelHeight),
		}
		switch {
		case r.track == nil && t.Codec[0] == r.kind:
			r.track = t
		case r.other == nil && t.Codec[0] != r.kind:
			r.other = t
		}
	}
	return nil
}

func (r *Reader) readCues(size int64) error {
	var v struct {
		Cues struct {
			CuePoint []struct {
				CueTime           uint64
				CueTrackPositions []struct{ CueClusterPosition uint64 }
			}
		}
	}
	if err := r.element(idCues, size, &v); err != nil {
		return err
	}
	for _, p := range v.Cues.CuePoint {
		if len(p.CueTrackPositions) == 0 {
			continue
		}
		r.cues = append(r.cues, cue{
			time: int64(p.CueTime),
			pos:  int64(p.CueTrackPositions[0].CueClusterPosition),
		})
	}
	return nil
}

func (r *Reader) element(id uint64, size int64, v any) error {
	body, err := r.bytes(size)
	if err != nil {
		return err
	}
	var whole bytes.Buffer
	whole.Grow(len(body) + 12)
	writeID(&whole, id)
	writeSize(&whole, len(body))
	whole.Write(body)
	if err := ebml.Unmarshal(&whole, v, ebml.WithIgnoreUnknown(true)); err != nil {
		return fmt.Errorf("webm: %w", err)
	}
	return nil
}

func writeID(w *bytes.Buffer, id uint64) {
	n := 1
	for id>>(8*n) != 0 {
		n++
	}
	for i := n - 1; i >= 0; i-- {
		w.WriteByte(byte(id >> (8 * i)))
	}
}

func writeSize(w *bytes.Buffer, size int) {
	w.WriteByte(0x01)
	for i := 6; i >= 0; i-- {
		w.WriteByte(byte(size >> (8 * i)))
	}
}

func (r *Reader) id() (uint64, error) {
	first, err := r.r.ReadByte()
	if err != nil {
		return 0, err
	}
	n := width(first)
	if n == 0 || n > 4 {
		return 0, fmt.Errorf("webm: an element id of %d bytes", n)
	}
	id := uint64(first)
	for i := 1; i < n; i++ {
		b, err := r.r.ReadByte()
		if err != nil {
			return 0, noEOF(err)
		}
		id = id<<8 | uint64(b)
	}
	return id, nil
}

func (r *Reader) size() (int64, error) {
	first, err := r.r.ReadByte()
	if err != nil {
		return 0, err
	}
	n := width(first)
	if n == 0 {
		return 0, fmt.Errorf("webm: an element size with no length marker")
	}
	v := uint64(first) & (0xFF >> n)
	all := v == 0xFF>>n
	for i := 1; i < n; i++ {
		b, err := r.r.ReadByte()
		if err != nil {
			return 0, err
		}
		v = v<<8 | uint64(b)
		all = all && b == 0xFF
	}
	if all {
		return unknown, nil
	}
	return int64(v), nil
}

func (r *Reader) bytes(size int64) ([]byte, error) {
	if size < 0 || size > 16<<20 {
		return nil, fmt.Errorf("webm: an element of %d bytes", size)
	}
	b := make([]byte, size)
	_, err := io.ReadFull(r.r, b)
	return b, noEOF(err)
}

func (r *Reader) skip(size int64) error {
	if size == unknown {
		return nil
	}
	_, err := r.r.Discard(int(size))
	return noEOF(err)
}

func width(first byte) int {
	for i := range 8 {
		if first&(0x80>>i) != 0 {
			return i + 1
		}
	}
	return 0
}

func noEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}
