package urlplayer

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"mime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"

	"github.com/ygelfand/libcountertop/pkg/media/cenc"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
	"github.com/ygelfand/libcountertop/pkg/media/webm"
)

const (
	gapAllowed = 100 * time.Millisecond
	lastFrame  = 20 * time.Millisecond
	// Chromium's SourceBufferStream kSeekToStartFudgeRoom.
	seekToStartFudge = time.Second
)

var errNoLane = errors.New("urlplayer: no such source buffer")

type Range struct{ Start, End time.Duration }

type Opened struct{ Width, Height int }

type Feed struct {
	mu       sync.Mutex
	changed  *sync.Cond
	lanes    map[int]*lane
	duration time.Duration
	ended    bool
	keys     func(ctx context.Context) (Keys, error)
}

type Keys interface {
	Wait(ctx context.Context, key [16]byte) error
	OpenAudio(t cenc.Track) error
	Picture(ctx context.Context, t cenc.Track, s cenc.Sample) error
	Sound(t cenc.Track, s cenc.Sample) error
	Begin(at time.Duration)
	Read(pcm []int16) (int, error)
	Video(width, height int) playback.Picture
	Origin() (time.Duration, bool)
	Fail(err error)
	Close()
}

type sound struct {
	name    string
	private []byte
	rate    int
}

type lane struct {
	held    []byte
	ftyp    []byte
	cenc    *cenc.Track
	sealed  bool
	fresh   [][]byte
	mp4     *fmp4.InitTrack
	webm    *webm.Reader
	inited  bool
	video   bool
	width   int
	height  int
	sound   sound
	config  *nalConfig
	samples []fed
}

type fed struct {
	sample
	dts time.Duration
	end time.Duration
}

func NewFeed() *Feed {
	f := &Feed{lanes: map[int]*lane{}}
	f.changed = sync.NewCond(&f.mu)
	return f
}

func (f *Feed) Add(id int, kind string) {
	l := &lane{}
	if base, _, err := mime.ParseMediaType(kind); err == nil && strings.HasSuffix(base, "/webm") {
		l.webm = webm.NewFeeder()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lanes[id] = l
	f.changed.Broadcast()
}

func (f *Feed) Drop(id int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.lanes, id)
	f.changed.Broadcast()
}

func (f *Feed) Abort(id int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if l := f.lanes[id]; l != nil {
		l.held = nil
	}
}

func (f *Feed) SetDuration(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.duration = d
}

func (f *Feed) Duration() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.duration
}

func (f *Feed) Protect(open func(ctx context.Context) (Keys, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = open
	f.changed.Broadcast()
}

func (f *Feed) InitData(id int) [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	l := f.lanes[id]
	if l == nil {
		return nil
	}
	out := l.fresh
	l.fresh = nil
	return out
}

func (f *Feed) End() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ended = true
	f.changed.Broadcast()
}

func (f *Feed) Append(id int, data []byte, offset time.Duration) ([]Range, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l := f.lanes[id]
	if l == nil {
		return nil, errNoLane
	}
	f.ended = false
	l.held = append(l.held, data...)
	err := l.parse(offset)
	f.changed.Broadcast()
	return l.buffered(), err
}

func (f *Feed) Remove(id int, from, until time.Duration) ([]Range, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l := f.lanes[id]
	if l == nil {
		return nil, errNoLane
	}
	l.samples = slices.DeleteFunc(
		l.samples,
		func(s fed) bool { return s.at >= from && s.at < until },
	)
	f.changed.Broadcast()
	return l.buffered(), nil
}

func (f *Feed) Opened() (Opened, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.openedLocked()
}

func (f *Feed) openedLocked() (Opened, bool) {
	var o Opened
	if len(f.lanes) == 0 {
		return o, false
	}
	for _, l := range f.lanes {
		if !l.inited {
			return o, false
		}
		if l.video {
			o.Width, o.Height = l.width, l.height
		}
	}
	return o, true
}

func (f *Feed) ready(ctx context.Context) (sound, video int, err error) {
	stop := context.AfterFunc(ctx, f.wake)
	defer stop()
	f.mu.Lock()
	defer f.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		if _, ok := f.openedLocked(); ok {
			break
		}
		f.changed.Wait()
	}
	sound, video = -1, -1
	for id, l := range f.lanes {
		switch {
		case l.video && video < 0:
			video = id
		case !l.video && sound < 0:
			sound = id
		}
	}
	return sound, video, nil
}

func (f *Feed) lane(id int) lane {
	f.mu.Lock()
	defer f.mu.Unlock()
	if l := f.lanes[id]; l != nil {
		return lane{
			video:  l.video,
			width:  l.width,
			height: l.height,
			sound:  l.sound,
			config: l.config,
			cenc:   l.cenc,
			sealed: l.sealed,
		}
	}
	return lane{}
}

