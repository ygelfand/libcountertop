package widgets

import (
	"hash/fnv"

	gogui "github.com/go-gui-org/go-gui/gui"
	"rsc.io/qr"
)

func QR(url string, w, h float32, ink gogui.Color) gogui.View {
	code, err := qr.Encode(url, qr.M)
	if err != nil {
		return gogui.Column(gogui.ContainerCfg{Width: w, Height: h, Sizing: gogui.FixedFixed})
	}
	side := min(w, h)
	scale := max(float32(int(side*0.88/float32(code.Size))), 1)
	quiet := scale * 3
	drawn := float32(code.Size)*scale + 2*quiet
	version := fnv.New64a()
	_, _ = version.Write([]byte(url)) // Hash writes always succeed.
	return gogui.Column(gogui.ContainerCfg{
		Width:   w,
		Height:  h,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Content: []gogui.View{gogui.DrawCanvas(gogui.DrawCanvasCfg{
			Width: drawn, Height: drawn,
			Version: version.Sum64(),
			OnDraw: func(dc *gogui.DrawContext) {
				dc.FilledRoundedRect(0, 0, drawn, drawn, quiet, gogui.RGB(0xff, 0xff, 0xff))
				for y := range code.Size {
					for x := range code.Size {
						if code.Black(x, y) {
							dc.FilledRect(
								quiet+float32(x)*scale,
								quiet+float32(y)*scale,
								scale,
								scale,
								ink,
							)
						}
					}
				}
			},
		})},
	})
}
