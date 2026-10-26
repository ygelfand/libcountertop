package poster

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/media/photos"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func jpegOf(w, h int, c color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 255
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

type fakePhotos struct {
	calls int
	empty bool
	data  []byte
}

func (f *fakePhotos) Next() (photos.Picture, bool) {
	f.calls++
	if f.empty {
		return photos.Picture{}, false
	}
	return photos.Picture{ID: "id" + string(rune('0'+f.calls)), Data: f.data}, true
}

func TestTheScheduleTakesTheNextPictureWhenDue(t *testing.T) {
	src := &fakePhotos{data: jpegOf(64, 48, color.RGBA{200, 50, 50, 255})}
	posterConfig = config.DefaultPoster()
	o := testOptions()
	o.Photos = src
	p := New(o)
	t0 := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

	p.step(t0, false, false)
	if src.calls != 0 {
		t.Fatalf("took %d pictures while off", src.calls)
	}
	if err := (posterWriter{}).Enabled(true); err != nil {
		t.Fatal(err)
	}
	src.empty = true
	p.step(t0, false, false)
	if _, ok := p.Frame().Current(); ok {
		t.Fatal("a backdrop with nothing cached")
	}
	src.empty, src.calls = false, 0
	p.step(t0, false, false)
	if src.calls != 1 || posterConfig.Last != "id1" {
		t.Fatalf("first step: %d calls, last %q", src.calls, posterConfig.Last)
	}
	if _, ok := p.Frame().Current(); !ok {
		t.Fatal("no backdrop after a fetch")
	}
	p.step(t0.Add(59*time.Minute), false, false)
	if src.calls != 1 {
		t.Fatalf("hourly took another after 59 minutes")
	}
	p.step(t0.Add(61*time.Minute), false, false)
	if src.calls != 2 {
		t.Fatalf("hourly did not take another after 61 minutes")
	}
	src.empty = true
	p.step(t0.Add(3*time.Hour), true, false)
	if _, ok := p.Frame().Current(); !ok {
		t.Error("an empty source dropped the picture already shown")
	}

	src.empty = false
	t1 := t0.Add(4 * time.Hour)
	if err := (posterWriter{}).Every(config.PosterWake); err != nil {
		t.Fatal(err)
	}
	p.step(t1, true, false)
	n := src.calls
	p.step(t1.Add(30*time.Hour), false, false)
	if src.calls != n {
		t.Fatalf("on-wake took one without a wake")
	}
	p.step(t1.Add(30*time.Hour), false, true)
	if src.calls != n+1 {
		t.Fatalf("on-wake did not take one on a wake")
	}

	if err := (posterWriter{}).Enabled(false); err != nil {
		t.Fatal(err)
	}
	p.step(t1.Add(31*time.Hour), false, false)
	if _, ok := p.Frame().Current(); ok {
		t.Error("turning it off left the picture up")
	}
}
