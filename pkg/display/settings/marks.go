package settings

import (
	"image"
	"slices"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type ModeMarks struct {
	Tessera string
	Native  func() image.Image
}

func (m ModeMarks) For(modes []config.HomeMode) []menu.Mark {
	out := make([]menu.Mark, len(modes))
	for i, mode := range modes {
		switch mode {
		case config.HomeTessera, config.HomeFirmware:
			out[i].SVG = m.Tessera
		case config.HomeNative:
			if m.Native != nil {
				out[i].Image = m.Native()
			}
		}
	}
	return out
}

func (m ModeMarks) Row(label string, modes []config.HomeMode, chosen config.HomeMode) menu.Row {
	labels := make([]string, len(modes))
	for i, mode := range modes {
		labels[i] = mode.Label()
	}
	return menu.Row{
		Label:   label,
		Kind:    menu.Segments,
		Options: labels,
		Marks:   m.For(modes),
		Level:   max(slices.Index(modes, chosen), 0),
	}
}

func (m ModeMarks) Pick(modes []config.HomeMode, set func(config.HomeMode)) func(int) {
	return func(i int) { set(modes[i]) }
}

func Themed(light, dark func() image.Image) func() image.Image {
	return func() image.Image {
		bg := gogui.CurrentTheme().Cfg.ColorBackground
		if theme.Dark(theme.Color{R: bg.R, G: bg.G, B: bg.B}) {
			return dark()
		}
		return light()
	}
}
