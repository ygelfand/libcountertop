package photos

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func twoTone(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{0, 0, 255, 255}
			if x < w/2 {
				c = color.RGBA{255, 0, 0, 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func withOrientation(data []byte, turn uint16) []byte {
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08")
	tiff = binary.BigEndian.AppendUint16(tiff, 1)
	tiff = binary.BigEndian.AppendUint16(tiff, tagOrient)
	tiff = binary.BigEndian.AppendUint16(tiff, 3)
	tiff = binary.BigEndian.AppendUint32(tiff, 1)
	tiff = binary.BigEndian.AppendUint16(tiff, turn)
	tiff = append(tiff, 0, 0, 0, 0, 0, 0)
	body := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1}
	seg = binary.BigEndian.AppendUint16(seg, uint16(len(body)+2))
	seg = append(seg, body...)
	out := append([]byte{}, data[:2]...)
	out = append(out, seg...)
	return append(out, data[2:]...)
}

func decoded(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestShrinkKeepsSmallPlainPictures(t *testing.T) {
	data := twoTone(80, 40)
	if got := shrink(data, 200); !bytes.Equal(got, data) {
		t.Error("a picture already within the panel was re-encoded")
	}
}

func TestShrinkFitsTheLongSide(t *testing.T) {
	for _, size := range []image.Point{{400, 200}, {200, 400}} {
		b := decoded(t, shrink(twoTone(size.X, size.Y), 100)).Bounds()
		if max(b.Dx(), b.Dy()) != 100 || b.Dx()*size.Y != b.Dy()*size.X {
			t.Errorf("%v shrank to %v", size, b.Size())
		}
	}
}

func TestShrinkTurnsUpright(t *testing.T) {
	data := withOrientation(twoTone(80, 40), 6)
	if got := orientation(data); got != 6 {
		t.Fatalf("read orientation %d", got)
	}
	img := decoded(t, shrink(data, 0))
	if b := img.Bounds(); b.Dx() != 40 || b.Dy() != 80 {
		t.Fatalf("turned to %v", b.Size())
	}
	top, _, _, _ := img.At(20, 5).RGBA()
	bottom, _, _, _ := img.At(20, 75).RGBA()
	if top < 0x8000 || bottom > 0x8000 {
		t.Errorf("rotation went the wrong way: top red %x, bottom red %x", top, bottom)
	}
}

func TestOrientationIgnoresOtherFiles(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("PNG"), twoTone(8, 8)} {
		if got := orientation(data); got != 0 {
			t.Errorf("orientation %d from %d bytes", got, len(data))
		}
	}
}
