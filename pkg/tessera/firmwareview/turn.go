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
	turnTime  = 320 * time.Millisecond
	fadeTime  = 240 * time.Millisecond
	holdTime  = time.Second
	cancelled = 0.25
	flick     = 0.6
)

type motion struct {
	old    *backend.Live
	want   func(page int)
	follow func() bool

	mu     sync.Mutex
	kind   schema.TesseraTurn
	step   int
	from   int
	area   image.Rectangle
	frame  image.Point
	ground gogui.Color

	dragging bool
	x0, x    float32
	fdown    bool
	fx       float32
	px       float32
	pat, xat time.Time

	began    time.Time
	at, to   float32
	held     time.Time
	ignoring int
}

func newMotion() *motion { return &motion{old: backend.NewLive("tessera-firmware-old")} }

func (m *motion) start(
	kind schema.TesseraTurn,
	step, from int,
	area image.Rectangle,
	frame image.Point,
	ground gogui.Color,
) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.kind, m.step, m.from, m.area, m.frame, m.ground = kind, step, from, area, frame, ground
	m.dragging = m.fdown && (m.follow == nil || m.follow())
	m.x0, m.x = m.fx, m.fx
	m.began, m.at, m.to, m.held = time.Now(), 0, 1, time.Time{}
}

func (m *motion) skip() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ignoring == 0 {
		return false
	}
	m.ignoring--
	m.kind, m.held = "", time.Time{}
	return true
}

func (m *motion) requests(want func(page int), follow func() bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.want, m.follow = want, follow
}

func (m *motion) track(x float32, down bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if down {
		m.px, m.pat = m.fx, m.xat
		m.fx, m.xat = x, now
	}
	m.fdown = down
	if !m.dragging || m.kind == "" {
		return
	}
	if down {
		m.x = x
		return
	}
	m.dragging = false
	e := m.dragged()
	speed := m.speed()
	m.began, m.at, m.to = now, e, 1
	if (e < cancelled && speed < flick || speed < -flick) && m.want != nil {
		m.to = 0
		m.ignoring++
		m.want(m.from)
	}
}

func (m *motion) speed() float32 {
	w := float32(m.area.Dx())
	secs := float32(m.xat.Sub(m.pat).Seconds())
	if w <= 0 || secs <= 0 || m.pat.IsZero() {
		return 0
	}
	dir := float32(1)
	if m.step < 0 {
		dir = -1
	}
	return dir * (m.px - m.fx) / w / secs
}

func (m *motion) dragged() float32 {
	w := float32(m.area.Dx())
	if w <= 0 {
		return 0
	}
	dir := float32(1)
	if m.step < 0 {
		dir = -1
	}
	return min(max(dir*(m.x0-m.x)/w, 0), 1)
}

func (m *motion) moving() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, live := m.progress()
	return live
}

func (m *motion) length() time.Duration {
	if m.kind == schema.TurnFade || m.kind == schema.TurnZoom {
		return fadeTime
	}
	return turnTime
}

func (m *motion) progress() (float32, bool) {
	if m.kind == "" {
		return 0, false
	}
	if m.dragging {
		return m.dragged(), true
	}
	if !m.held.IsZero() {
		if time.Since(m.held) < holdTime {
			return 0, true
		}
		m.kind, m.held = "", time.Time{}
		return 0, false
	}
	span := float64(m.length()) * math.Abs(float64(m.to-m.at))
	t := 1.0
	if span > 0 {
		t = float64(time.Since(m.began)) / span
	}
	if t >= 1 {
		if m.to == 0 && m.ignoring > 0 {
			m.held = time.Now()
			return 0, true
		}
		m.kind = ""
		return 0, false
	}
	return m.at + (m.to-m.at)*float32(1-math.Pow(1-t, 3)), true
}

type layer struct {
	dx, scale, opacity float32
}

func (m *motion) overlay(at ui.Rect, live string) gogui.View {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.frame.X == 0 || m.frame.Y == 0 {
		return nil
	}
	e, ok := m.progress()
	if !ok {
		return nil
	}
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
