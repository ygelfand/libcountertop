package widgets

import (
	"testing"
	"time"

	gui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/icons"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
)

func TestKeyboardQRUsesAnIconAndALargerPanel(t *testing.T) {
	if icons.Named("qrcode") == 0 {
		t.Fatal("QR icon is missing")
	}
	for _, size := range [][2]int{{800, 480}, {1280, 800}, {720, 1152}} {
		w := gui.SimpleWindow("keyboard", size[0], size[1], &struct{}{}, func(*gui.Window) {})
		e := NewEditor()
		e.remote.Target = "field"
		opts := KeyboardOptions{
			Size:    KeyboardStandard,
			Palette: theme.Theme{},
			Kit:     style.ByName("standard").Kit,
			Type:    func(rune) {},
			Press:   func(gui.KeyCode) {},
			Cancel:  "Cancel",
			Save:    "Save",
			Pair:    func(string) (string, time.Time, error) { return "", time.Time{}, nil },
		}
		ordinary, _ := e.Keyboard(w, opts)
		e.pairURL, e.pairUntil = "https://192.168.1.25/#control=code", time.Now().Add(time.Minute)
		paired, view := e.Keyboard(w, opts)
		if paired.H <= ordinary.H || paired.Y < 0 {
			t.Fatalf("%v: ordinary=%+v, paired=%+v", size, ordinary, paired)
		}
		layout := gui.GenerateViewLayout(view, w)
		var check func(*gui.Layout)
		check = func(l *gui.Layout) {
			if l.Shape.TC != nil && l.Shape.TC.Text == "QR" {
				t.Fatal("keyboard still uses a text QR key")
			}
			for i := range l.Children {
				check(&l.Children[i])
			}
		}
		check(&layout)
	}
}
