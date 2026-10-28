package menu

import (
	"image"

	gogui "github.com/go-gui-org/go-gui/gui"
)

type Mark struct {
	SVG   string
	Image image.Image
}

type Kind int

type View interface{ Covers() bool }

const (
	Plain Kind = iota
	Chevron
	Toggle
	Slider
	Field
	Segments
	Password
)

type Row struct {
	Glyph string
	Label string
	Hint  string

	Snap func(level int) int

	Value string

	Options []string
	Marks   []Mark

	Kind  Kind
	On    bool
	Level int

	Chosen bool
	Dim    bool

	Preview Preview

	Save func(string)
	Open func() View
}

type Preview interface {
	View(w *gogui.Window, width, height float32) gogui.View
}

type Cell struct {
	Label   string
	Chosen  bool
	Preview Preview
	Edit    func()
	Editor  func() View
}
