package doorbellview

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"strings"
	"sync"

	gogui "github.com/go-gui-org/go-gui/gui"
	xdraw "golang.org/x/image/draw"

	"github.com/ygelfand/libcountertop/pkg/display/message"
)

type pictures struct {
	mu    sync.Mutex
	gen   uint64
	shown map[string]message.Picture
}

func (p *pictures) keep(name string, jpeg []byte, most int) (message.Picture, bool) {
	img, _, err := image.Decode(bytes.NewReader(jpeg))
	if err != nil {
		return message.Picture{}, false
	}
	return p.keepImage(name, shrink(img, most)), true
}

func (p *pictures) keepImage(name string, rgba *image.RGBA) message.Picture {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.shown == nil {
		p.shown = map[string]message.Picture{}
	}
	p.gen++
	key := fmt.Sprintf("doorbell/%s/%d", name, p.gen)
	pic := message.Picture{
		Src:    gogui.UseImage(key, rgba.Rect.Dx(), rgba.Rect.Dy(), rgba.Pix),
		Width:  rgba.Rect.Dx(),
		Height: rgba.Rect.Dy(),
	}
	if old, ok := p.shown[name]; ok {
		gogui.DropImage(old.Src)
	}
	p.shown[name] = pic
	return pic
}

func (p *pictures) get(name string) (message.Picture, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pic, ok := p.shown[name]
	return pic, ok
}

func (p *pictures) prune(prefix string, keep map[string]bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for name, pic := range p.shown {
		if strings.HasPrefix(name, prefix) && !keep[name] {
			gogui.DropImage(pic.Src)
			delete(p.shown, name)
		}
	}
}

func shrink(img image.Image, most int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if most > 0 && max(w, h) > most {
		if w >= h {
			w, h = most, max(1, h*most/w)
		} else {
			w, h = max(1, w*most/h), most
		}
		out := image.NewRGBA(image.Rect(0, 0, w, h))
		xdraw.ApproxBiLinear.Scale(out, out.Rect, img, b, draw.Src, nil)
		return out
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Rect, img, b.Min, draw.Src)
	return out
}
