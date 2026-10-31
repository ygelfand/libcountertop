package picture

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func TestTheDecodeScaleStillCoversTheBox(t *testing.T) {
	cases := []struct {
		sw, sh, w, h, want int
	}{
		{1920, 1080, 1200, 1920, 1},
		{1920, 1080, 960, 1600, 2},
		{1920, 1080, 400, 400, 4},
		{1920, 1080, 100, 100, 8},
		{1920, 1080, 0, 0, 1},
		{640, 480, 1280, 800, 1},
		{4000, 3000, 1920, 1920, 2},
	}
	for _, c := range cases {
		if got := ScaleDenom(c.sw, c.sh, c.w, c.h); got != c.want {
			t.Errorf("%dx%d in %dx%d: 1/%d, want 1/%d", c.sw, c.sh, c.w, c.h, got, c.want)
		}
	}
}

func TestADecodeIsRGBAAtTheChosenScale(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for i := range src.Pix {
		src.Pix[i] = 0x80
	}
	src.Set(0, 0, color.RGBA{R: 0xff, A: 0xff})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, nil); err != nil {
		t.Fatal(err)
	}
	for _, chroma := range []Chroma{Fast, Smooth} {
		img, err := Decode(buf.Bytes(), image.Point{X: 80, Y: 80}, chroma)
		if err != nil {
			t.Fatal(err)
		}
		pic, ok := img.(*image.RGBA)
		if !ok {
			t.Fatalf("chroma %d: got %T, want *image.RGBA", chroma, img)
		}
		if b := pic.Bounds(); b.Dx() != 80 || b.Dy() != 60 {
			t.Errorf("chroma %d: decoded %v, want 80x60 (a quarter)", chroma, b.Size())
		}
	}
}
