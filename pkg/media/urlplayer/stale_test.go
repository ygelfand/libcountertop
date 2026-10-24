package urlplayer

import (
	"bufio"
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/media/cast"
)

func TestAReplacedTrackCannotPauseItsSuccessor(t *testing.T) {
	url := serve(t, "audio/wav", wavFile(outRate, ramp(outRate)), true)
	out := &fakeOutput{}
	p := New(Options{Output: out})
	if err := p.Load(cast.Media{ContentID: url}, 0, true); err != nil {
		t.Fatal(err)
	}
	out.wait(t, "play")
	old := out.last()
	if err := p.Seek(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	out.wait(t, "play handoff play")
	old.Pause()
	old.Stop()
	if got, want := out.calls(), "play handoff play"; got != want {
		t.Fatalf("output saw %q after the old track's controls, want %q", got, want)
	}
	if _, paused := p.cur, p.paused; paused {
		t.Fatal("the player thinks it is paused")
	}
}

func TestMP3PaceReadsTheTagAndBitrate(t *testing.T) {
	var b bytes.Buffer
	b.Write([]byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0x01, 0x39})
	b.Write(make([]byte, 128+57))
	b.Write([]byte{0xff, 0xfb, 0x90, 0x64})
	b.Write(make([]byte, 64))
	tag, pace := mp3Pace(bufio.NewReader(&b))
	if tag != 195 || pace != 16000 {
		t.Fatalf("tag %d, pace %d bytes/s; want 195 and 16000 (128 kb/s)", tag, pace)
	}
}

func TestMP3SyncFindsTheNextFrameFromMidStream(t *testing.T) {
	body, err := os.ReadFile("../pcm/testdata/tone.mp3")
	if err != nil {
		t.Fatal(err)
	}
	first := -1
	for i := range len(body) - 4 {
		if n := mp3FrameLen(body[i:]); n > 0 && i+n+4 <= len(body) && mp3FrameLen(body[i+n:]) > 0 {
			first = i
			break
		}
	}
	if first < 0 {
		t.Fatal("no frame in the fixture")
	}
	next := first + mp3FrameLen(body[first:])
	r := bufio.NewReader(bytes.NewReader(body[first+7:]))
	if err := mp3Sync(r); err != nil {
		t.Fatal(err)
	}
	head, _ := r.Peek(4)
	if !bytes.Equal(head, body[next:next+4]) {
		t.Fatalf("synced to % x, want the frame at %d (% x)", head, next, body[next:next+4])
	}
}

func TestMP3PaceGivesUpOnAnUnknownHeader(t *testing.T) {
	if tag, pace := mp3Pace(
		bufio.NewReader(bytes.NewReader([]byte("not an mp3 at all"))),
	); pace != 0 {
		t.Fatalf("tag %d, pace %d for garbage", tag, pace)
	}
}