func (f *Feed) opener() func(ctx context.Context) (Keys, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.keys
}

func (f *Feed) wake() {
	f.mu.Lock()
	f.changed.Broadcast()
	f.mu.Unlock()
}

type cursor struct {
	id      int
	started bool
	dts     time.Duration
	expect  time.Duration
}

func (f *Feed) reader(ctx context.Context, from time.Duration, ids ...int) func() (sample, error) {
	cursors := make([]*cursor, len(ids))
	for i, id := range ids {
		cursors[i] = &cursor{id: id}
	}
	return func() (sample, error) {
		stop := context.AfterFunc(ctx, f.wake)
		defer stop()
		f.mu.Lock()
		defer f.mu.Unlock()
		for {
			if err := ctx.Err(); err != nil {
				return sample{}, err
			}
			s, ok, err := f.pick(cursors, from)
			if ok || err != nil {
				return s.sample, err
			}
			f.changed.Wait()
		}
	}
}

func (f *Feed) pick(cursors []*cursor, from time.Duration) (fed, bool, error) {
	var best *cursor
	var next fed
	for _, c := range cursors {
		l := f.lanes[c.id]
		if l == nil {
			continue
		}
		if !c.started {
			at, ok := l.start(from)
			if !ok {
				if f.ended {
					continue
				}
				return fed{}, false, nil
			}
			c.started, c.dts = true, at
		}
		i, ok := l.after(c.dts)
		if ok && c.expect > 0 && l.samples[i].dts > c.expect+gapAllowed {
			ok = false
		}
		if !ok {
			if f.ended {
				continue
			}
			return fed{}, false, nil
		}
		if best == nil || l.samples[i].dts < next.dts {
			best, next = c, l.samples[i]
		}
	}
	if best == nil {
		return fed{}, false, io.EOF
	}
	best.dts, best.expect = next.dts, next.dts+(next.end-next.at)
	return next, true, nil
}

func (f *Feed) keyframe(at time.Duration) (time.Duration, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.lanes {
		if !l.video {
			continue
		}
		i, ok := l.key(at)
		if !ok {
			return 0, false
		}
		return l.samples[i].at, true
	}
	return 0, false
}

func (l *lane) start(from time.Duration) (time.Duration, bool) {
	i, ok := l.key(from)
	if !ok {
		return 0, false
	}
	return l.samples[i].dts - 1, true
}

func (l *lane) key(from time.Duration) (int, bool) {
	held := slices.ContainsFunc(l.samples, func(s fed) bool { return s.at <= from && s.end > from })
	if !held {
		next := slices.IndexFunc(l.samples, func(s fed) bool { return s.key && s.at > from })
		if next < 0 || l.samples[next].at-from > seekToStartFudge {
			return 0, false
		}
		return next, true
	}
	key := -1
	for i, s := range l.samples {
		if s.at > from {
			break
		}
		if s.key {
			key = i
		}
	}
	if key < 0 {
		return 0, false
	}
	return key, true
}

func (l *lane) after(dts time.Duration) (int, bool) {
	i, _ := slices.BinarySearchFunc(l.samples, dts+1, func(s fed, t time.Duration) int {
		return compare(s.dts, t)
	})
	return i, i < len(l.samples)
}

func compare(a, b time.Duration) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func (l *lane) buffered() []Range {
	var out []Range
	var last time.Duration
	for i, s := range l.samples {
		if i == 0 || s.dts > last+gapAllowed {
			out = append(out, Range{Start: s.at, End: s.end})
		}
		r := &out[len(out)-1]
		r.Start, r.End = min(r.Start, s.at), max(r.End, s.end)
		last = s.dts + (s.end - s.at)
	}
	return out
}

func (l *lane) insert(added []fed) {
	if len(added) == 0 {
		return
	}
	first, last := added[0].dts, added[len(added)-1].dts
	l.samples = slices.DeleteFunc(
		l.samples,
		func(s fed) bool { return s.dts >= first && s.dts <= last },
	)
	i, _ := slices.BinarySearchFunc(l.samples, first, func(s fed, t time.Duration) int {
		return compare(s.dts, t)
	})
	l.samples = slices.Insert(l.samples, i, added...)
}

