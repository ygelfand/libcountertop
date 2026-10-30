package thermostatview

import (
	"strings"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/say"
)

var (
	termGreen = hex(0x3dff86)
	termDim   = hex(0x1f9a50)
)

var terminalLook = look{
	back: hex(0x04140a),
	sheet: sheet{
		shade: rgba(0x000000, 0.7), panel: hex(0x04140a), ink: termGreen,
		muted: termDim, accent: hex(0x04140a), chosen: termGreen,
		family: style.MonoFamily, upper: true, radius: 0.006, edge: termDim,
	},
	draw: drawTerminal,
}

func glow(
	dc *gogui.DrawContext,
	x, base float32,
	s string,
	size float32,
	c gogui.Color,
	fn font,
	a anchor,
) float32 {
	halo := c.WithOpacity(0.16)
	d := max(1, size*0.04)
	for _, o := range [][2]float32{{-d, 0}, {d, 0}, {0, -d}, {0, d}} {
		write(dc, x+o[0], base+o[1], s, size, halo, fn, a)
	}
	return write(dc, x, base, s, size, c, fn, a)
}

func (f *face) termStatus() string {
	switch f.doing() {
	case heating:
		return upper(say.T("thermostat.action.heating")) + " ▲"
	case cooling:
		return upper(say.T("thermostat.action.cooling")) + " ▼"
	}
	return upper(f.action())
}

