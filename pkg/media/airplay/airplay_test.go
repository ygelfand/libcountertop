package airplay

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"io"
	"os"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/media/playback"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func TestMessagesRoundTrip(t *testing.T) {
	var b bytes.Buffer
	head := binary.LittleEndian.AppendUint64(nil, 1500)
	if err := writeMessage(&b, msgFrame, append(head, 0, 0, 0, 1, 0x65)); err != nil {
		t.Fatalf("writeMessage: %v", err)
	}
	m, err := readMessage(&b)
	if err != nil {
		t.Fatalf("readMessage: %v", err)
	}
	f, err := frameOf(m.payload)
	if err != nil {
		t.Fatalf("frameOf: %v", err)
	}
	if m.kind != msgFrame || f.at != 1500*time.Microsecond ||
		!bytes.Equal(f.data, []byte{0, 0, 0, 1, 0x65}) {
		t.Errorf("got kind %q at %v data %x", rune(m.kind), f.at, f.data)
	}
}

func TestAnOversizedMessageIsRefused(t *testing.T) {
	head := binary.LittleEndian.AppendUint32(nil, msgFrame)
	head = binary.LittleEndian.AppendUint32(head, maxPayload+1)
	if _, err := readMessage(bytes.NewReader(head)); err == nil {
		t.Error("an oversized message was read")
	}
}

func TestSilenceKeepsToTheClock(t *testing.T) {
	m := newMirror("mac", mirroring{}, 48000, 2, func() {})
	pcm := make([]int16, 48000*2)
	for i := range pcm {
		pcm[i] = 1
	}
	n, err := m.Read(context.Background(), pcm)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	lead := int(silenceLead * 48000 / time.Second * 2)
	if n < lead || n > lead+2*480 {
		t.Errorf("first read gave %d samples, want about %d", n, lead)
	}
	for _, v := range pcm[:n] {
		if v != 0 {
			t.Fatal("the silence was not silent")
		}
	}
	if n, _ := m.Read(context.Background(), pcm); n > 2*4800 {
		t.Errorf("an immediate second read gave %d samples", n)
	}

	m.end()
	if _, err := m.Read(context.Background(), pcm); !errors.Is(err, io.EOF) {
		t.Errorf("Read after the end = %v, want EOF", err)
	}
}

func TestFramesCountFromTheFirst(t *testing.T) {
	m := newMirror(
		"mac",
		mirroring{codec: codecH264, width: 1920, height: 1080},
		48000,
		2,
		func() {},
	)
	ctx := context.Background()
	m.push(ctx, frame{at: 5 * time.Second, data: []byte{1}})
	m.push(ctx, frame{at: 5*time.Second + 33*time.Millisecond, data: []byte{2}})

	p, err := m.Live(ctx)
	if err != nil || p.Codec != playback.H264 || p.Width != 1920 || p.Height != 1080 {
		t.Fatalf("Live = %+v, %v", p, err)
	}
	for _, want := range []time.Duration{0, 33 * time.Millisecond} {
		_, at, err := p.Next(ctx)
		if err != nil || at != want {
			t.Errorf("frame at %v (%v), want %v", at, err, want)
		}
	}
	m.end()
	if _, _, err := p.Next(ctx); !errors.Is(err, io.EOF) {
		t.Errorf("Next after the end = %v, want EOF", err)
	}
}

type output struct {
	playback.Output
	played, stopped []playback.Source
	ended           func()
	width, height   int
	restarts        int
}

func (o *output) Play(src playback.Source) { o.played = append(o.played, src) }
func (o *output) Stop(src playback.Source) { o.stopped = append(o.stopped, src) }

func screeningRig(t *testing.T) (*screening, *output, *os.File) {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	t.Cleanup(func() { read.Close(); write.Close() })
	out := &output{width: 1200, height: 1920}
	r := &Receiver{opts: Options{
		Settings: &section{cfg: schema.DefaultAirPlay()},
		Rate:     48000,
		Channels: 2,
		Size:     func() (int, int) { return out.width, out.height },
		Output: func(ended func()) playback.Output {
			out.ended = ended
			return out
		},
	}}
	s := &screening{
		r:       r,
		conn:    write,
		width:   1200,
		height:  1920,
		restart: func() { out.restarts++ },
	}
	return s, out, read
}

func mirrorMessage(w, h uint32) message { return codecMirrorMessage(codecH264, w, h) }

func codecMirrorMessage(codec, w, h uint32) message {
	p := binary.LittleEndian.AppendUint32(nil, codec)
	p = binary.LittleEndian.AppendUint32(p, w)
	p = binary.LittleEndian.AppendUint32(p, h)
	return message{kind: msgMirror, payload: p}
}

