package urlplayer

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/media/cast"
	"github.com/ygelfand/libcountertop/pkg/media/cenc"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

type fakeKeys struct {
	mu       sync.Mutex
	audio    *cenc.Track
	pictures []cenc.Sample
	sounds   []cenc.Sample
	closed   bool
}

func (k *fakeKeys) OpenAudio(t cenc.Track) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.audio = &t
	return nil
}

func (k *fakeKeys) Picture(_ context.Context, _ cenc.Track, s cenc.Sample) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.pictures = append(k.pictures, s)
	return nil
}

func (k *fakeKeys) Sound(_ cenc.Track, s cenc.Sample) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.sounds = append(k.sounds, s)
	return nil
}

func (k *fakeKeys) Wait(context.Context, [16]byte) error { return nil }
func (k *fakeKeys) Begin(time.Duration)                  {}
func (k *fakeKeys) Read([]int16) (int, error)            { return 0, nil }
func (k *fakeKeys) Origin() (time.Duration, bool)        { return 0, false }
func (k *fakeKeys) Fail(error)                           {}

func (k *fakeKeys) Video(width, height int) playback.Picture {
	return playback.Picture{
		H264:   true,
		Width:  width,
		Height: height,
		Sealed: func(context.Context) ([]byte, time.Duration, *cenc.Crypt, error) { return nil, 0, nil, nil },
	}
}

func (k *fakeKeys) Close() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.closed = true
}

func (k *fakeKeys) counts() (pictures, sounds int, encrypted bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	encrypted = len(k.pictures) > 0 && len(k.sounds) > 0 &&
		k.pictures[0].Encrypted && k.sounds[0].Encrypted
	return len(k.pictures), len(k.sounds), encrypted
}

func sealedFeed(t *testing.T) *Feed {
	t.Helper()
	video, err := os.ReadFile("testdata/feed-sealed-v.mp4")
	if err != nil {
		t.Fatal(err)
	}
	sound, err := os.ReadFile("testdata/feed-sealed-a.mp4")
	if err != nil {
		t.Fatal(err)
	}
	f := NewFeed()
	f.Add(videoLane, `video/mp4; codecs="avc1.64000a"`)
	f.Add(soundLane, `audio/mp4; codecs="mp4a.40.2"`)
	appendInPieces(t, f, videoLane, video, 500)
	appendInPieces(t, f, soundLane, sound, 300)
	return f
}

func withPSSH(t *testing.T, file []byte) ([]byte, []byte) {
	t.Helper()
	pssh := binary.BigEndian.AppendUint32(nil, 32)
	pssh = append(pssh, "pssh"...)
	pssh = append(pssh, make([]byte, 24)...)
	for at := 0; at+8 <= len(file); {
		n := int(binary.BigEndian.Uint32(file[at:]))
		if string(file[at+4:at+8]) == "moov" {
			out := slices.Concat(file[:at+n], pssh, file[at+n:])
			binary.BigEndian.PutUint32(out[at:], uint32(n+len(pssh)))
			return out, pssh
		}
		at += n
	}
	t.Fatal("no moov to carry a pssh")
	return nil, nil
}

func TestAProtectedFeedReportsItsInitData(t *testing.T) {
	video, err := os.ReadFile("testdata/feed-sealed-v.mp4")
	if err != nil {
		t.Fatal(err)
	}
	video, pssh := withPSSH(t, video)
	f := NewFeed()
	f.Add(videoLane, `video/mp4; codecs="avc1.64000a"`)
	appendInPieces(t, f, videoLane, video, 500)
	if o, ok := f.Opened(); !ok || o.Width != 64 || o.Height != 64 {
		t.Fatalf("a protected feed opened as %+v %v, want 64x64", o, ok)
	}
	if got := f.InitData(videoLane); len(got) != 1 || !bytes.Equal(got[0], pssh) {
		t.Fatalf("a protected init segment gave %x for the page, want its pssh", got)
	}
	if len(f.InitData(videoLane)) != 0 {
		t.Fatal("init data was handed out twice")
	}
}

func TestAProtectedFeedPlaysThroughItsKeys(t *testing.T) {
	f := sealedFeed(t)
	f.End()
	keys := &fakeKeys{}
	f.Protect(func(context.Context) (Keys, error) { return keys, nil })
	tr := New(Options{Screen: true}).track(cast.Media{}, f, 0)
	if err := tr.ensure(); err != nil {
		t.Fatal(err)
	}
	pic, err := tr.Live(context.Background())
	if err != nil || pic.Sealed == nil || pic.Width != 64 {
		t.Fatalf("a protected track's picture is %+v, %v; want the keys' sealed picture", pic, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p, s, _ := keys.counts(); p >= 30 && s >= 90 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	p, s, encrypted := keys.counts()
	if p < 30 || s < 90 || !encrypted {
		t.Fatalf(
			"keys got %d pictures and %d sound frames, encrypted %v; want the whole clip, encrypted",
			p,
			s,
			encrypted,
		)
	}
	if keys.audio == nil || keys.audio.Format != "mp4a" || len(keys.audio.Config) == 0 {
		t.Fatalf("the sound decoder was opened with %+v", keys.audio)
	}
	tr.close()
	if !keys.closed {
		t.Fatal("closing the track left its keys open")
	}
}

func TestAProtectedFeedWithoutKeysRefusesToPlay(t *testing.T) {
	f := sealedFeed(t)
	tr := New(Options{Screen: true}).track(cast.Media{}, f, 0)
	defer tr.close()
	if err := tr.ensure(); err == nil {
		t.Fatal("a protected feed played with no keys")
	}
}
