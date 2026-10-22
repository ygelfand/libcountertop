package gogui

import (
	"image"
	"sync"
)

const livePrefix = "mem:live:"

type Live struct {
	key string

	mu    sync.Mutex
	w, h  int
	pix   []byte
	dirty image.Rectangle
	fresh bool
}

var lives sync.Map

func NewLive(key string) *Live {
	l := &Live{key: key}
	lives.Store(key, l)
	return l
}

func (l *Live) Src() string { return livePrefix + l.key }

func (l *Live) Write(w, h int, r image.Rectangle, rgba []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if w != l.w || h != l.h {
		l.w, l.h = w, h
		l.pix = make([]byte, w*h*4)
		l.fresh = true
	}
	r = r.Intersect(image.Rect(0, 0, w, h))
	if r.Empty() || len(rgba) < r.Dx()*r.Dy()*4 {
		return
	}
	row := r.Dx() * 4
	for y := r.Min.Y; y < r.Max.Y; y++ {
		at := (y*w + r.Min.X) * 4
		copy(l.pix[at:at+row], rgba[(y-r.Min.Y)*row:])
	}
	l.dirty = l.dirty.Union(r)
}

func (l *Live) take(upload func(w, h int, r image.Rectangle, pix []byte) error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.w == 0 {
		return
	}
	r := l.dirty
	if l.fresh {
		r = image.Rect(0, 0, l.w, l.h)
	}
	if r.Empty() {
		return
	}
	var pix []byte
	if r.Dx() == l.w {
		pix = l.pix[r.Min.Y*l.w*4 : r.Max.Y*l.w*4]
	} else {
		row := r.Dx() * 4
		pix = make([]byte, row*r.Dy())
		for y := r.Min.Y; y < r.Max.Y; y++ {
			at := (y*l.w + r.Min.X) * 4
			copy(pix[(y-r.Min.Y)*row:], l.pix[at:at+row])
		}
	}
	if upload(l.w, l.h, r, pix) == nil {
		l.dirty = image.Rectangle{}
		l.fresh = false
	}
}

func (r *Renderer) livePicture(src string) picture {
	v, ok := lives.Load(src[len(livePrefix):])
	if !ok {
		return picture{}
	}
	l := v.(*Live)
	id, ok := r.live[src]
	if !ok {
		id = r.imageID()
		r.live[src] = id
		l.mu.Lock()
		l.fresh = true
		l.mu.Unlock()
	}
	l.take(func(w, h int, at image.Rectangle, pix []byte) error {
		return r.sink.Texture(id, w, h, at.Min.X, at.Min.Y, at.Dx(), at.Dy(), pix)
	})
	return picture{id: id, ok: true}
}