func (l *lane) parse(offset time.Duration) error {
	if l.webm != nil {
		return l.parseWebM(offset)
	}
	for {
		n, kind, ok := box(l.held)
		if !ok {
			return nil
		}
		switch kind {
		case "ftyp":
			l.ftyp = slices.Clone(l.held[:n])
		case "moov":
			if err := l.init(l.held[:n]); err != nil {
				l.held = l.held[n:]
				return err
			}
		case "moof":
			m, next, ok := box(l.held[n:])
			if !ok {
				return nil
			}
			if next != "mdat" {
				l.held = l.held[n:]
				return fmt.Errorf("urlplayer: media source: a moof followed by %q", next)
			}
			if err := l.part(l.held[:n+m], offset); err != nil {
				l.held = l.held[n+m:]
				return err
			}
			n += m
		}
		l.held = l.held[n:]
	}
}

func box(b []byte) (int, string, bool) {
	if len(b) < 8 {
		return 0, "", false
	}
	n, header := uint64(binary.BigEndian.Uint32(b)), uint64(8)
	if n == 1 {
		if len(b) < 16 {
			return 0, "", false
		}
		n, header = binary.BigEndian.Uint64(b[8:]), 16
	}
	if n < header || n > uint64(len(b)) {
		return 0, "", false
	}
	return int(n), string(b[4:8]), true
}

func (l *lane) init(moov []byte) error {
	whole := append(slices.Clone(l.ftyp), moov...)
	if t, err := cenc.ParseInit(whole); err == nil {
		l.cenc = &t
		l.fresh = append(l.fresh, t.PSSH...)
		if t.Scheme != cenc.SchemeNone {
			return l.sealedInit(t)
		}
	}
	var i fmp4.Init
	if err := i.Unmarshal(bytes.NewReader(whole)); err != nil {
		return fmt.Errorf("urlplayer: media source: %w", err)
	}
	if len(i.Tracks) == 0 {
		return errors.New("urlplayer: media source: an init segment without tracks")
	}
	l.mp4 = i.Tracks[0]
	switch c := l.mp4.Codec.(type) {
	case *codecs.H264:
		var sps h264.SPS
		if err := sps.Unmarshal(c.SPS); err != nil {
			return fmt.Errorf("urlplayer: media source: %w", err)
		}
		l.video, l.width, l.height = true, sps.Width(), sps.Height()
		config := avcConfig(4, [][]byte{c.SPS}, [][]byte{c.PPS})
		l.config = &config
	case *codecs.H265:
		config, w, h, err := hevcConfig(c.VPS, c.SPS, c.PPS)
		if err != nil {
			return fmt.Errorf("urlplayer: media source: %w", err)
		}
		l.video, l.width, l.height, l.config = true, w, h, &config
	default:
		if c.IsVideo() {
			return fmt.Errorf("%w: media source video %T", errUnplayable, c)
		}
		name, private, rate, err := mp4Sound(c)
		if err != nil {
			return err
		}
		l.sound = sound{name: name, private: private, rate: rate}
	}
	l.inited = true
	return nil
}

func (l *lane) sealedInit(t cenc.Track) error {
	switch t.Format {
	case "avc1", "avc3":
		l.video, l.width, l.height = true, t.Width, t.Height
		config := avcConfig(t.LengthSize, t.SPS, t.PPS)
		l.config = &config
	case "mp4a":
		l.sound = sound{name: codecAACName, private: t.Config, rate: t.Rate}
	default:
		return fmt.Errorf("%w: protected media source %s", errUnplayable, t.Format)
	}
	l.sealed, l.inited = true, true
	return nil
}

func (l *lane) sealedPart(data []byte, offset time.Duration) error {
	samples, pssh, err := cenc.ParseFragment(*l.cenc, data)
	if err != nil {
		return fmt.Errorf("urlplayer: media source: %w", err)
	}
	l.fresh = append(l.fresh, pssh...)
	added := make([]fed, 0, len(samples))
	var dts time.Duration
	for i, s := range samples {
		s.At += offset
		if i == 0 {
			dts = s.At
		}
		cs := s
		added = append(added, fed{
			sample: sample{
				video: l.video, data: s.Data, at: s.At, key: s.Key, config: l.config, sealed: &cs,
				track: l.cenc,
			},
			dts: dts,
			end: s.At + s.Duration,
		})
		dts += max(s.Duration, 1)
	}
	l.insert(added)
	return nil
}