func drawTerminal(f *face) {
	w, h := f.rw, f.rh
	mono := font{family: style.MonoFamily}
	bold := font{family: style.MonoFamily, bold: true}
	pad := float32(40)
	compact := f.shape == small
	if compact {
		pad = 26
	}
	set := f.setpoint()
	lo, hi := f.span()
	var shown float64
	if set != nil {
		shown = f.eased("set", *set)
	}
	title := "> " + strings.ReplaceAll(upper(f.name()), " ", "-")
	titleSize, titleY := float32(24), float32(64)
	switch f.shape {
	case full:
		titleSize, titleY = 26, 90
	case small:
		titleSize, titleY = 18, 42
	}
	in := f.number(f.st.Current) + "°" + f.letter()
	kinds := f.kinds()
	command := func(k pick) string { return "$ " + strings.ToLower(k.title()) }
	ext := ""
	if out := f.outside(); out != "" {
		ext = "EXT " + out + f.letter()
	}
	if hum := f.humidity(); hum != "" {
		ext = strings.TrimSpace(ext + "  RH " + hum)
	}
	var (
		numX, numY, numSize, degX, degY, degSize float32
		keyX, keyY, keyW, keyH                   float32
		barX, barY, barSize                      float32
		barChars                                 int
		cmdX, cmdY, cmdW, cmdRow, cmdSize        float32
		extX, extY, extSize                      float32
		lines                                    [][2]string
		lineX, lineY, lineSize                   float32
	)
	switch f.shape {
	case full:
		numX, numY, numSize, degX, degY, degSize = 62, 390, 210, 330, 270, 56
		lines, lineX, lineY, lineSize = [][2]string{
			{"INSIDE", in},
			{"STATUS", f.termStatus()},
		}, 70, 460, 22
		keyX, keyY, keyW, keyH = 70, 560, 96, 84
		barX, barY, barSize, barChars = 320, 612, 26, 18
		cmdX, cmdY, cmdW, cmdRow, cmdSize = 760, 210, w-760-70, 60, 24
		extX, extY, extSize = 760, 430, 20
	case tall:
		numX, numY, numSize, degX, degY, degSize = pad-6, 290, 180, pad+228, 190, 48
		lines, lineX, lineY, lineSize = [][2]string{
			{"INSIDE", in},
			{"STATUS", f.termStatus()},
		}, pad, 350, 20
		keyX, keyY, keyW, keyH = pad, 420, 84, 72
		barX, barY, barSize, barChars = pad+2*84+16+20, 420+72*0.62, 22, 12
		cmdX, cmdY, cmdW, cmdRow, cmdSize = pad, 580, w-2*pad, 50, 22
		extX, extY, extSize = pad, 760, 18
	case mid:
		numX, numY, numSize, degX, degY, degSize = pad-6, 290, 180, pad+228, 190, 48
		lines, lineX, lineY, lineSize = [][2]string{
			{"INSIDE", in},
			{"STATUS", f.termStatus()},
		}, pad, 350, 20
		keyX, keyY, keyW, keyH = pad, 470, 84, 72
		barX, barY, barSize, barChars = pad+2*84+16+20, 470+72*0.62, 22, 14
		cmdX, cmdY, cmdW, cmdRow, cmdSize = 430, 150, w-430-pad, 54, 22
		extX, extY, extSize = 430, 380, 18
	default:
		numX, numY, numSize, degX, degY, degSize = pad-4, 170, 120, pad+150, 108, 32
		lines, lineX, lineY, lineSize = [][2]string{{"IN", in}, {"", f.termStatus()}}, pad, 212, 16
		keyX, keyY, keyW, keyH = pad, h-104, 70, 62
		barChars = 8
		if w > 700 {
			barChars = 14
		}
		barX, barY, barSize = pad+172, h-66, 16
		cx := w*0.5 + 10
		if w <= 700 {
			cx = w * 0.5
		}
		cmdX, cmdY, cmdW, cmdRow, cmdSize = cx, 110, w-cx-pad, 44, 18
		extX, extY, extSize = cx, h-40, 16
	}
	filled := int(frac(shown, lo, hi)*float64(barChars) + 0.5)
	bar := "[" + strings.Repeat("█", filled) + strings.Repeat("·", barChars-filled) + "]"
	f.paint(func(dc *gogui.DrawContext) {
		glow(dc, pad, titleY, title, titleSize, termGreen, mono, left)
		labelY := numY - numSize*0.82
		if f.st.Ranged() {
			f.ends(dc, lineX, labelY, lineSize, termGreen, termDim, mono, left)
		} else if f.shape != small {
			glow(dc, lineX, labelY, "TARGET", lineSize, termDim, mono, left)
		}
		glow(dc, numX, numY, f.number(f.setpoint()), numSize, termGreen, bold, left)
		glow(dc, degX, degY, "°"+f.letter(), degSize, termGreen, mono, left)
		for i, l := range lines {
			y := lineY + float32(i)*lineSize*1.6
			if l[0] == "" {
				glow(dc, lineX, y, l[1], lineSize, termGreen, mono, left)
				continue
			}
			glow(dc, lineX, y, l[0], lineSize, termDim, mono, left)
			off := lineSize * 6.5
			if f.shape == small {
				off = lineSize * 2.4
			}
			glow(dc, lineX+off, y, l[1], lineSize, termGreen, mono, left)
		}
		for i, sym := range []string{"−", "+"} {
			kx := keyX + float32(i)*(keyW+16)
			dc.RoundedRect(kx, keyY, keyW, keyH, 6, termGreen, 2)
			dc.FilledRoundedRect(kx, keyY+keyH-8, keyW, 9, 3, termDim)
			glow(dc, kx+keyW/2, keyY+keyH*0.62, sym, keyH*0.46, termGreen, mono, center)
		}
		glow(dc, barX, barY, bar, barSize, termGreen, mono, left)
		dc.FilledRect(cmdX, cmdY-cmdRow*0.6, cmdW, 2, termDim)
		for i, k := range kinds {
			y := cmdY + float32(i)*cmdRow + cmdRow*0.3
			glow(dc, cmdX, y, command(k), cmdSize, termDim, mono, left)
			glow(
				dc,
				cmdX+cmdW,
				y,
				strings.ToLower(f.pickValue(k))+" ▸",
				cmdSize,
				termGreen,
				mono,
				right,
			)
		}
		dc.FilledRect(cmdX, cmdY+float32(len(kinds)-1)*cmdRow+cmdRow*0.7, cmdW, 2, termDim)
		if ext != "" {
			glow(dc, extX, extY, ext, extSize, termDim, mono, left)
		}
		for y := float32(0); y < h; y += 4 {
			dc.FilledRect(0, y, w, 2, gogui.Black.WithOpacity(0.28))
		}
		dc.FilledRectGradient(0, 0, w, h, &gogui.CanvasGradient{
			Radial: true, CX: w / 2, CY: h / 2, R: max(w, h) * 0.75, FX: w / 2, FY: h / 2,
			Stops: []gogui.GradientStop{
				{Color: gogui.Black.WithOpacity(0), Pos: 0.6},
				{Color: gogui.Black.WithOpacity(0.7), Pos: 1},
			},
		})
	})
	blink := f.phase(1100*time.Millisecond) < 0.5
	scan := float32(f.phase(7 * time.Second))
	f.motion(func(dc *gogui.DrawContext) {
		if blink {
			tw := measure(dc, title, titleSize, mono)
			dc.FilledRect(
				pad+tw+titleSize*0.3,
				titleY-titleSize*0.78,
				titleSize*0.55,
				titleSize*0.9,
				termGreen.WithOpacity(0.85),
			)
		}
		band := h * 0.12
		y := -band + (h+band)*scan
		dc.FilledRectGradient(0, max(y, 0), w, band, &gogui.CanvasGradient{
			X1: 0, Y1: y, X2: 0, Y2: y + band, Stops: []gogui.GradientStop{
				{Color: termGreen.WithOpacity(0), Pos: 0},
				{Color: termGreen.WithOpacity(0.05), Pos: 0.5},
				{Color: termGreen.WithOpacity(0), Pos: 1},
			},
		})
	})
	for i := range 2 {
		kx := keyX + float32(i)*(keyW+16)
		f.step("key"+[]string{"-", "+"}[i], rect{kx, keyY, keyW, keyH}, 2*i-1)
	}
	charW := barSize * 0.6
	width := charW * float32(barChars+2)
	f.drag("bar", rect{barX, barY - barSize*1.4, width, barSize * 2},
		func(x, _, _, _ float32, _ float64) float64 {
			k := float64((x - barX - charW) / (charW * float32(barChars)))
			return lo + k*(hi-lo)
		})
	for i, k := range kinds {
		y := cmdY + float32(i)*cmdRow + cmdRow*0.3
		f.chooser("cmd-"+k.title(), rect{cmdX, y - cmdRow*0.7, cmdW, cmdRow}, k)
	}
	if f.st.Ranged() {
		labelY := numY - numSize*0.82
		f.endTap("low", rect{lineX - 6, labelY - lineSize - 6, lineSize * 3.4, lineSize + 14}, Low)
		f.endTap(
			"high",
			rect{lineX + lineSize*3.4, labelY - lineSize - 6, lineSize * 4, lineSize + 14},
			High,
		)
	}
}
