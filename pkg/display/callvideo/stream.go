package callvideo

import (
	"errors"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h265"

	"github.com/ygelfand/libcountertop/pkg/display/surface"
	"github.com/ygelfand/libcountertop/pkg/media/call"
	"github.com/ygelfand/libcountertop/pkg/media/rtc"
)

type Stream struct{ l *layer }

func NewStream(d Display, codec, id uint32, z int) *Stream {
	return &Stream{l: &layer{display: d, codec: codec, id: id, z: z, box: fullScreen}}
}

func (s *Stream) Show(f rtc.Frame) {
	if s.Refused() {
		return
	}
	if f.Key {
		if sz, ok := sized(s.l.codec, f.Data); ok {
			s.l.mu.Lock()
			moved := sz != s.l.size
			s.l.mu.Unlock()
			if moved {
				s.l.update(sz, false)
			}
		}
	}
	s.l.Show(f)
}

func (s *Stream) Refused() bool {
	st := s.l.stats()
	return st.Shown == 0 && errors.Is(st.Failed, surface.Broken)
}

func (s *Stream) Reorient() { s.l.reorient() }

func (s *Stream) Stats() call.LayerStats { return s.l.stats() }

func (s *Stream) Close() { s.l.close() }

func sized(codec uint32, annexB []byte) (Size, bool) {
	if codec == surface.HEVC {
		return sizedHEVC(annexB)
	}
	var au h264.AnnexB
	if err := au.Unmarshal(annexB); err != nil {
		return Size{}, false
	}
	for _, n := range au {
		if len(n) == 0 || h264.NALUType(n[0]&0x1f) != h264.NALUTypeSPS {
			continue
		}
		var sps h264.SPS
		if err := sps.Unmarshal(n); err != nil {
			return Size{}, false
		}
		return Size{Width: sps.Width(), Height: sps.Height()}, true
	}
	return Size{}, false
}

func sizedHEVC(annexB []byte) (Size, bool) {
	var au h264.AnnexB
	if err := au.Unmarshal(annexB); err != nil {
		return Size{}, false
	}
	for _, n := range au {
		if len(n) < 2 || h265.NALUType((n[0]>>1)&0x3f) != h265.NALUType_SPS_NUT {
			continue
		}
		var sps h265.SPS
		if err := sps.Unmarshal(n); err != nil {
			return Size{}, false
		}
		return Size{Width: sps.Width(), Height: sps.Height()}, true
	}
	return Size{}, false
}
