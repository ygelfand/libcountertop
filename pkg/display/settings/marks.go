package settings

import (
	"image"
	"slices"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/icons"
	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type ModeMarks struct {
	Tessera string
	Native  func() image.Image
}

type Mode interface {
	~string
	Label() string
}

func (m ModeMarks) mark(mode string) menu.Mark {
	switch mode {
	case string(config.HomeTessera), string(config.HomeFirmware):
		return menu.Mark{SVG: m.Tessera}
	case string(config.HomeLovelace):
		return menu.Mark{SVG: icons.HomeAssistant}
	case string(config.HomeNative):
		if m.Native != nil {
			return menu.Mark{Image: m.Native()}
		}
	}
	return menu.Mark{}
}

func ModeRow[M Mode](m ModeMarks, label string, modes []M, chosen M) menu.Row {
	labels := make([]string, len(modes))
	marks := make([]menu.Mark, len(modes))
	for i, mode := range modes {
		labels[i] = mode.Label()
		marks[i] = m.mark(string(mode))
	}
	return menu.Row{
		Label:   label,
		Kind:    menu.Segments,
		Options: labels,
		Marks:   marks,
		Level:   max(slices.Index(modes, chosen), 0),
	}
}

func ModePick[M Mode](modes []M, set func(M)) func(int) {
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
