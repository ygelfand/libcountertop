package clockview

import (
	"testing"

	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
)

func TestCanvasVersionTracksDrawnChanges(t *testing.T) {
	box := ui.Rect{W: 960, H: 480}
	r := Reading{Time: "10:01", Date: "Monday", Second: 1}
	ink := theme.Default()
	base := canvasVersion(box, r, ink, false)
	r.Second = 2
	if canvasVersion(box, r, ink, false) != base {
		t.Fatal("minute clock redraws for seconds")
	}
	if canvasVersion(box, r, ink, true) == base {
		t.Fatal("second hand did not invalidate")
	}
	r.Time = "11:01"
	if canvasVersion(box, r, ink, false) == base {
		t.Fatal("hour correction with same final digit did not invalidate")
	}
	r.Time = "10:01"
	ink.Text.R ^= 1
	if canvasVersion(box, r, ink, false) == base {
		t.Fatal("palette change did not invalidate")
	}
}