func TestAMirrorShowsTheCodecTheSenderChose(t *testing.T) {
	for _, c := range []struct {
		codec uint32
		want  uint32
	}{
		{codecH264, playback.H264},
		{codecH265, playback.HEVC},
	} {
		s, out, _ := screeningRig(t)
		ctx := context.Background()
		s.handle(ctx, codecMirrorMessage(c.codec, 1920, 1080))
		if len(out.played) != 1 {
			t.Fatalf("codec %d: played %d sources, want 1", c.codec, len(out.played))
		}
		p, _ := out.played[0].(playback.Live).Live(ctx)
		if p.Codec != c.want {
			t.Errorf("codec %d: picture %08x, want %08x", c.codec, p.Codec, c.want)
		}
	}
}

func TestAMirrorInAnUnknownCodecIsRefused(t *testing.T) {
	s, out, helper := screeningRig(t)
	s.handle(context.Background(), codecMirrorMessage(9, 1920, 1080))
	if len(out.played) != 0 {
		t.Errorf("played %d sources, want none", len(out.played))
	}
	m, err := readMessage(helper)
	if err != nil || m.kind != msgDisconnect {
		t.Fatalf("helper heard %q (%v), want a disconnect", rune(m.kind), err)
	}
}

func TestAMirrorPlaysUntilTheSenderEndsIt(t *testing.T) {
	s, out, _ := screeningRig(t)
	ctx := context.Background()
	s.handle(ctx, message{kind: msgClient, payload: []byte("Office Mac")})
	s.handle(ctx, mirrorMessage(1920, 1080))
	if len(out.played) != 1 {
		t.Fatalf("played %d sources, want 1", len(out.played))
	}
	src := out.played[0]
	if got := src.Showing().Title; got != "Office Mac" {
		t.Errorf("showing %q", got)
	}

	s.handle(ctx, message{kind: msgFrame, payload: binary.LittleEndian.AppendUint64(nil, 7)})
	p, _ := src.(playback.Live).Live(ctx)
	if _, at, err := p.Next(ctx); err != nil || at != 0 {
		t.Errorf("first frame at %v (%v)", at, err)
	}

	s.handle(ctx, message{kind: msgEnded})
	if len(out.stopped) != 1 || out.stopped[0] != src {
		t.Errorf("stopped %v, want the mirror", out.stopped)
	}
	if _, _, err := p.Next(ctx); !errors.Is(err, io.EOF) {
		t.Errorf("Next after the end = %v, want EOF", err)
	}
}

func TestAudioOnlyPlaysWithoutAPicture(t *testing.T) {
	s, out, _ := screeningRig(t)
	ctx := context.Background()
	s.handle(ctx, message{kind: msgClient, payload: []byte("Office Mac")})
	s.handle(ctx, message{kind: msgSound})
	if len(out.played) != 1 {
		t.Fatalf("played %d sources, want 1", len(out.played))
	}
	src := out.played[0]
	if _, ok := src.(playback.Pictures); ok {
		t.Error("an audio-only session offers a picture")
	}

	s.handle(ctx, audioMessage(0))
	pcm := make([]int16, 8)
	if n, err := src.Read(ctx, pcm); err != nil || n != 4 || pcm[0] != 1000 {
		t.Errorf("Read = %d %v (%v), want the 4 samples sent", n, pcm[:n], err)
	}

	s.handle(ctx, message{kind: msgEnded})
	if len(out.stopped) != 1 || out.stopped[0] != src {
		t.Errorf("stopped %v, want the audio", out.stopped)
	}
}

type section struct{ cfg schema.AirPlay }

func (s *section) Read() schema.AirPlay { return s.cfg }

func (s *section) Update(change func(*schema.AirPlay)) error {
	change(&s.cfg)
	return nil
}

func audioMessage(due time.Duration) message {
	p := binary.LittleEndian.AppendUint32(nil, 48000)
	p = binary.LittleEndian.AppendUint32(p, 2)
	p = binary.LittleEndian.AppendUint32(p, uint32(int32(due/time.Microsecond)))
	for range 4 {
		p = binary.LittleEndian.AppendUint16(p, 1000)
	}
	return message{kind: msgAudio, payload: p}
}

