package picture

import (
	"bytes"
	"image"
	_ "image/png"

	"github.com/gen2brain/jpegn"
)

type Chroma = jpegn.UpsampleMethod

const (
	Fast   Chroma = jpegn.NearestNeighbor
	Smooth Chroma = jpegn.CatmullRom
)

func Decode(data []byte, box image.Point, chroma Chroma) (image.Image, error) {
	cfg, err := jpegn.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		img, _, err := image.Decode(bytes.NewReader(data))
		return img, err
	}
	return jpegn.Decode(bytes.NewReader(data), &jpegn.Options{
		ToRGBA:         true,
		UpsampleMethod: chroma,
		ScaleDenom:     ScaleDenom(cfg.Width, cfg.Height, box.X, box.Y),
	})
}

func ScaleDenom(sw, sh, w, h int) int {
	if w <= 0 || h <= 0 || sw <= 0 || sh <= 0 {
		return 1
	}
	fw, fh := w, sh*w/sw
	if fh > h {
		fw, fh = sw*h/sh, h
	}
	d := 1
	for d < 8 && sw/(2*d) >= fw && sh/(2*d) >= fh {
		d *= 2
	}
	return d
}
