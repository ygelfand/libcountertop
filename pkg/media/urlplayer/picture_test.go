package urlplayer

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/media/cast"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
)

func TestAnMKVWithH264ShowsPicturesAndPlaysSound(t *testing.T) {
	body, err := os.ReadFile("testdata/clip.mkv")
	if err != nil {
		t.Fatal(err)
	}
	url := serve(t, "video/x-matroska", body, true)
	tr := New(
		Options{HTTP: http.DefaultClient, Screen: true},
	).track(cast.Media{ContentID: url}, nil, 0)
	defer tr.close()
	if err := tr.ensure(); err != nil {
		t.Fatal(err)
	}
	if !tr.Pictured() {
		t.Fatal("an H.264 MKV is not pictured")
	}
	pic, err := tr.Live(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pic.Codec != playback.H264 || pic.Width != 64 || pic.Height != 64 {
		t.Fatalf("picture %08x %dx%d, want H264 64x64", pic.Codec, pic.Width, pic.Height)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, at, err := pic.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(first, []byte{0, 0, 0, 1}) || first[4]&0x1f != 7 {
		t.Fatalf(
			"first frame at %v starts % x, want an Annex-B SPS",
			at,
			first[:min(8, len(first))],
		)
	}
	if got := readAll(t, tr); len(got) < outRate {
		t.Fatalf("decoded %d samples alongside the picture, want about a second", len(got))
	}
}

func TestAnMKVOnABoardWithoutAScreenIsSoundOnly(t *testing.T) {
	body, err := os.ReadFile("testdata/clip.mkv")
	if err != nil {
		t.Fatal(err)
	}
	tr := newTrack(serve(t, "video/x-matroska", body, true), 0, nil)
	defer tr.close()
	if got := readAll(t, tr); len(got) < outRate {
		t.Fatalf("decoded %d samples, want about a second", len(got))
	}
	if tr.Pictured() {
		t.Fatal("a board without a screen asked for pictures")
	}
}

func TestAnMP4WithItsIndexAtTheEndShowsPicturesAndPlaysSound(t *testing.T) {
	body, err := os.ReadFile("testdata/clip.mp4")
	if err != nil {
		t.Fatal(err)
	}
	url := serve(t, "video/mp4", body, true)
	tr := New(
		Options{HTTP: http.DefaultClient, Screen: true},
	).track(cast.Media{ContentID: url}, nil, 0)
	defer tr.close()
	if err := tr.ensure(); err != nil {
		t.Fatal(err)
	}
	pic, err := tr.Live(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if pic.Codec != playback.H264 || pic.Width != 64 || pic.Height != 64 {
		t.Fatalf("picture %08x %dx%d, want H264 64x64", pic.Codec, pic.Width, pic.Height)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, _, err := pic.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(first, []byte{0, 0, 0, 1}) || first[4]&0x1f != 7 {
		t.Fatalf("first frame starts % x, want an Annex-B SPS", first[:min(8, len(first))])
	}
	if got := readAll(t, tr); len(got) < 3*outRate {
		t.Fatalf(
			"decoded %d samples alongside the picture, want about two seconds of stereo",
			len(got),
		)
	}
}

func TestAnMP4StartsPartWay(t *testing.T) {
	body, err := os.ReadFile("testdata/clip.mp4")
	if err != nil {
		t.Fatal(err)
	}
	tr := newTrack(serve(t, "video/mp4", body, true), time.Second, nil)
	defer tr.close()
	got := readAll(t, tr)
	if second := 2 * outRate; len(got) < second*9/10 || len(got) > second*3/2 {
		t.Fatalf(
			"decoded %d samples from 1s into a 2s clip, want about one second of stereo",
			len(got),
		)
	}
}

func TestMatroskaSoundInEveryCodecWeDecode(t *testing.T) {
	for _, name := range []string{"mp3", "flac", "vorbis"} {
		body, err := os.ReadFile("testdata/sound-" + name + ".mka")
		if err != nil {
			t.Fatal(err)
		}
		tr := newTrack(serve(t, "audio/x-matroska", body, true), 0, nil)
		got := readAll(t, tr)
		tr.close()
		if second := 2 * outRate; len(got) < second {
			t.Errorf(
				"%s in matroska decoded %d samples, want over a second of stereo",
				name,
				len(got),
			)
		}
	}
}

func TestHEVCShowsPicturesInMKVAndMP4(t *testing.T) {
	for file, kind := range map[string]string{
		"testdata/clip-hevc.mkv": "video/x-matroska",
		"testdata/clip-hevc.mp4": "video/mp4",
	} {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		tr := New(
			Options{HTTP: http.DefaultClient, Screen: true},
		).track(cast.Media{ContentID: serve(t, kind, body, true)}, nil, 0)
		if err := tr.ensure(); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		pic, err := tr.Live(context.Background())
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if pic.Codec != playback.HEVC || pic.Width != 64 || pic.Height != 64 {
			t.Fatalf(
				"%s: picture %08x %dx%d, want HEVC 64x64",
				file,
				pic.Codec,
				pic.Width,
				pic.Height,
			)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		first, _, err := pic.Next(ctx)
		cancel()
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if !bytes.HasPrefix(first, []byte{0, 0, 0, 1}) || (first[4]>>1)&0x3f != 32 {
			t.Fatalf(
				"%s: first frame starts % x, want an Annex-B VPS",
				file,
				first[:min(8, len(first))],
			)
		}
		if got := readAll(t, tr); len(got) < outRate {
			t.Fatalf(
				"%s: decoded %d samples alongside the picture, want about a second",
				file,
				len(got),
			)
		}
		tr.close()
	}
}

func TestTenBitHEVCPlaysSoundOnly(t *testing.T) {
	body, err := os.ReadFile("testdata/clip-hevc10.mkv")
	if err != nil {
		t.Fatal(err)
	}
	tr := New(
		Options{HTTP: http.DefaultClient, Screen: true},
	).track(cast.Media{ContentID: serve(t, "video/x-matroska", body, true)}, nil, 0)
	defer tr.close()
	if got := readAll(t, tr); len(got) < outRate {
		t.Fatalf("decoded %d samples, want about a second", len(got))
	}
	if tr.Pictured() {
		t.Fatal("a Main 10 HEVC track was offered to the decoder")
	}
}

func TestHEVCConfigRejectsCutShortAndNonMainRecords(t *testing.T) {
	record := make([]byte, 23)
	record[0], record[1], record[22] = 1, 1, 1
	if _, err := parseHEVCConfig(record); err == nil {
		t.Error("parsed an hvcC record whose array runs past its end")
	}
	record[1] = 2
	if _, err := parseHEVCConfig(record); !errors.Is(err, errUnplayable) {
		t.Errorf("a Main 10 hvcC record gave %v, want unplayable", err)
	}
}

func TestAVCConfigRejectsARecordCutShort(t *testing.T) {
	if _, err := parseAVCConfig([]byte{1, 0x42, 0, 0x1e, 0xff, 0xe1, 0, 9, 0x67}); err == nil {
		t.Fatal("parsed an avcC record whose SPS runs past its end")
	}
}