func TestSyncedAudioWaitsForItsTime(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		mode    schema.AirPlayAudioMode
		latency time.Duration
		want    int
	}{
		{schema.AirPlaySynced, 0, 0},
		{schema.AirPlaySynced, time.Second, 4},
		{schema.AirPlayInstant, 0, 4},
	} {
		s := newSound("mac", 48000, 2, tc.mode, func() {})
		s.latency = tc.latency
		a, err := audioOf(audioMessage(time.Second).payload)
		if err != nil {
			t.Fatal(err)
		}
		s.hear(a)
		pcm := make([]int16, 8)
		if n, err := s.Read(ctx, pcm); err != nil || n != tc.want {
			t.Errorf("%s %v: Read = %d (%v), want %d", tc.mode, tc.latency, n, err, tc.want)
		}
	}
}

func TestWithoutAScreenAMirrorIsRefused(t *testing.T) {
	s, out, helper := screeningRig(t)
	s.r.opts.Size = nil
	s.handle(context.Background(), mirrorMessage(1920, 1080))
	if len(out.played) != 0 {
		t.Errorf("played %d sources, want none", len(out.played))
	}
	m, err := readMessage(helper)
	if err != nil || m.kind != msgDisconnect {
		t.Fatalf("helper heard %q (%v), want a disconnect", rune(m.kind), err)
	}
}

func TestClosingThePictureDisconnectsTheSender(t *testing.T) {
	s, out, helper := screeningRig(t)
	ctx := context.Background()
	s.handle(ctx, mirrorMessage(1280, 720))
	src := out.played[0]

	out.ended()
	m, err := readMessage(helper)
	if err != nil || m.kind != msgDisconnect {
		t.Fatalf("helper heard %q (%v), want a disconnect", rune(m.kind), err)
	}
	pcm := make([]int16, 960)
	if _, err := src.Read(ctx, pcm); !errors.Is(err, io.EOF) {
		t.Errorf("Read after the picture closed = %v, want EOF", err)
	}
	s.handle(ctx, message{kind: msgFrame, payload: binary.LittleEndian.AppendUint64(nil, 7)})
}

func TestHeardAudioPlaysBeforeSilence(t *testing.T) {
	m := newMirror("mac", mirroring{}, 48000, 2, func() {})
	m.hear(audio{rate: 48000, channels: 1, pcm: []int16{7, 8, 9}})
	pcm := make([]int16, 960)
	n, err := m.Read(context.Background(), pcm)
	if err != nil || n != 6 || pcm[0] != 7 || pcm[1] != 7 || pcm[5] != 9 {
		t.Fatalf("read %d samples %v (%v), want the heard audio in stereo", n, pcm[:n], err)
	}
	if n, _ := m.Read(context.Background(), pcm); n != 0 {
		t.Errorf("a short gap in the audio filled %d samples of silence", n)
	}

	m.hear(audio{rate: 48000, channels: 2, pcm: make([]int16, 48000*2)})
	if most := int(mostAudio * 48000 / time.Second * 2); m.queued != most {
		t.Errorf("held %d samples, want at most %d", m.queued, most)
	}
}

func TestATurnRestartsTheReceiverOnceNobodyIsMirroring(t *testing.T) {
	s, out, _ := screeningRig(t)
	ctx := context.Background()
	s.handle(ctx, mirrorMessage(1200, 1920))

	out.width, out.height = 1920, 1200
	s.resized()
	if out.restarts != 0 {
		t.Fatal("a turn restarted the receiver mid-mirror")
	}
	s.handle(ctx, message{kind: msgEnded})
	if out.restarts != 1 {
		t.Errorf("restarted %d times after the mirror ended, want 1", out.restarts)
	}

	s.width, s.height = 1920, 1200
	s.resized()
	if out.restarts != 1 {
		t.Error("an unchanged size restarted the receiver")
	}
}

func TestTheScreenShrinksIntoWhatTheDecoderTakes(t *testing.T) {
	most := image.Pt(1920, 1088)
	for _, c := range []struct{ w, h, wantW, wantH int }{
		{1200, 1920, 1088, 1740},
		{1920, 1200, 1740, 1088},
		{1280, 800, 1280, 800},
		{800, 1280, 800, 1280},
	} {
		if w, h := fit(c.w, c.h, most); w != c.wantW || h != c.wantH {
			t.Errorf("fit(%dx%d) = %dx%d, want %dx%d", c.w, c.h, w, h, c.wantW, c.wantH)
		}
	}
}

func TestAMirrorSendsFramesOnlyOnChange(t *testing.T) {
	m := newMirror("mac", mirroring{codec: codecH264}, 48000, 2, func() {})
	if p, _ := m.Live(context.Background()); !p.Sparse {
		t.Error("the mirror's picture is not marked sparse")
	}
}
