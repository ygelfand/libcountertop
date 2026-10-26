package doorbellview

import (
	"fmt"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/camera/doorbell"
	"github.com/ygelfand/libcountertop/pkg/display/icons"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/say"
)

const (
	doorShare   = 0.62
	tileAspectW = 4
	tileAspectH = 3
	rowShare    = 1.6
	gapShare    = 0.04
	time12      = "3:04 PM"
	time24      = "15:04"
	otherDay    = "Jan 2, "
)

func (v *View) buildApp(w *gogui.Window) gogui.View {
	vw, vh := w.WindowSize()
	content := []gogui.View{
		widgets.CloseCorner(v.presses, "doorbell-app-close", func() { v.o.Shell.Remove(v.app) }),
		widgets.SettingsCorner(
			v.presses,
			"doorbell-app-settings",
			vw,
			func() { v.o.Shell.Push(v.Page()) },
		),
	}
	top := int(gogui.PadLarge)*2 + int(style.Reach()*1.2)
	content = append(content, v.doors(ui.Rect{W: vw, H: vh}, top)...)
	return v.page(content)
}

func (v *View) doors(whole ui.Rect, top int) []gogui.View {
	doors := v.o.Bell.Doors()
	if len(doors) == 0 {
		return []gogui.View{v.note(whole.W, whole.H, say.T("doorbell.none"))}
	}
	vw, vh := whole.W, whole.H
	gap := int(float32(min(vw, vh)) * gapShare)
	top = max(top, gap)
	area := ui.Rect{X: gap, Y: top, W: vw - 2*gap, H: vh - top - gap}
	recent := v.o.Bell.Recent()
	var list ui.Rect
	if len(recent) > 0 {
		if vw >= vh {
			doorsW := int(float64(area.W) * doorShare)
			list = ui.Rect{X: area.X + doorsW + gap, Y: area.Y, W: area.W - doorsW - gap, H: area.H}
			area.W = doorsW
		} else {
			doorsH := int(float64(area.H) * doorShare)
			list = ui.Rect{X: area.X, Y: area.Y + doorsH + gap, W: area.W, H: area.H - doorsH - gap}
			area.H = doorsH
		}
	}
	var out []gogui.View
	for i, at := range grid(area, len(doors), gap) {
		out = append(out, widgets.Placed(at, v.doorTile(doors[i], at)))
	}
	return append(out, v.history(recent, list, gap)...)
}

func grid(area ui.Rect, n, gap int) []ui.Rect {
	bestCols, bestW, bestH := 1, 0, 0
	for cols := 1; cols <= n; cols++ {
		rows := (n + cols - 1) / cols
		w := (area.W - gap*(cols-1)) / cols
		h := (area.H - gap*(rows-1)) / rows
		if tw, th := fitTile(w, h); tw*th > bestW*bestH {
			bestCols, bestW, bestH = cols, tw, th
		}
	}
	rows := (n + bestCols - 1) / bestCols
	usedW := bestCols*bestW + (bestCols-1)*gap
	usedH := rows*bestH + (rows-1)*gap
	x0, y0 := area.X+(area.W-usedW)/2, area.Y+(area.H-usedH)/2
	out := make([]ui.Rect, n)
	for i := range n {
		col, row := i%bestCols, i/bestCols
		out[i] = ui.Rect{X: x0 + col*(bestW+gap), Y: y0 + row*(bestH+gap), W: bestW, H: bestH}
	}
	return out
}

func fitTile(w, h int) (int, int) {
	if w*tileAspectH > h*tileAspectW {
		return h * tileAspectW / tileAspectH, h
	}
	return w, w * tileAspectH / tileAspectW
}

func (v *View) page(content []gogui.View) gogui.View {
	return gogui.Column(gogui.ContainerCfg{
		ID:      "doorbell-app",
		Sizing:  gogui.FillFill,
		Padding: gogui.NoPadding,
		Color:   gogui.RGB(0, 0, 0),
		Content: content,
	})
}

func (v *View) note(vw, vh int, text string) gogui.View {
	return widgets.Placed(ui.Rect{W: vw, H: vh}, gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFill,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Content: []gogui.View{gogui.Label(text, widgets.Secondary())},
	}))
}