func (l *lane) part(data []byte, offset time.Duration) error {
	if l.sealed {
		return l.sealedPart(data, offset)
	}
	if l.mp4 == nil {
		return errors.New("urlplayer: media source: a media segment before its init segment")
	}
	var parts fmp4.Parts
	if err := parts.Unmarshal(data); err != nil {
		return fmt.Errorf("urlplayer: media source: %w", err)
	}
	scale := int64(l.mp4.TimeScale)
	if scale == 0 {
		return errors.New("urlplayer: media source: a track without a time scale")
	}
	at := func(ticks int64) time.Duration {
		return time.Duration(ticks*int64(time.Second)/scale) + offset
	}
	var added []fed
	for _, p := range parts {
		for _, pt := range p.Tracks {
			if pt.ID != l.mp4.ID {
				continue
			}
			dts := int64(pt.BaseTime)
			for _, s := range pt.Samples {
				pts := dts + int64(s.PTSOffset)
				key := !s.IsNonSyncSample
				if l.config != nil {
					key = l.config.key(s.Payload)
				}
				added = append(added, fed{
					sample: sample{
						video: l.video, data: s.Payload, at: at(pts), key: key, config: l.config,
					},
					dts: at(dts),
					end: at(pts + int64(s.Duration)),
				})
				dts += int64(s.Duration)
			}
		}
	}
	l.insert(added)
	return nil
}

func (l *lane) parseWebM(offset time.Duration) error {
	used, frames, err := l.webm.Feed(l.held)
	l.held = l.held[used:]
	if err != nil {
		l.held = nil
		return fmt.Errorf("urlplayer: media source: %w", err)
	}
	if !l.inited {
		tracks := l.webm.Tracks()
		if len(tracks) == 0 {
			return nil
		}
		if err := l.useWebM(tracks[0]); err != nil {
			return err
		}
	}
	added := make([]fed, 0, len(frames))
	for _, fr := range frames {
		at := time.Duration(fr.Time) + offset
		dts := at
		if n := len(added); n > 0 && dts <= added[n-1].dts {
			dts = added[n-1].dts + 1
		}
		key := fr.Key || !l.video
		if l.config != nil {
			key = l.config.key(fr.Data)
		}
		added = append(added, fed{
			sample: sample{video: l.video, data: fr.Data, at: at, key: key, config: l.config},
			dts:    dts,
			end:    at + lastFrame,
		})
	}
	for i := range len(added) - 1 {
		if gap := added[i+1].at - added[i].at; gap > 0 {
			added[i].end = added[i].at + gap
		}
	}
	if n := len(added); n > 1 {
		added[n-1].end = added[n-1].at + (added[n-2].end - added[n-2].at)
	}
	l.insert(added)
	return nil
}

func (l *lane) useWebM(t webm.Track) error {
	switch {
	case t.Codec == AVC || t.Codec == HEVC:
		c, err := matroskaConfig(t.Codec, t.Private)
		if err != nil {
			return err
		}
		l.video, l.width, l.height, l.config = true, t.Width, t.Height, &c
	case strings.HasPrefix(t.Codec, "V_"):
		return fmt.Errorf("%w: media source video %s", errUnplayable, t.Codec)
	default:
		l.sound = sound{name: t.Codec, private: t.Private, rate: int(t.Rate)}
	}
	l.inited = true
	return nil
}

func (t *track) openFeed(ctx context.Context) error {
	t.format = "media source"
	soundLane, videoLane, err := t.feed.ready(ctx)
	if err != nil {
		return err
	}
	if soundLane < 0 {
		return fmt.Errorf("%w: a media source without sound", errUnplayable)
	}
	if t.feed.lane(soundLane).sealed || (videoLane >= 0 && t.feed.lane(videoLane).sealed) {
		return t.openSealed(ctx, soundLane, videoLane)
	}
	s := t.feed.lane(soundLane).sound
	frame, hz, err := audioFrames(ctx, s.name, s.private, s.rate)
	if err != nil {
		return err
	}
	lanes := []int{soundLane}
	var pic *picture
	if v := t.feed.lane(videoLane); videoLane >= 0 && v.config != nil && t.screen {
		pic = newPicture(v.width, v.height, *v.config)
		t.picture = pic.playback()
		lanes = append(lanes, videoLane)
	}
	t.seekable = true
	t.length = t.feed.Duration()
	return t.use(startMuxed(ctx, t.feed.reader(ctx, t.at, lanes...), frame, hz, t.at, pic))
}

func (p *Player) Feeds(mimeType string, width, height, rate int) bool {
	kind, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return false
	}
	switch kind {
	case "video/mp4", "audio/mp4", "video/webm", "audio/webm":
		return p.Plays(mimeType, width, height, rate)
	}
	return false
}

func (p *Player) Seals(mimeType string, width, height, rate int) bool {
	if !p.Feeds(mimeType, width, height, rate) {
		return false
	}
	_, params, _ := mime.ParseMediaType(mimeType)
	for name := range strings.SplitSeq(params["codecs"], ",") {
		if c, _ := parseCodec(strings.TrimSpace(name)); c == codecHEVC {
			return false
		}
	}
	return true
}
