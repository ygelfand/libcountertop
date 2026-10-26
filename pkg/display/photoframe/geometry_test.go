package photoframe

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func TestAutoCoversCloseShapesAndContainsOthers(t *testing.T) {
	for _, c := range []struct {
		src  image.Rectangle
		w, h int
		want schema.PhotoFit
	}{
		{image.Rect(0, 0, 1600, 1000), 1280, 800, schema.FitCover},
		{image.Rect(0, 0, 1400, 1000), 1280, 800, schema.FitCover},
		{image.Rect(0, 0, 1000, 1000), 1280, 800, schema.FitContain},
		{image.Rect(0, 0, 1000, 1600), 1280, 800, schema.FitContain},
		{image.Rect(0, 0, 4000, 1000), 1280, 800, schema.FitContain},
	} {
		if got := Resolve(schema.FitAuto, c.src, c.w, c.h); got != c.want {
			t.Errorf("%v on %dx%d: %s, want %s", c.src.Size(), c.w, c.h, got, c.want)
		}
	}
	if got := Resolve(schema.FitFramed, image.Rect(0, 0, 10, 40), 40, 10); got != schema.FitFramed {
		t.Errorf("a chosen fit became %s", got)
	}
}

func TestCoverCropsToTheScreensShape(t *testing.T) {
	for _, c := range []struct {
		src  image.Rectangle
		w, h int
		want image.Rectangle
	}{
		{image.Rect(0, 0, 1440, 1080), 1200, 1920, image.Rect(382, 0, 1057, 1080)},
		{image.Rect(0, 0, 1440, 1080), 1920, 1200, image.Rect(0, 90, 1440, 990)},
		{image.Rect(0, 0, 800, 1280), 1200, 1920, image.Rect(0, 0, 800, 1280)},
	} {
		if got := cover(c.src, c.w, c.h); got != c.want {
			t.Errorf("%v into %dx%d: %v, want %v", c.src, c.w, c.h, got, c.want)
		}
	}
}

func TestWholeFitsShowTheWholePhotoInside(t *testing.T) {
	for _, fit := range []schema.PhotoFit{schema.FitContain, schema.FitBars, schema.FitFramed} {
		p := place(fit, 600, 1200, 1280, 800, "a", 0, 0)
		if p.src != [4]float32{0, 0, 1, 1} {
			t.Errorf("%s samples %v, want the whole photo", fit, p.src)
		}
		x, y, w, h := p.dst[0], p.dst[1], p.dst[2], p.dst[3]
		if x < 0 || y < 0 || x+w > 1280 || y+h > 800 || w/h < 0.49 || w/h > 0.51 {
			t.Errorf("%s draws at %v", fit, p.dst)
		}
	}
	if p := place(schema.FitBars, 600, 1200, 1280, 800, "a", 0, 0); p.back != backSolid {
		t.Errorf("plain edges use %d", p.back)
	}
	if p := place(
		schema.FitFramed,
		600,
		1200,
		1280,
		800,
		"a",
		0,
		0,
	); p.mat <= 0 ||
		p.back != backBlur {
		t.Errorf("framed has mat %v back %d", p.mat, p.back)
	}
}

func TestCoverFillsTheArea(t *testing.T) {
	p := place(schema.FitCover, 1600, 1200, 1280, 800, "a", 0, 0)
	if p.dst != [4]float32{0, 0, 1280, 800} || p.back != backNone {
		t.Fatalf("cover draws at %v back %d", p.dst, p.back)
	}
	if got := p.src[2] * 1600 / (p.src[3] * 1200); got < 1.59 || got > 1.61 {
		t.Errorf("cover samples a %.2f shape, want 1.6", got)
	}
}

func TestSlowPanMovesWithinThePhoto(t *testing.T) {
	first := place(schema.FitKenBurns, 1600, 1200, 1280, 800, "b", 0, 30*time.Second)
	later := place(schema.FitKenBurns, 1600, 1200, 1280, 800, "b", 10*time.Second, 30*time.Second)
	if first.src == later.src {
		t.Error("the window did not move in ten seconds")
	}
	for _, p := range []placement{first, later} {
		u, v, w, h := p.src[0], p.src[1], p.src[2], p.src[3]
		if u < 0 || v < 0 || u+w > 1.0001 || v+h > 1.0001 {
			t.Errorf("window %v leaves the photo", p.src)
		}
	}
	hourly := place(schema.FitKenBurns, 1600, 1200, 1280, 800, "b", 10*time.Second, time.Hour)
	if hourly.src != later.src {
		t.Error("a long span did not cap the leg")
	}
	back := place(schema.FitKenBurns, 1600, 1200, 1280, 800, "b", 50*time.Second, 30*time.Second)
	if back.src != later.src {
		t.Errorf("the second leg does not retrace the first: %v vs %v", back.src, later.src)
	}
}

func TestKenBurnsPlansDiffer(t *testing.T) {
	seen := map[kenPlan]bool{}
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		p := planFor(id)
		if p != planFor(id) {
			t.Fatalf("plan for %q changed between calls", id)
		}
		if p.fromX == p.toX && p.fromY == p.toY {
			t.Errorf("plan for %q does not move", id)
		}
		seen[p] = true
	}
	if len(seen) < 3 {
		t.Errorf("eight photos got only %d different moves", len(seen))
	}
}

func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 255
	}
	return img
}

func TestTexturesFitTheLayer(t *testing.T) {
	sharp := fit(solid(4000, 2000, color.RGBA{1, 2, 3, 255}), 1280)
	if b := sharp.Bounds(); b.Dx() != 1280 || b.Dy() != 640 {
		t.Errorf("sharp texture is %v", b.Size())
	}
	if b := blurred(sharp).Bounds(); b.Dx() != 1280/blurShrink || b.Dy() != 640/blurShrink {
		t.Errorf("blurred texture is %v", b.Size())
	}
	if b := fit(solid(300, 200, color.RGBA{}), 1280).Bounds(); b.Dx() != 300 {
		t.Errorf("a small photo was scaled to %v", b.Size())
	}
}

func TestValuesCarryTheTransition(t *testing.T) {
	now := time.Now()
	s := &Stage{area: ui.Rect{W: 1280, H: 800}, look: Look{
		Back:   theme.Color{R: 255},
		Fit:    schema.FitCover,
		Change: schema.TransitionSlide,
		Box:    image.Rect(100, 100, 300, 200),
	}}
	s.pairs[0] = pair{id: "old", w: 1600, h: 1000, set: true}
	s.pairs[1] = pair{id: "new", w: 1600, h: 1000, set: true}
	s.newest, s.changed = 1, now.Add(-changeFor/2)
	u := s.values(now)
	if u[0] < 0.45 || u[0] > 0.55 || u[3] != 1 || u[2] != 1 {
		t.Errorf("halfway: progress %v, mixing %v, newest %v", u[0], u[3], u[2])
	}
	if u[1] != 2 {
		t.Errorf("slide is transition %v", u[1])
	}
	if u[4] != 1 || u[7] == 0 || u[10] != 200 {
		t.Errorf("background %v, scrim %v, box width %v", u[4], u[7], u[10])
	}
	if u[pairBase+15] != 1 || u[pairBase+pairFloats+15] != 1 {
		t.Error("both photos should be drawn mid-transition")
	}
	u = s.values(now.Add(changeFor))
	if u[0] != 1 || u[3] != 0 || u[pairBase+15] != 0 {
		t.Errorf("after: progress %v, mixing %v, old drawn %v", u[0], u[3], u[pairBase+15])
	}
}