func (v *View) doorTile(d doorbell.Door, at ui.Rect) gogui.View {
	name := v.doorName(d.Slot)
	var inside []gogui.View
	if pic, ok := v.shots.get(doorKey(d.Slot)); ok {
		sw, sh := fitInside(pic.Width, pic.Height, at.W, at.H)
		inside = append(inside, gogui.Image(gogui.ImageCfg{Src: pic.Src, Width: sw, Height: sh}))
	}
	st := gogui.CurrentTheme().TextStyleTitle
	st.Color = gogui.RGB(0xff, 0xff, 0xff)
	inside = append(inside, widgets.Placed(
		ui.Rect{Y: at.H - int(st.Size*2.2), W: at.W, H: int(st.Size * 2.2)},
		gogui.Row(gogui.ContainerCfg{
			Sizing:  gogui.FillFill,
			Color:   gogui.RGBA(0, 0, 0, 0x99),
			HAlign:  gogui.HAlignCenter,
			VAlign:  gogui.VAlignMiddle,
			Padding: gogui.NoPadding,
			Content: []gogui.View{gogui.Label(name, st)},
		}),
	))
	slot := d.Slot
	return v.presses.Pressable(gogui.Column, gogui.ContainerCfg{
		ID:      fmt.Sprintf("doorbell-door-%d", slot),
		Width:   float32(at.W),
		Height:  float32(at.H),
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Radius:  gogui.RadiusLarge,
		Clip:    true,
		Color:   gogui.RGB(0x22, 0x22, 0x22),
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Content: inside,
	}, func(gogui.EventCtx) { v.Watch(slot, false) })
}

func (v *View) history(recent []doorbell.Event, area ui.Rect, gap int) []gogui.View {
	if len(recent) == 0 || area.H <= 0 {
		return nil
	}
	st := gogui.CurrentTheme().Cfg.TextStyleDef
	st.Color = gogui.RGB(0xff, 0xff, 0xff)
	rowH := int(style.Reach() * rowShare)
	fits := max((area.H+gap/2)/(rowH+gap/2), 1)
	var out []gogui.View
	for i, e := range recent[:min(len(recent), fits)] {
		at := ui.Rect{X: area.X, Y: area.Y + i*(rowH+gap/2), W: area.W, H: rowH}
		out = append(out, widgets.Placed(at, v.eventRow(e, at, st)))
	}
	return out
}

func (v *View) eventRow(e doorbell.Event, at ui.Rect, st gogui.TextStyle) gogui.View {
	var cells []gogui.View
	if pic, ok := v.shots.get(eventKey(e)); ok {
		sw, sh := fitInside(pic.Width, pic.Height, at.H*4/3, at.H)
		cells = append(cells, gogui.Image(gogui.ImageCfg{Src: pic.Src, Width: sw, Height: sh}))
	} else {
		icon := gogui.CurrentTheme().TextStyleIconMedium
		icon.Family, icon.Size, icon.Color = icons.Family, float32(at.H)*0.5, st.Color
		cells = append(cells, gogui.Label(icons.Glyph(icons.Named(kindIcon(e.Kind))), icon))
	}
	cells = append(cells,
		gogui.Column(gogui.ContainerCfg{
			Sizing:  gogui.FillFit,
			Padding: gogui.NoPadding,
			Content: []gogui.View{
				gogui.Label(e.Says(), st),
				gogui.Label(v.when(e.At, time.Now()), widgets.Secondary()),
			},
		}),
	)
	slot := e.Slot
	return v.presses.Pressable(gogui.Row, gogui.ContainerCfg{
		ID:      fmt.Sprintf("doorbell-event-%d", e.At.UnixNano()),
		Width:   float32(at.W),
		Height:  float32(at.H),
		Sizing:  gogui.FixedFixed,
		Padding: gogui.PaddingSmall,
		Spacing: gogui.SpacingMedium,
		Radius:  gogui.RadiusMedium,
		Color:   gogui.RGB(0x1a, 0x1a, 0x1a),
		VAlign:  gogui.VAlignMiddle,
		Content: cells,
	}, func(gogui.EventCtx) { v.Watch(slot, false) })
}

func (v *View) when(at, now time.Time) string {
	clock := time12
	if v.o.Clock24 != nil && v.o.Clock24() {
		clock = time24
	}
	y1, m1, d1 := at.Date()
	y2, m2, d2 := now.Date()
	if y1 == y2 && m1 == m2 && d1 == d2 {
		return at.Format(clock)
	}
	return at.Format(otherDay + clock)
}
