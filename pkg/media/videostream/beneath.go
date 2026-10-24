package videostream

import (
	"sync"

	"github.com/ygelfand/libcountertop/pkg/display/geometry"
	"github.com/ygelfand/libcountertop/pkg/display/surface"
)

type Beneath struct {
	Rotation func() geometry.Orientation
	Size     func() (int, int)
	Stalled  func()
	Resumed  func()

	mu   sync.Mutex
	c    *surface.Client
	kept uint32
	spec Spec
}

type Spec struct {
	Codec   uint32
	W, H    int
	Session uint32
}

func (b *Beneath) Orientation() geometry.Orientation { return b.Rotation() }
func (b *Beneath) Native() (int, int)                { return b.Size() }

func (b *Beneath) Stalling() {
	if b.Stalled != nil {
		b.Stalled()
	}
}

func (b *Beneath) Resuming() {
	if b.Resumed != nil {
		b.Resumed()
	}
}

func (b *Beneath) Reuse(c *surface.Client, s Spec) (uint32, Spec, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.c != c || b.kept == 0 || b.spec.Codec != s.Codec || b.spec.Session != s.Session {
		return 0, Spec{}, false
	}
	return b.kept, b.spec, true
}

func (b *Beneath) Adopt(c *surface.Client, id uint32, s Spec) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.c != nil && b.kept != 0 && b.kept != id {
		_ = b.c.Destroy(b.kept)
	}
	b.c, b.kept, b.spec = c, id, s
}

func (b *Beneath) Supersede(id uint32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.c != nil && b.kept != 0 && b.kept != id {
		_ = b.c.Destroy(b.kept)
		b.c, b.kept = nil, 0
	}
}

func (b *Beneath) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.c != nil && b.kept != 0 {
		_ = b.c.Destroy(b.kept)
	}
	b.c, b.kept = nil, 0
}
