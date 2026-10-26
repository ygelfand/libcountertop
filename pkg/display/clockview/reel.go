package clockview

import (
	"fmt"
	"math"
	"strconv"

	"github.com/go-gui-org/go-glyph"
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
)

const reelSide = 3

func Reel(w *gogui.Window, box ui.Rect, r Reading, ink theme.Theme) gogui.View {
	hour, minute, ok := clockHands(r)
	if !ok {
		return Plain(w, box, r, ink)
	}
	hours, first := 24, 0
	if r.Suffix != "" {
		hours, first = 12, 1
	}
	_, hourAt := turning(w, "reel-hour", strconv.Itoa(hour))
	_, minuteAt := turning(w, "reel-minute", strconv.Itoa(minute))

	bw, bh := float32(box.W), float32(box.H)
	big := min(bh*0.3, bw*0.16)
	small := big * 0.34
	slot := big * 0.95
	rowGap := big * 0.15
	height := 2*big + rowGap
	version := canvasVersion(box, r, ink, false) ^
		uint64(math.Float32bits(hourAt)) ^ uint64(math.Float32bits(minuteAt))<<32
	strips := gogui.DrawCanvas(gogui.DrawCanvasCfg{
		ID: "reel", Width: bw, Height: height, Version: version,
		OnDraw: func(dc *gogui.DrawContext) {
			strip := func(y float32, value, count, base int, at float32) {
				shift := 1 - at
				for i := -reelSide - 1; i <= reelSide+1; i++ {
					p := float64(i) + float64(shift)
					near := float32(max(0, 1-math.Abs(p)))
					size := small + (big-small)*near
					fade := float32(max(0, 1-math.Abs(p)/(reelSide+0.5)))
					if fade <= 0 {
						continue
					}
					n := ((value-base+i)%count+count)%count + base
					text := fmt.Sprintf("%02d", n)
					weight := glyph.TypefaceRegular
					if near > 0.5 {
						weight = glyph.TypefaceBold
					}
					st := Lettering(weight, size, ink.Text.Blend(ink.Muted, float64(1-near)))
					st.Color = st.Color.WithOpacity(fade)
					x := bw/2 + float32(p)*slot*(0.55+0.45*float32(math.Min(1, math.Abs(p))))
					dc.Text(x-dc.TextWidth(text, st)/2, y+(big-dc.FontHeight(st))/2, text, st)
				}
			}
			strip(0, hour, hours, first, hourAt)
			strip(big+rowGap, minute, 60, 0, minuteAt)
		},
	})
	cfg := centred(append([]gogui.View{strips}, dateLine(r, big*0.18, ink)...)...)
	cfg.Spacing = gogui.SpacingPx(big * 0.2)
	return gogui.Column(cfg)
}
