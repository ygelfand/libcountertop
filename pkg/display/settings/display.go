package settings

import (
	"slices"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/say"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func (p *BasicPages) Display() *shell.Page {
	return &shell.Page{
		Title: say.T("display.title"),
		Build: func() ([]menu.Row, []func(int)) {
			cfg := p.state()
			sizes := menu.Steps(len(config.ScreenSizes()))
			auto := cfg.Screen.Mode == config.ModeAuto
			hides := config.DockDelays()
			dock := menu.Steps(len(hides))
			hide := slices.Index(hides, cfg.Screen.DockHide)
			if hide < 0 {
				hide = slices.Index(hides, config.DelayNever)
			}

			rows := []menu.Row{
				{
					Label: say.T("display.brightness"),
					Kind:  menu.Slider,
					Level: cfg.Screen.Backlight,
				},
				{Label: say.T("display.auto"), Kind: menu.Toggle, On: auto},
				{
					Glyph: gogui.IconPalette,
					Label: say.T("display.theme"),
					Kind:  menu.Chevron,
					Value: ThemeSays(cfg.Screen.Theme),
				},
				{
					Label: say.T("display.style"),
					Kind:  menu.Chevron,
					Value: say.T("style." + style.ByName(cfg.Screen.Style).Name),
				},
				{
					Label: say.T("display.size"),
					Kind:  menu.Slider,
					Level: sizes.Level(slices.Index(config.ScreenSizes(), cfg.UISize)),
					Snap:  sizes.Snap,
					Value: say.T("uisize." + cfg.UISize),
				},
				{
					Glyph: gogui.IconMoon,
					Label: say.T("display.idle"),
					Kind:  menu.Chevron,
					Value: cfg.Idle.After.Label(),
				},
				SegmentRow(say.T("controls.dock"), config.Edges(), cfg.Screen.Drawer),
				{
					Label: say.T("controls.dock.hide"),
					Kind:  menu.Slider,
					Level: dock.Level(hide),
					Snap:  dock.Snap,
					Value: hides[hide].Label(),
				},
				SegmentRow(say.T("controls.volume"), config.Sides(), cfg.Screen.Volume),
				{
					Glyph: gogui.IconEye,
					Label: say.T("display.marks"),
					Kind:  menu.Toggle,
					On:    cfg.Screen.Marks,
				},
				{
					Glyph: gogui.IconPicture,
					Label: say.T("photos.page"),
					Kind:  menu.Chevron,
					Value: PosterSays(p.options.Preferences.Poster.Read()),
				},
			}
			acts := []func(int){
				p.options.Screen.SetBacklight,
				func(int) { p.toggleAuto(auto) },
				p.open(func() shell.View { return p.Theme() }),
				p.open(func() shell.View { return p.Style() }),
				func(level int) { p.setSize(config.ScreenSizes()[sizes.Index(level)]) },
				p.open(func() shell.View { return p.options.Idle.Page() }),
				SegmentPick(config.Edges(), p.options.Screen.SetDrawer),
				func(level int) { p.options.Screen.SetDockHide(hides[dock.Index(level)]) },
				SegmentPick(config.Sides(), p.setVolumeEdge),
				func(int) { p.options.Privacy.SetMarks(!cfg.Screen.Marks) },
				p.open(func() shell.View { return p.Photos() }),
			}
			return rows, acts
		},
	}
}
