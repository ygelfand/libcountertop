package clockview

import (
	"math"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
)

var (
	barsTile  = theme.Color{R: 0xf3, G: 0xf1, B: 0xec}
	barsSeam  = theme.Color{R: 0xd9, G: 0xd5, B: 0xcc}
	barsFrame = theme.Color{R: 0x1b, G: 0x1b, B: 0x1d}
	barsInk   = theme.Color{R: 0x15, G: 0x15, B: 0x15}
)

const (
	up, right, down, left = 0, 90, 180, 270
	rest                  = 225
)

type arms [2]float64

var barDigits = map[byte][6]arms{
	'0': {{right, down}, {left, down}, {up, down}, {up, down}, {up, right}, {up, left}},
	'1': {{rest, rest}, {down, down}, {rest, rest}, {up, down}, {rest, rest}, {up, up}},
	'2': {{right, right}, {left, down}, {right, down}, {left, up}, {up, right}, {left, left}},
	'3': {{right, right}, {left, down}, {right, right}, {up, down}, {right, right}, {up, left}},
	'4': {{down, down}, {down, down}, {up, right}, {up, down}, {rest, rest}, {up, up}},
	'5': {{right, down}, {left, left}, {up, right}, {left, down}, {right, right}, {up, left}},
	'6': {{right, down}, {left, left}, {up, down}, {left, down}, {up, right}, {up, left}},
	'7': {{right, right}, {left, down}, {rest, rest}, {up, down}, {rest, rest}, {up, up}},
	'8': {{right, down}, {left, down}, {up, right}, {up, left}, {up, right}, {up, left}},
	'9': {{right, down}, {left, down}, {up, right}, {up, down}, {right, right}, {up, left}},
}

func Bars(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	hour, minute, ok := split(r)
	if !ok {
		return Plain(w, box, r, ink)
	}
	now := hour + minute
	was, at := turning(w, "bars", now)
	if len(was) != 4 {
		was, at = now, 1
	}
	cols, rows := 8, 3
	if box.H > box.W {
		cols, rows = 4, 6
	}
	bw, bh := float32(box.W), float32(box.H)
	tile := min(bw*0.86/(float32(cols)+0.5), bh*0.6/(float32(rows)+0.5))
	frame := tile * 0.25
	width, height := float32(cols)*tile+2*frame, float32(rows)*tile+2*frame
	version := canvasVersion(box, r, ink, false)
	version ^= uint64(math.Float32bits(at)) ^ uint64(len(was))<<40
	grid := gogui.DrawCanvas(gogui.DrawCanvasCfg{
		ID: "bars", Width: width, Height: height, Version: version,
		OnDraw: func(dc *gogui.DrawContext) {
			dc.FilledRoundedRect(0, 0, width, height, frame*0.4, widgets.Color(barsFrame))
			dc.FilledRect(frame, frame, width-2*frame, height-2*frame, widgets.Color(barsSeam))
			black := widgets.Color(barsInk)
			for n := range 4 {
				gx, gy := n*2, 0
				if cols == 4 {
					gx, gy = (n%2)*2, (n/2)*3
				}
				from, to := barDigits[was[n]], barDigits[now[n]]
				for k := range 6 {
					x := frame + float32(gx+k%2)*tile
					y := frame + float32(gy+k/2)*tile
					dc.FilledRect(
						x+tile*0.015,
						y+tile*0.015,
						tile*0.97,
						tile*0.97,
						widgets.Color(barsTile),
					)
					cx, cy := x+tile/2, y+tile/2
					for a := range 2 {
						deg := sweep(from[k][a], to[k][a], at)
						dc.FilledPolygon(
							blade(cx, cy, -tile*0.07, tile*0.5, tile*0.14, tile*0.14, deg),
							black,
						)
					}
				}
			}
		},
	})
	lines := dateLine(r, tile*0.22, ink)
	if r.Suffix != "" && r.Dated() {
		lines = dateLine(Reading{Date: r.Suffix + " · " + r.Date}, tile*0.22, ink)
	}
	cfg := centred(append([]gogui.View{grid}, lines...)...)
	cfg.Spacing = gogui.SpacingPx(tile * 0.3)
	return gogui.Column(cfg)
}

func sweep(from, to float64, at float32) float64 {
	turn := math.Mod(to-from+360, 360)
	return from + turn*float64(at)
}
