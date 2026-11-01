package settings

import (
	"image"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/previews"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/visual"
)

func NoneTile(s ui.Surface, at ui.Rect, p theme.Theme) { ui.FillRect(s, at, p.Surface) }

func OffTile(s ui.Surface, at ui.Rect, _ theme.Theme) { ui.FillRect(s, at, theme.Color{}) }

func PhotosTile(s ui.Surface, at ui.Rect, _ theme.Theme) {
	ui.FillRect(s, at, theme.Color{R: 0x5d, G: 0x9c, B: 0xd8})
	sun := min(at.W, at.H) / 5
	ui.FillRounded(s, ui.Rect{X: at.X + at.W*2/3, Y: at.Y + at.H/6, W: sun, H: sun}, sun/2,
		theme.Color{R: 0xff, G: 0xd8, B: 0x6b})
	ground := at.Y + at.H
	ui.FillPolygon(s, []ui.Point{
		{
			X: at.X,
			Y: ground,
		},
		{X: at.X + at.W*2/5, Y: at.Y + at.H*2/5},
		{X: at.X + at.W*3/4, Y: ground},
	}, theme.Color{R: 0x3f, G: 0x6e, B: 0x4a})
	ui.FillPolygon(s, []ui.Point{
		{X: at.X + at.W*2/5, Y: ground}, {X: at.X + at.W*7/10, Y: at.Y + at.H*3/5},
		{X: at.X + at.W, Y: ground},
	}, theme.Color{R: 0x2e, G: 0x55, B: 0x38})
}

func VisualPicker(
	title, empty string,
	none func(),
	chosen func() string,
	pick func(visual.Kind),
	thumbnail func(visual.Kind, int, int) *image.RGBA,
	kit previews.Kit,
) *shell.Page {
	return &shell.Page{
		Title: title,
		Tiles: func() ([]menu.Cell, []func(int)) {
			now := ""
			if chosen != nil {
				now = chosen()
			}
			var cells []menu.Cell
			var acts []func(int)
			if none != nil {
				cells = append(cells, menu.Cell{
					Label:   empty,
					Chosen:  chosen != nil && now == "",
					Preview: kit.Painted("none", NoneTile),
				})
				acts = append(acts, func(int) { none() })
			}
			for _, k := range visual.Built() {
				cells = append(cells, menu.Cell{
					Label:   k.Label(),
					Chosen:  chosen != nil && string(k) == now,
					Preview: kit.Painted("visual/"+string(k), ThumbTile(k, thumbnail)),
				})
				acts = append(acts, func(int) { pick(k) })
			}
			return cells, acts
		},
	}
}

func ThumbTile(
	k visual.Kind,
	thumbnail func(visual.Kind, int, int) *image.RGBA,
) func(ui.Surface, ui.Rect, theme.Theme) {
	return func(s ui.Surface, at ui.Rect, palette theme.Theme) {
		img := thumbnail(k, at.W, at.H)
		if img == nil {
			ui.FillRect(s, at, palette.Surface)
			return
		}
		ui.DrawRGBA(s, at.X, at.Y, img, 1, ui.ClipOf(s))
	}
}
