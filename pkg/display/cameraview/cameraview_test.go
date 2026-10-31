package cameraview

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

func TestTheCollageKeepsEveryCameraWholeAndOnScreen(t *testing.T) {
	wide, old, door := 16.0/9.0, 4.0/3.0, 9.0/16.0
	cases := []struct {
		name   string
		shapes []float64
		w, h   float64
		scale  int
	}{
		{"mixed landscape", []float64{wide, old, wide, door, old, wide, wide}, 1920, 1200, 3},
		{"many small", slices.Repeat([]float64{wide, old}, 20), 1920, 1200, 1},
		{"portrait screen", []float64{wide, door, old, wide}, 1200, 1920, 3},
		{"one doorbell, largest", []float64{door}, 1280, 800, 5},
		{"one panorama", []float64{4}, 1280, 800, 3},
	}
	for _, c := range cases {
		pages := Collage(c.shapes, c.w, c.h, RowHeight(c.scale, c.w), 6)
		seen := 0
		for p, rects := range pages {
			if len(rects) == 0 {
				t.Errorf("%s: page %d is empty", c.name, p)
			}
			for _, r := range rects {
				got, want := float64(r.W)/float64(r.H), c.shapes[seen]
				if math.Abs(got-want)/want > 0.05 {
					t.Errorf(
						"%s: camera %d drawn at %.3f, its shape is %.3f",
						c.name,
						seen,
						got,
						want,
					)
				}
				if r.X < 0 || r.Y < 0 || float64(r.X+r.W) > c.w+1 || float64(r.Y+r.H) > c.h+1 {
					t.Errorf(
						"%s: camera %d at %+v leaves the %vx%v screen",
						c.name,
						seen,
						r,
						c.w,
						c.h,
					)
				}
				seen++
			}
		}
		if seen != len(c.shapes) {
			t.Errorf("%s: placed %d cameras, want %d", c.name, seen, len(c.shapes))
		}
	}
}

func TestAFullRowSpansTheScreen(t *testing.T) {
	shapes := []float64{16.0 / 9.0, 4.0 / 3.0, 16.0 / 9.0, 16.0 / 9.0, 4.0 / 3.0, 16.0 / 9.0}
	pages := Collage(shapes, 1920, 1200, RowHeight(3, 1920), 6)
	first := pages[0]
	right := 0
	for _, r := range first {
		if r.Y != first[0].Y {
			break
		}
		right = r.X + r.W
	}
	if first[0].X > 2 || right < 1918 {
		t.Errorf("first row runs %d to %d, want 0 to 1920", first[0].X, right)
	}
}

func TestBiggerScaleMeansFewerPerPage(t *testing.T) {
	shapes := slices.Repeat([]float64{16.0 / 9.0}, 12)
	small := Collage(shapes, 1920, 1200, RowHeight(1, 1920), 6)
	large := Collage(shapes, 1920, 1200, RowHeight(4, 1920), 6)
	if len(small[0]) <= len(large[0]) {
		t.Errorf("smallest scale fits %d on a page, larger fits %d", len(small[0]), len(large[0]))
	}
}

func TestTheAnswerSaysWhetherAudioComes(t *testing.T) {
	video := "v=0\r\nm=video 9 UDP/TLS/RTP/SAVPF 102\r\na=sendonly\r\na=rtpmap:102 H264/90000\r\n"
	cases := []struct {
		name, sdp, want string
	}{
		{"no audio section", video, "none"},
		{"audio rejected", video + "m=audio 0 UDP/TLS/RTP/SAVPF 111\r\n", "rejected"},
		{
			"opus sent",
			video + "m=audio 9 UDP/TLS/RTP/SAVPF 111\r\na=sendonly\r\na=rtpmap:111 opus/48000/2\r\n",
			"sendonly opus/48000/2",
		},
		{
			"audio inactive",
			"m=audio 9 UDP/TLS/RTP/SAVPF 0\r\na=inactive\r\na=rtpmap:0 PCMU/8000\r\n" + video,
			"inactive PCMU/8000",
		},
	}
	for _, c := range cases {
		if got := audioOf(c.sdp); got != c.want {
			t.Errorf("%s: audio %q, want %q", c.name, got, c.want)
		}
	}
}

func jpegOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestASnapshotIsFittedToTheTile(t *testing.T) {
	shot := jpegOf(t, 640, 360)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/camera_proxy/camera.door" || r.URL.Query().Get("token") != "abc" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(shot)
	}))
	defer srv.Close()

	img, err := snapshot(
		context.Background(),
		srv.Client(),
		srv.URL,
		"/api/camera_proxy/camera.door?token=abc",
		320,
		320,
	)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 320 || b.Dy() != 180 {
		t.Errorf("fitted to %v, want 320x180", b.Size())
	}
}

func TestNoAddressIsAnError(t *testing.T) {
	if _, err := snapshot(
		context.Background(),
		http.DefaultClient,
		"",
		"/x",
		10,
		10,
	); err != ErrNoSite {
		t.Errorf("an unknown address said %v, want ErrNoSite", err)
	}
}

func TestTheLiveStreamYieldsEachFrame(t *testing.T) {
	shot := jpegOf(t, 160, 90)
	for _, declared := range []string{"frame", "--frame"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/camera_proxy_stream/camera.door" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "multipart/x-mixed-replace;boundary="+declared)
			for range 3 {
				fmt.Fprintf(
					w,
					"--frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n",
					len(shot),
				)
				_, _ = w.Write(shot)
				_, _ = w.Write([]byte("\r\n"))
			}
			_, _ = w.Write([]byte("--frame--\r\n"))
		}))

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		frames := 0
		_ = stream(ctx, srv.Client(), srv.URL, "/api/camera_proxy/camera.door?token=abc",
			func(img *image.RGBA) {
				frames++
				if b := img.Bounds(); b.Dx() != 160 || b.Dy() != 90 {
					t.Errorf("boundary %q: frame is %v, want the full 160x90", declared, b.Size())
				}
			})
		cancel()
		srv.Close()
		if frames != 3 {
			t.Errorf("boundary %q: got %d frames, want 3", declared, frames)
		}
	}
}

func TestTheLiveStreamOutlastsTheClientTimeout(t *testing.T) {
	shot := jpegOf(t, 16, 9)
	const sent = 8
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "multipart/x-mixed-replace;boundary=frame")
		for range sent {
			fmt.Fprintf(
				w,
				"--frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n",
				len(shot),
			)
			_, _ = w.Write(shot)
			_, _ = w.Write([]byte("\r\n"))
			w.(http.Flusher).Flush()
			time.Sleep(50 * time.Millisecond)
		}
		_, _ = w.Write([]byte("--frame--\r\n"))
	}))
	defer srv.Close()
	client := srv.Client()
	client.Timeout = 100 * time.Millisecond
	frames := 0
	err := stream(context.Background(), client, srv.URL, "/api/camera_proxy/camera.door",
		func(*image.RGBA) { frames++ })
	if frames != sent {
		t.Errorf(
			"got %d frames over %v, want %d; err %v",
			frames,
			sent*50*time.Millisecond,
			sent,
			err,
		)
	}
}
