package widgets

import (
	"testing"

	gogui "github.com/go-gui-org/go-gui/gui"
)

func TestQRContentChangesInvalidateCanvas(t *testing.T) {
	version := func(value string) uint64 {
		w := gogui.SimpleWindow("qr", 128, 128, &struct{}{}, func(*gogui.Window) {})
		view := QR(value, 128, 128, gogui.RGB(0, 0, 0))
		layout := gogui.GenerateViewLayout(view, w)
		if len(layout.Children) != 1 || layout.Children[0].Shape == nil {
			t.Fatal("QR did not produce one canvas")
		}
		return layout.Children[0].Shape.Version
	}
	first := "https://example.com/a"
	second := "https://example.com/b"
	want := version(first)
	if got := version(second); got == want {
		t.Error("different URLs of equal length reused the same canvas version")
	}
	if got := version(first); got != want {
		t.Error("the same URL changed the canvas version")
	}
}
