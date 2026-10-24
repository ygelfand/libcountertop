package ui

import (
	"image"

	"github.com/ygelfand/libcountertop/pkg/display/theme"
)

type Image struct {
	w, h int
	px   []theme.Color
}

func NewImage(w, h int, c theme.Color) *Image {
	img := &Image{w: w, h: h, px: make([]theme.Color, w*h)}
	img.FillRect(Rect{W: w, H: h}, c)
	return img
}

func FromRGBA(img *image.RGBA) *Image {
	box := img.Bounds()
	out := &Image{w: box.Dx(), h: box.Dy(), px: make([]theme.Color, box.Dx()*box.Dy())}
	for y := range out.h {
		row := img.Pix[(box.Min.Y+y-img.Rect.Min.Y)*img.Stride:]
		for x := range out.w {
			p := row[(box.Min.X+x-img.Rect.Min.X)*4:]
			out.px[y*out.w+x] = theme.Color{R: p[0], G: p[1], B: p[2]}
		}
	}
	return out
}

func (i *Image) Size() (w, h int) { return i.w, i.h }

func (i *Image) Set(x, y int, c theme.Color) {
	if x < 0 || y < 0 || x >= i.w || y >= i.h {
		return
	}
	i.px[y*i.w+x] = c
}

func (i *Image) At(x, y int) theme.Color {
	if x < 0 || y < 0 || x >= i.w || y >= i.h {
		return theme.Color{}
	}
	return i.px[y*i.w+x]
}

type RGBADrawer interface {
	DrawRGBA(x, y int, img *image.RGBA, scale int, clip Rect)
}

func DrawRGBA(s Surface, x, y int, img *image.RGBA, scale int, clip Rect) {
	if d, ok := s.(RGBADrawer); ok {
		d.DrawRGBA(x, y, img, scale, clip)
		return
	}
	scale = max(scale, 1)
	b := img.Bounds()
	w, h := s.Size()
	x0, y0 := max(x, 0), max(y, 0)
	x1, y1 := min(x+b.Dx()*scale, w), min(y+b.Dy()*scale, h)
	if clip.W > 0 && clip.H > 0 {
		x0, y0 = max(x0, clip.X), max(y0, clip.Y)
		x1, y1 = min(x1, clip.X+clip.W), min(y1, clip.Y+clip.H)
	}
	for py := y0; py < y1; py++ {
		src := img.Pix[((py-y)/scale)*img.Stride:]
		for px := x0; px < x1; px++ {
			i := ((px - x) / scale) * 4
			s.Set(px, py, theme.Color{R: src[i], G: src[i+1], B: src[i+2]})
		}
	}
}

func (i *Image) DrawRGBA(x, y int, img *image.RGBA, scale int, clip Rect) {
	scale = max(scale, 1)
	b := img.Bounds()
	x0, y0 := max(x, 0), max(y, 0)
	x1, y1 := min(x+b.Dx()*scale, i.w), min(y+b.Dy()*scale, i.h)
	if clip.W > 0 && clip.H > 0 {
		x0, y0 = max(x0, clip.X), max(y0, clip.Y)
		x1, y1 = min(x1, clip.X+clip.W), min(y1, clip.Y+clip.H)
	}
	if x1 <= x0 || y1 <= y0 {
		return
	}
	sx0, phase0 := (x0-x)/scale, (x0-x)%scale
	sy, phy := (y0-y)/scale, (y0-y)%scale
	built := -1
	for py := y0; py < y1; py++ {
		dst := i.px[py*i.w+x0 : py*i.w+x1]
		if sy == built {
			copy(dst, i.px[(py-1)*i.w+x0:(py-1)*i.w+x1])
		} else {
			src := img.Pix[sy*img.Stride:]
			s, ph := sx0*4, phase0
			for k := range dst {
				dst[k] = theme.Color{R: src[s], G: src[s+1], B: src[s+2]}
				if ph++; ph == scale {
					ph, s = 0, s+4
				}
			}
			built = sy
		}
		if phy++; phy == scale {
			phy, sy = 0, sy+1
		}
	}
}

func Draw(s Surface, img *Image, x, y int) {
	w, h := s.Size()

	x0, y0 := max(-x, 0), max(-y, 0)
	x1, y1 := min(img.w, w-x), min(img.h, h-y)

	if clip := ClipOf(s); clip.W > 0 && clip.H > 0 {
		x0, y0 = max(x0, clip.X-x), max(y0, clip.Y-y)
		x1, y1 = min(x1, clip.X+clip.W-x), min(y1, clip.Y+clip.H-y)
	}
	if x1 <= x0 || y1 <= y0 {
		return
	}

	for iy := y0; iy < y1; iy++ {
		row := img.px[iy*img.w+x0 : iy*img.w+x1]
		for i, c := range row {
			s.Set(x+x0+i, y+iy, c)
		}
	}
}

func DrawScaled(s Surface, img *Image, r Rect) {
	if r.W <= 0 || r.H <= 0 || img.w <= 0 || img.h <= 0 {
		return
	}

	sw, sh := img.w, img.h
	if sw*r.H > sh*r.W {
		sw = sh * r.W / r.H
	} else {
		sh = sw * r.H / r.W
	}
	ox, oy := (img.w-sw)/2, (img.h-sh)/2

	for y := range r.H {
		sy := oy + y*sh/r.H
		for x := range r.W {
			setClipped(s, r.X+x, r.Y+y, img.At(ox+x*sw/r.W, sy))
		}
	}
}

func (i *Image) FillRect(r Rect, c theme.Color) {
	x0, y0 := max(r.X, 0), max(r.Y, 0)
	x1, y1 := min(r.X+r.W, i.w), min(r.Y+r.H, i.h)
	if x1 <= x0 || y1 <= y0 {
		return
	}

	first := i.px[y0*i.w+x0 : y0*i.w+x1]
	for x := range first {
		first[x] = c
	}

	for y := y0 + 1; y < y1; y++ {
		copy(i.px[y*i.w+x0:y*i.w+x1], first)
	}
}

func DrawImageScaled(s Surface, img image.Image, at Rect, on theme.Color) {
	b := img.Bounds()

	for y := range at.H {
		sy := b.Min.Y + y*b.Dy()/at.H
		for x := range at.W {
			sx := b.Min.X + x*b.Dx()/at.W

			// RGBA() is alpha-premultiplied.
			sr, sg, sb, sa := img.At(sx, sy).RGBA()
			if sa == 0 {
				continue
			}

			c := theme.Color{R: byte(sr >> 8), G: byte(sg >> 8), B: byte(sb >> 8)}
			if sa < 0xffff {
				inv := 0xffff - sa
				c = theme.Color{
					R: byte((sr + uint32(on.R)*0x101*inv/0xffff) >> 8),
					G: byte((sg + uint32(on.G)*0x101*inv/0xffff) >> 8),
					B: byte((sb + uint32(on.B)*0x101*inv/0xffff) >> 8),
				}
			}
			setClipped(s, at.X+x, at.Y+y, c)
		}
	}
}
