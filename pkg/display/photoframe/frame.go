package photoframe

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"sync"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"
	_ "golang.org/x/image/webp"

	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/hook"
	"github.com/ygelfand/libcountertop/pkg/media/photos"
)

const (
	historyMost = 12
	tapShare    = 8
)

type Source interface {
	Next() (photos.Picture, bool)
}

type Frame struct {
	Changed hook.Hook[int]

	name string
	src  Source

	mu      sync.Mutex
	history []photos.Picture
	at      int
	img     image.Image
	shownAt time.Time
	version int
}

type Shot struct {
	ID      string
	Image   image.Image
	Version int
	Shown   time.Time
}

func New(name string, src Source) *Frame { return &Frame{name: name, src: src, at: -1} }

func (f *Frame) Due(now time.Time, every time.Duration) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.img == nil || every > 0 && now.Sub(f.shownAt) >= every
}

func (f *Frame) Next(now time.Time) bool {
	f.mu.Lock()
	if f.at+1 < len(f.history) {
		at := f.at + 1
		pic := f.history[at]
		f.mu.Unlock()
		return f.show(pic, at, now)
	}
	f.mu.Unlock()
	pic, ok := f.src.Next()
	if !ok {
		return false
	}
	img, err := Decode(pic.Data)
	if err != nil {
		slog.Warn("decoding a photo failed", "frame", f.name, "photo", pic.ID, "err", err)
		return false
	}
	f.mu.Lock()
	f.history = append(f.history, pic)
	if len(f.history) > historyMost {
		f.history = f.history[len(f.history)-historyMost:]
	}
	f.put(img, len(f.history)-1, now)
	v := f.version
	f.mu.Unlock()
	f.Changed.Emit(v)
	return true
}

func (f *Frame) Previous(now time.Time) bool {
	f.mu.Lock()
	if f.at <= 0 {
		f.mu.Unlock()
		return false
	}
	at := f.at - 1
	pic := f.history[at]
	f.mu.Unlock()
	return f.show(pic, at, now)
}

func (f *Frame) Drop() {
	f.mu.Lock()
	had := f.img != nil
	f.img = nil
	f.version++
	v := f.version
	f.mu.Unlock()
	if had {
		f.Changed.Emit(v)
	}
}

func (f *Frame) Showing() (photos.Picture, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.img == nil || f.at < 0 || f.at >= len(f.history) {
		return photos.Picture{}, false
	}
	pic := f.history[f.at]
	pic.Data = nil
	return pic, true
}

func (f *Frame) Current() (Shot, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.img == nil {
		return Shot{Version: f.version}, false
	}
	s := Shot{Image: f.img, Version: f.version, Shown: f.shownAt}
	if f.at >= 0 && f.at < len(f.history) {
		s.ID = f.history[f.at].ID
	}
	return s, true
}

func (f *Frame) show(pic photos.Picture, at int, now time.Time) bool {
	img, err := Decode(pic.Data)
	if err != nil {
		slog.Warn("decoding a photo failed", "frame", f.name, "photo", pic.ID, "err", err)
		return false
	}
	f.mu.Lock()
	f.put(img, at, now)
	v := f.version
	f.mu.Unlock()
	f.Changed.Emit(v)
	return true
}

func (f *Frame) put(img image.Image, at int, now time.Time) {
	f.img, f.at, f.shownAt = img, at, now
	f.version++
}

func Decode(data []byte) (image.Image, error) {
	img, kind, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("photoframe: decoding %d bytes: %w", len(data), err)
	}
	if b := img.Bounds(); b.Dx() < 2 || b.Dy() < 2 {
		return nil, fmt.Errorf("photoframe: a %dx%d %s", b.Dx(), b.Dy(), kind)
	}
	return img, nil
}

func Taps(area ui.Rect, page func(forward bool)) []gogui.View {
	side := min(max(area.W/tapShare, int(style.Reach())), area.W/2)
	zone := func(x int, forward bool) gogui.View {
		return widgets.Placed(
			ui.Rect{X: x, Y: area.Y, W: side, H: area.H},
			gogui.Column(gogui.ContainerCfg{
				Sizing:  gogui.FillFill,
				Padding: gogui.NoPadding,
				OnClick: func(e gogui.EventCtx) {
					page(forward)
					e.Consume()
				},
			}),
		)
	}
	return []gogui.View{zone(area.X, false), zone(area.X+area.W-side, true)}
}
