package homealarmview

import (
	"math"
	"strings"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"
)

func (f *face) phase(period time.Duration) float64 {
	if !f.still {
		f.v.again(f.w)
	}
	return float64(f.now.UnixNano()%int64(period)) / float64(period)
}

func (f *face) breath(period time.Duration) float32 {
	return float32((1 - math.Cos(2*math.Pi*f.phase(period))) / 2)
}

func (f *face) blink(period time.Duration) bool { return f.phase(period) < 0.5 }

func bit(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

const breathPeriod = 3200 * time.Millisecond

func (f *face) dress(cfg *gogui.ContainerCfg, k key) {
	sk := f.sk
	solid := k.fill.A > 0x20
	if !sk.flat && k.radius == 0 {
		cfg.Radius = gogui.RadiusPx(min(k.w, k.h) * 0.12)
	}
	if solid && !sk.flat {
		cfg.Gradient = &gogui.GradientDef{
			Direction: gogui.GradientToBottom,
			Stops: []gogui.GradientStop{
				{Color: mix(k.fill, gogui.White, 0.1), Pos: 0},
				{Color: mix(k.fill, gogui.Black, 0.14), Pos: 1},
			},
		}
	}
	lit := sk.chosen.IsSet() && k.fill == sk.chosen
	glow := k.edge
	if !glow.IsSet() || glow.A == 0 {
		glow = k.ink
	}
	switch {
	case lit:
		b := f.breath(breathPeriod)
		cfg.Shadow = &gogui.BoxShadow{
			Color:      sk.chosen.WithOpacity(0.25 + 0.35*b),
			BlurRadius: k.h * (0.18 + 0.12*b),
		}
	case sk.glow:
		cfg.Shadow = &gogui.BoxShadow{Color: glow.WithOpacity(0.28), BlurRadius: k.h * 0.14}
	case solid && !sk.flat:
		cfg.Shadow = &gogui.BoxShadow{
			Color:      gogui.Black.WithOpacity(0.32),
			OffsetY:    k.h * 0.05,
			BlurRadius: k.h * 0.14,
		}
	}
}

func (f *face) ticking() string {
	if f.blink(time.Second) {
		return f.clock()
	}
	return strings.ReplaceAll(f.clock(), ":", " ")
}

func (f *face) breathe(c, toward gogui.Color) gogui.Color {
	return mix(c, toward, 0.22*f.breath(breathPeriod))
}
