package gogui

import (
	"image"
	"testing"

	"github.com/go-gui-org/go-gui/gui"
)

type upload struct {
	at  image.Rectangle
	len int
}

type rectSink struct {
	sink
	uploads []upload
}

func (s *rectSink) Texture(_ uint32, _, _, x, y, rw, rh int, pix []byte) error {
	s.uploads = append(s.uploads, upload{image.Rect(x, y, x+rw, y+rh), len(pix)})
	return nil
}

func TestALivePictureUploadsWholeOnceThenOnlyWhatChanged(t *testing.T) {
	l := NewLive("gogui-test/live")
	l.Write(16, 8, image.Rect(0, 0, 16, 8), make([]byte, 16*8*4))
	w := gui.SimpleWindow("live", 64, 64, &counter{}, func(w *gui.Window) {
		w.SetView(func(*gui.Window) gui.View {
			return gui.Image(gui.ImageCfg{Src: l.Src(), Width: 16, Height: 8})
		})
	})
	s := &rectSink{}
	r, err := New(s, w)
	if err != nil {
		t.Fatal(err)
	}
	w.Config.OnInit(w)
	base := len(s.uploads)
	render := func() {
		t.Helper()
		if _, err := r.Render(w); err != nil {
			t.Fatal(err)
		}
	}
	render()
	render()
	changed := image.Rect(3, 2, 7, 5)
	l.Write(16, 8, changed, make([]byte, changed.Dx()*changed.Dy()*4))
	render()

	got := s.uploads[base:]
	want := []upload{
		{image.Rect(0, 0, 16, 8), 16 * 8 * 4},
		{changed, changed.Dx() * changed.Dy() * 4},
	}
	if len(got) != len(want) {
		t.Fatalf("uploads %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("upload %d: %v, want %v", i, got[i], want[i])
		}
	}
}
