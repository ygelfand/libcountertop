package urlplayer

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"

	"github.com/ygelfand/libcountertop/pkg/media/cast"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

func ramp(frames int) []int16 {
	out := make([]int16, 2*frames)
	for i := range frames {
		out[2*i], out[2*i+1] = int16(i%30000), int16(-(i % 30000))
	}
	return out
}

func wavFile(rate int, samples []int16) []byte {
	var b bytes.Buffer
	data := len(samples) * 2
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+data))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{
		uint32(16), uint16(1), uint16(2), uint32(rate), uint32(rate * 4), uint16(4), uint16(16),
	} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(data))
	_ = binary.Write(&b, binary.LittleEndian, samples)
	return b.Bytes()
}

func flacFile(t *testing.T, rate int, samples []int16) []byte {
	t.Helper()
	var b bytes.Buffer
	frames := len(samples) / 2
	info := &meta.StreamInfo{
		BlockSizeMin: 4096, BlockSizeMax: 4096, SampleRate: uint32(rate),
		NChannels: 2, BitsPerSample: 16, NSamples: uint64(frames),
	}
	enc, err := flac.NewEncoder(&b, info)
	if err != nil {
		t.Fatal(err)
	}
	for start := 0; start < frames; start += 4096 {
		n := min(4096, frames-start)
		left, right := make([]int32, n), make([]int32, n)
		for i := range n {
			left[i], right[i] = int32(samples[2*(start+i)]), int32(samples[2*(start+i)+1])
		}
		f := &frame.Frame{
			Header: frame.Header{
				HasFixedBlockSize: true, BlockSize: uint16(n), SampleRate: uint32(rate),
				Channels: frame.ChannelsLR, BitsPerSample: 16, Num: uint64(start / 4096),
			},
			Subframes: []*frame.Subframe{
				{SubHeader: frame.SubHeader{Pred: frame.PredVerbatim}, Samples: left, NSamples: n},
				{SubHeader: frame.SubHeader{Pred: frame.PredVerbatim}, Samples: right, NSamples: n},
			},
		}
		if err := enc.WriteFrame(f); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func serve(t *testing.T, contentType string, body []byte, ranges bool) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		if ranges {
			http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/media"
}

func newTrack(url string, at time.Duration, resample func(int) func([]int16) []int16) *track {
	return New(Options{HTTP: http.DefaultClient, Resample: resample}).
		track(cast.Media{ContentID: url}, nil, at)
}

func readAll(t *testing.T, tr *track) []int16 {
	t.Helper()
	var out []int16
	buf := make([]int16, 1920)
	for {
		n, err := tr.Read(context.Background(), buf)
		out = append(out, buf[:n]...)
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestSniffRecognisesEachFormat(t *testing.T) {
	for _, c := range []struct {
		head        []byte
		contentType string
		want        string
	}{
		{[]byte("fLaC\x00"), "", FLAC},
		{[]byte("RIFF\x00\x00\x00\x00WAVE"), "", WAV},
		{[]byte{0x1a, 0x45, 0xdf, 0xa3}, "", Matroska},
		{[]byte("ID3\x04"), "", MP3},
		{[]byte{0xff, 0xf1, 0x50}, "", AAC},
		{[]byte{0xff, 0xfb, 0x90}, "", MP3},
		{nil, "audio/mpeg", MP3},
		{nil, "audio/aac", AAC},
		{nil, "video/webm; codecs=vp9", Matroska},
		{[]byte("#EXTM3U\n#EXT-X-"), "", HLS},
		{nil, "application/vnd.apple.mpegurl", HLS},
		{nil, "application/x-mpegURL", HLS},
		{nil, "audio/ogg", ""},
	} {
		if got := sniff(c.head, c.contentType); got != c.want {
			t.Errorf("sniff(% x, %q) = %q, want %q", c.head, c.contentType, got, c.want)
		}
	}
}

func TestWAVPlaysEverySample(t *testing.T) {
	want := ramp(outRate / 2)
	got := readAll(t, newTrack(serve(t, "audio/wav", wavFile(outRate, want), true), 0, nil))
	if !slicesEqual(got, want) {
		t.Fatalf("got %d samples, want %d identical", len(got), len(want))
	}
}

func TestWAVSeekLandsOnTheExactSample(t *testing.T) {
	want := ramp(outRate)
	got := readAll(
		t,
		newTrack(serve(t, "audio/wav", wavFile(outRate, want), true), 500*time.Millisecond, nil),
	)
	if !slicesEqual(got, want[outRate:]) {
		t.Fatalf(
			"after a half-second seek got %d samples starting %v, want %v",
			len(got),
			got[:2],
			want[outRate:outRate+2],
		)
	}
}

func TestAStreamAtAnotherRateIsResampled(t *testing.T) {
	asked := 0
	resample := func(from int) func([]int16) []int16 {
		asked = from
		return func(p []int16) []int16 { return p }
	}
	readAll(t, newTrack(serve(t, "audio/wav", wavFile(44100, ramp(4410)), true), 0, resample))
	if asked != 44100 {
		t.Fatalf("resampler asked for %d Hz, want 44100", asked)
	}
}

func TestFLACPlaysAndSeeksByDecodingForward(t *testing.T) {
	want := ramp(outRate)
	url := serve(t, "audio/flac", flacFile(t, outRate, want), true)
	if got := readAll(t, newTrack(url, 0, nil)); !slicesEqual(got, want) {
		t.Fatalf("got %d samples, want %d identical", len(got), len(want))
	}
	if got := readAll(
		t,
		newTrack(url, 250*time.Millisecond, nil),
	); !slicesEqual(
		got,
		want[outRate/2:],
	) {
		t.Fatalf(
			"after a quarter-second seek got %d samples, want %d",
			len(got),
			len(want[outRate/2:]),
		)
	}
}

func TestMP3Decodes(t *testing.T) {
	body, err := os.ReadFile("../pcm/testdata/tone.mp3")
	if err != nil {
		t.Fatal(err)
	}
	got := readAll(
		t,
		newTrack(serve(t, "audio/mpeg", body, true), 0, func(int) func([]int16) []int16 {
			return func(p []int16) []int16 { return p }
		}),
	)
	loud := false
	for _, s := range got {
		if s > 1000 || s < -1000 {
			loud = true
			break
		}
	}
	if len(got) == 0 || !loud {
		t.Fatalf("decoded %d samples, loud=%v", len(got), loud)
	}
}

func TestAServerWithoutRangesPlaysButCannotSeek(t *testing.T) {
	want := ramp(4800)
	url := serve(t, "audio/wav", wavFile(outRate, want), false)
	out := &fakeOutput{}
	p := New(Options{Output: out})
	if err := p.Load(cast.Media{ContentID: url}, 0, true); err != nil {
		t.Fatal(err)
	}
	out.wait(t, "play")
	got := readAll(t, out.last())
	if !slicesEqual(got, want) {
		t.Fatalf("got %d samples, want %d", len(got), len(want))
	}
	if err := p.Seek(30 * time.Second); err == nil {
		t.Fatal("seeked a stream from a server that ignores ranges")
	}
	if err := p.Seek(p.Elapsed() + time.Second); err != nil {
		t.Fatalf("a stream that cannot seek refused a seek to where it already is: %v", err)
	}
}

func TestPlayerMapsTransportOntoTheOutput(t *testing.T) {
	url := serve(t, "audio/wav", wavFile(outRate, ramp(outRate)), true)
	out := &fakeOutput{}
	p := New(Options{Output: out})
	if err := p.Load(cast.Media{ContentID: url}, 0, false); err != nil {
		t.Fatal(err)
	}
	out.wait(t, "play pause")
	_ = p.Play()
	if err := p.Seek(400 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	out.wait(t, "play pause resume handoff play")
	_ = p.Stop()
	want := "play pause resume handoff play stop"
	if got := out.calls(); got != want {
		t.Fatalf("output saw %q, want %q", got, want)
	}
	if at := out.tracks[1].Start(); at != 400*time.Millisecond {
		t.Fatalf("seeked track starts at %v, want 400ms", at)
	}
}

func TestLoadRefusesWhatIsNotHTTP(t *testing.T) {
	p := New(Options{Output: &fakeOutput{}})
	if err := p.Load(cast.Media{ContentID: "file:///etc/passwd"}, 0, true); err == nil {
		t.Fatal("loaded a file URL")
	}
}

func slicesEqual(a, b []int16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type fakeOutput struct {
	mu     sync.Mutex
	log    []string
	tracks []*track
}

func (f *fakeOutput) record(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, s)
}

func (f *fakeOutput) calls() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.log, " ")
}

func (f *fakeOutput) wait(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f.calls() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("output saw %q, want %q", f.calls(), want)
}

func (f *fakeOutput) last() *track {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tracks[len(f.tracks)-1]
}

func (f *fakeOutput) Attend(*playback.Session) {}
func (f *fakeOutput) Failed(error)             {}
func (f *fakeOutput) Play(src playback.Source) {
	f.mu.Lock()
	f.tracks = append(f.tracks, src.(*track))
	f.mu.Unlock()
	f.record("play")
}
func (f *fakeOutput) Stop(playback.Source)    { f.record("stop") }
func (f *fakeOutput) Handoff(playback.Source) { f.record("handoff") }
func (f *fakeOutput) Pause(playback.Source)   { f.record("pause") }
func (f *fakeOutput) Resume(playback.Source)  { f.record("resume") }
func (f *fakeOutput) Position(playback.Source) (time.Duration, bool) {
	return 0, false
}
func (f *fakeOutput) Behind(playback.Source) time.Duration { return 0 }
func (f *fakeOutput) Changed(playback.Source)              {}
