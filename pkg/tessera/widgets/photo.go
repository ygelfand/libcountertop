package widgets

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"slices"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"
	_ "golang.org/x/image/bmp"

	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	kit "github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/fetch"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/tessera"
)

const (
	photoEvery   = 4 * time.Second
	photoPatient = 10 * time.Second
	photoFails   = 3
	photoTimeout = 10 * time.Second
	photoMost    = 32 << 20
)

type photo struct {
	wish    string
	url     string
	img     image.Image
	keys    []string
	fetched time.Time
	asked   time.Time
	busy    bool
	fails   int
}

var photoClient = fetch.Client(photoTimeout)

func fetchPhoto(url string) (image.Image, error) {
	resp, err := photoClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tessera: picture %s: %s", url, resp.Status)
	}
	img, _, err := image.Decode(http.MaxBytesReader(nil, resp.Body, photoMost))
	return img, err
}

func (v *View) photoFor(kind string) *photo {
	v.mu.Lock()
	defer v.mu.Unlock()
	p := v.photos[kind]
	if p == nil {
		p = &photo{}
		v.photos[kind] = p
	}
	return p
}

func (v *View) load(w *gogui.Window, kind, url string) {
	p := v.photoFor(kind)
	v.mu.Lock()
	if p.busy {
		v.mu.Unlock()
		return
	}
	p.busy = true
	v.mu.Unlock()
	safe.Go("tessera picture", func() {
		img, err := fetchPhoto(url)
		v.mu.Lock()
		p.busy, p.fetched = false, time.Now()
		if err != nil {
			p.fails++
		} else {
			for _, k := range p.keys {
				gogui.DropImage(k)
			}
			p.img, p.url, p.fails, p.keys = img, url, 0, nil
		}
		v.mu.Unlock()
		w.QueueCommand(func(w *gogui.Window) { w.InvalidateLayout() })
	})
}

func (v *View) keepFresh(
	w *gogui.Window,
	s *tessera.Screen,
	kind, entity string,
	every time.Duration,
	ask func(),
	now time.Time,
) image.Image {
	p := v.photoFor(kind)
	v.mu.Lock()
	asked, fetched, busy, fails, img := p.asked, p.fetched, p.busy, p.fails, p.img
	v.mu.Unlock()
	got, url, ok := s.Picture(kind)
	switch {
	case asked.IsZero() || fails >= photoFails || (!ok && now.Sub(asked) > photoPatient):
		v.mu.Lock()
		p.asked, p.fails = now, 0
		v.mu.Unlock()
		ask()
	case ok && (entity == "" || got == entity) && !busy && now.Sub(fetched) >= every:
		v.load(w, kind, url)
	}
	v.tick(w, now, time.Second)
	return img
}

func (v *View) want(kind, wish string) bool {
	p := v.photoFor(kind)
	v.mu.Lock()
	same := p.wish == wish
	v.mu.Unlock()
	if same {
		return false
	}
	v.forgetPhoto(kind)
	p = v.photoFor(kind)
	v.mu.Lock()
	p.wish = wish
	v.mu.Unlock()
	return true
}

func (v *View) forgetPhoto(kind string) {
	v.mu.Lock()
	p := v.photos[kind]
	delete(v.photos, kind)
	v.mu.Unlock()
	if p != nil {
		for _, k := range p.keys {
			gogui.DropImage(k)
		}
	}
}

func (v *View) source(kind, name string, img image.Image) string {
	key := fmt.Sprintf("tessera-%s/%s/%p", kind, name, img)
	p := v.photoFor(kind)
	v.mu.Lock()
	if !slices.Contains(p.keys, key) {
		p.keys = append(p.keys, key)
	}
	v.mu.Unlock()
	return kit.ImageSource(key, img)
}

func fitted(img image.Image, box ui.Rect) (int, int) {
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return 0, 0
	}
	scale := min(float64(box.W)/float64(b.Dx()), float64(box.H)/float64(b.Dy()))
	return int(float64(b.Dx()) * scale), int(float64(b.Dy()) * scale)
}

func (v *View) openCamera(t tessera.Tile) {
	v.mu.Lock()
	v.camera = t.Entity
	v.mu.Unlock()
	v.forgetPhoto(tessera.PictureFull)
}

func (v *View) closeCamera(*gogui.Window) {
	v.mu.Lock()
	v.camera = ""
	v.mu.Unlock()
	v.forgetPhoto(tessera.PictureFull)
}

func (v *View) showingCamera() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.camera
}

func (v *View) cameraView(
	w *gogui.Window,
	at ui.Rect,
	s *tessera.Screen,
	t tessera.Tile,
	now time.Time,
) gogui.View {
	reach := style.Reach()
	img := v.keepFresh(w, s, tessera.PictureFull, t.Entity, photoEvery, func() {
		s.AskPicture(
			tessera.PictureFull,
			map[string]string{"entity": t.Entity, "size": "", "bg": "000000"},
		)
	}, now)
	title := gogui.CurrentTheme().Cfg.TextStyleDef
	title.Size *= 1.3
	pal := v.o.UI.Palette()
	box := ui.Rect{W: at.W - int(reach), H: at.H - int(reach*2.5)}
	picture := gogui.Label("", title)
	if img != nil {
		pw, ph := fitted(img, box)
		picture = kit.Picture(v.source(tessera.PictureFull, t.Entity, img), pw, ph)
	}
	return kit.Placed(at, gogui.Column(gogui.ContainerCfg{
		Width:   float32(at.W),
		Height:  float32(at.H),
		Sizing:  gogui.FixedFixed,
		Color:   gogui.RGB(pal.Background.R, pal.Background.G, pal.Background.B),
		Padding: gogui.PadAll(reach * 0.5),
		Spacing: gogui.SpacingPx(reach * 0.5),
		HAlign:  gogui.HAlignCenter,
		Content: []gogui.View{
			gogui.Row(gogui.ContainerCfg{
				Sizing:  gogui.FillFit,
				Padding: gogui.NoPadding,
				Spacing: gogui.SpacingPx(reach * 0.4),
				VAlign:  gogui.VAlignMiddle,
				Content: []gogui.View{
					v.key("tessera-camera-back", gogui.IconArrowLeft, v.closeCamera),
					gogui.Label(t.Name, title),
				},
			}),
			gogui.Column(gogui.ContainerCfg{
				Sizing:  gogui.FillFill,
				Padding: gogui.NoPadding,
				HAlign:  gogui.HAlignCenter,
				VAlign:  gogui.VAlignMiddle,
				Content: []gogui.View{picture},
			}),
		},
	}))
}
