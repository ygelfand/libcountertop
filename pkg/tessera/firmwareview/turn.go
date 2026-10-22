package firmwareview

import (
	"image"
	"math"
	"sync"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	backend "github.com/ygelfand/libcountertop/pkg/display/gogui"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	turnTime = 320 * time.Millisecond
	fadeTime = 240 * time.Millisecond
)

type motion struct {
	old *backend.Live

	mu     sync.Mutex
	kind   schema.TesseraTurn
	began  time.Time
	step   int
	area   image.Rectangle
	frame  image.Point
	ground gogui.Color
}

func newMotion() *motion { return &motion{old: backend.NewLive("tessera-firmware-old")} }

func (m *motion) start(
	kind schema.TesseraTurn,
	step int,
	area image.Rectangle,
	frame image.Point,
	ground gogui.Color,
) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.kind, m.began, m.step, m.area, m.frame, m.ground = kind, time.Now(), step, area, frame, ground
}

func (m *motion) moving() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.kind != "" && time.Since(m.began) < m.length()
}

func (m *motion) length() time.Duration {
	if m.kind == schema.TurnFade || m.kind == schema.TurnZoom {
		return fadeTime
	}
	return turnTime
}

type layer struct {
	dx, scale, opacity float32
}

func (m *motion) overlay(at ui.Rect, live string) gogui.View {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.kind == "" || m.frame.X == 0 || m.frame.Y == 0 {
		return nil
	}
	t := float64(time.Since(m.began)) / float64(m.length())
	if t >= 1 {
		m.kind = ""
		return nil
	}
	e := float32(1 - math.Pow(1-t, 3))
	sx, sy := float32(at.W)/float32(m.frame.X), float32(at.H)/float32(m.frame.Y)
	box := ui.Rect{
		X: int(float32(m.area.Min.X) * sx),
		Y: int(float32(m.area.Min.Y) * sy),
		W: int(float32(m.area.Dx()) * sx),
		H: int(float32(m.area.Dy()) * sy),
	}
	dir := float32(1)
	if m.step < 0 {
		dir = -1
	}
	w := float32(box.W)
	still := layer{scale: 1, opacity: 1}
	var below, above layer
	var oldOnTop bool
	switch m.kind {
	case schema.TurnSlide:
		below = layer{dx: -dir * e * w, scale: 1, opacity: 1}
		above = layer{dx: dir * (1 - e) * w, scale: 1, opacity: 1}
	case schema.TurnCover:
		below = layer{dx: -dir * 0.3 * e * w, scale: 1, opacity: 1 - 0.5*e}
		above = layer{dx: dir * (1 - e) * w, scale: 1, opacity: 1}
	case schema.TurnReveal:
		below, above, oldOnTop = still, layer{dx: -dir * e * w, scale: 1, opacity: 1}, true
	case schema.TurnDeck:
		below = layer{scale: 1 - 0.08*e, opacity: 1 - 0.6*e}
		above = layer{dx: dir * (1 - e) * w, scale: 1, opacity: 1}
	case schema.TurnZoom:
		below = layer{scale: 1 - 0.06*e, opacity: 1 - e}
		above = layer{scale: 1.06 - 0.06*e, opacity: e}
	case schema.TurnFade:
		below, above, oldOnTop = still, layer{scale: 1, opacity: 1 - e}, true
	default:
		return nil
	}
	old, fresh := m.old.Src(), live
	first, second := old, fresh
	if oldOnTop {
		first, second = fresh, old
	}
	return gogui.Column(gogui.ContainerCfg{
		Float:        true,
		FloatOffsetX: float32(box.X),
		FloatOffsetY: float32(box.Y),
		Width:        float32(box.W),
		Height:       float32(box.H),
		Sizing:       gogui.FixedFixed,
		Padding:      gogui.NoPadding,
		ClipContents: true,
		Color:        m.ground,
		Content: []gogui.View{
			picture(first, below, at, box),
			picture(second, above, at, box),
		},
	})
}

func picture(src string, l layer, at, box ui.Rect) gogui.View {
	cx, cy := float32(box.W)/2, float32(box.H)/2
	x := cx + (-float32(box.X)-cx)*l.scale + l.dx
	y := cy + (-float32(box.Y)-cy)*l.scale
	return gogui.Column(gogui.ContainerCfg{
		Float:        true,
		FloatOffsetX: x,
		FloatOffsetY: y,
		Width:        float32(at.W) * l.scale,
		Height:       float32(at.H) * l.scale,
		Sizing:       gogui.FixedFixed,
		Padding:      gogui.NoPadding,
		Content: []gogui.View{gogui.Image(gogui.ImageCfg{
			Src:     src,
			Width:   float32(at.W) * l.scale,
			Height:  float32(at.H) * l.scale,
			Opacity: gogui.SomeF(l.opacity),
		})},
	})
}
