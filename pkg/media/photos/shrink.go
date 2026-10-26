package photos

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	_ "image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	shrinkQuality = 90
	tagOrient     = 0x0112
)

func shrink(data []byte, most int) []byte {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return data
	}
	turn := orientation(data)
	big := most > 0 && max(cfg.Width, cfg.Height) > most
	if !big && turn <= 1 {
		return data
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data
	}
	w, h := cfg.Width, cfg.Height
	if big {
		if w >= h {
			w, h = most, max(h*most/w, 1)
		} else {
			w, h = max(w*most/h, 1), most
		}
	}
	scaled := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), src, src.Bounds(), draw.Src, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(
		&buf,
		orient(scaled, turn),
		&jpeg.Options{Quality: shrinkQuality},
	); err != nil {
		return data
	}
	return buf.Bytes()
}

func orient(src *image.RGBA, turn int) *image.RGBA {
	if turn <= 1 || turn > 8 {
		return src
	}
	w, h := src.Rect.Dx(), src.Rect.Dy()
	ow, oh := w, h
	if turn >= 5 {
		ow, oh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := range oh {
		for x := range ow {
			var sx, sy int
			switch turn {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			si := sy*src.Stride + sx*4
			di := y*out.Stride + x*4
			copy(out.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return out
}

func orientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0
	}
	at := 2
	for at+4 <= len(data) {
		if data[at] != 0xFF {
			return 0
		}
		marker := data[at+1]
		size := int(binary.BigEndian.Uint16(data[at+2:]))
		if marker == 0xDA || size < 2 || at+2+size > len(data) {
			return 0
		}
		body := data[at+4 : at+2+size]
		if marker == 0xE1 && bytes.HasPrefix(body, []byte("Exif\x00\x00")) {
			return exifOrientation(body[6:])
		}
		at += 2 + size
	}
	return 0
}

func exifOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 0
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}
	ifd := int(order.Uint32(tiff[4:]))
	if ifd+2 > len(tiff) {
		return 0
	}
	n := int(order.Uint16(tiff[ifd:]))
	for i := range n {
		e := ifd + 2 + i*12
		if e+12 > len(tiff) {
			return 0
		}
		if order.Uint16(tiff[e:]) == tagOrient {
			return int(order.Uint16(tiff[e+8:]))
		}
	}
	return 0
}
