package appearance

import (
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
)

type Size struct {
	Name string
	Set  func(cfg *gogui.ThemeCfg)
}

const (
	SizeMini    = "mini"
	SizeCompact = "compact"
	SizeMedium  = "medium"
	SizeLarge   = "large"
	SizeXLarge  = "xlarge"
)

func large(c *gogui.ThemeCfg) {
	c.SizeTextTiny, c.SizeTextXSmall, c.SizeTextSmall = 16, 20, 24
	c.SizeTextMedium, c.SizeTextLarge, c.SizeTextXLarge = 30, 38, 52
	c.TextStyleDef.Size = 30
	c.SizeSwitchWidth, c.SizeSwitchHeight = 92, 52
	c.SizeRadio = 40
	c.SizeSlider, c.SizeSliderThumb = 10, 40
	c.SizeProgressBar, c.SizeScrollbar = 14, 10
	c.Padding, c.PaddingSmall = gogui.PadAll(14), gogui.PadAll(8)
	c.PaddingMedium, c.PaddingLarge = gogui.PadAll(14), gogui.PadAll(22)
	c.PaddingField, c.PaddingButton = gogui.NewPadding(
		16,
		24,
		16,
		24,
	), gogui.NewPadding(
		16,
		28,
		16,
		28,
	)
	c.SpacingTight, c.SpacingSmall, c.SpacingMedium, c.SpacingLarge = 4, 8, 16, 28
	c.Radius, c.RadiusSmall, c.RadiusMedium, c.RadiusLarge = 12, 8, 12, 18
}

func compact(c *gogui.ThemeCfg) {
	c.SizeTextTiny, c.SizeTextXSmall, c.SizeTextSmall = 12, 14, 17
	c.SizeTextMedium, c.SizeTextLarge, c.SizeTextXLarge = 20, 26, 34
	c.TextStyleDef.Size = 20
	c.SizeSwitchWidth, c.SizeSwitchHeight = 64, 36
	c.SizeRadio = 28
	c.SizeSlider, c.SizeSliderThumb = 6, 26
	c.SizeProgressBar, c.SizeScrollbar = 10, 6
	c.Padding, c.PaddingSmall = gogui.PadAll(10), gogui.PadAll(6)
	c.PaddingMedium, c.PaddingLarge = gogui.PadAll(10), gogui.PadAll(16)
	c.PaddingField, c.PaddingButton = gogui.NewPadding(
		10,
		16,
		10,
		16,
	), gogui.NewPadding(
		10,
		18,
		10,
		18,
	)
	c.SpacingTight, c.SpacingSmall, c.SpacingMedium, c.SpacingLarge = 3, 6, 12, 20
	c.Radius, c.RadiusSmall, c.RadiusMedium, c.RadiusLarge = 9, 6, 9, 12
}

func scaled(base func(*gogui.ThemeCfg), f float32) func(*gogui.ThemeCfg) {
	return func(c *gogui.ThemeCfg) {
		base(c)
		pad := func(p gogui.Padding) gogui.Padding {
			return gogui.NewPadding(p.Top*f, p.Right*f, p.Bottom*f, p.Left*f)
		}
		for _, v := range []*float32{
			&c.SizeTextTiny, &c.SizeTextXSmall, &c.SizeTextSmall, &c.SizeTextMedium, &c.SizeTextLarge, &c.SizeTextXLarge,
			&c.TextStyleDef.Size, &c.SizeSwitchWidth, &c.SizeSwitchHeight, &c.SizeRadio,
			&c.SizeSlider, &c.SizeSliderThumb, &c.SizeProgressBar, &c.SizeScrollbar,
			&c.SpacingTight, &c.SpacingSmall, &c.SpacingMedium, &c.SpacingLarge,
			&c.Radius, &c.RadiusSmall, &c.RadiusMedium, &c.RadiusLarge,
		} {
			*v *= f
		}
		c.Padding, c.PaddingSmall, c.PaddingMedium, c.PaddingLarge = pad(
			c.Padding,
		), pad(
			c.PaddingSmall,
		), pad(
			c.PaddingMedium,
		), pad(
			c.PaddingLarge,
		)
		c.PaddingField, c.PaddingButton = pad(c.PaddingField), pad(c.PaddingButton)
	}
}

var sizes = []Size{
	{Name: SizeMini, Set: scaled(compact, 16.0/20)},
	{Name: SizeCompact, Set: compact},
	{Name: SizeMedium, Set: scaled(compact, 24.0/20)},
	{Name: SizeLarge, Set: large},
	{Name: SizeXLarge, Set: scaled(large, 36.0/30)},
}

func Sizes() []string {
	out := make([]string, 0, len(sizes))
	for _, s := range sizes {
		out = append(out, s.Name)
	}
	return out
}

func sizeNamed(name string) Size {
	for _, s := range sizes {
		if s.Name == name {
			return s
		}
	}
	return sizes[0]
}

func Look(name, palette, size string, custom theme.Custom) gogui.Theme {
	st := style.ByName(name)
	p, ok := theme.Resolve(style.Theme(name, palette), custom)
	if !ok {
		p = theme.Default()
	}

	cfg := gogui.ThemeDark.Cfg
	if !p.Dark {
		cfg = gogui.ThemeLight.Cfg
	}
	cfg.Name = st.Name + "/" + p.Name + "/" + size
	cfg.ColorBackground = widgets.Color(p.Background)
	cfg.ColorPanel = widgets.Color(p.Surface)
	cfg.ColorInterior = widgets.Color(p.Surface)
	cfg.ColorAccent = widgets.Color(p.Accent)
	cfg.ColorSuccess = widgets.Color(p.Success)
	cfg.ColorWarning = widgets.Color(p.Warning)
	cfg.ColorError = widgets.Color(p.Danger)
	cfg.ColorTextSecondary = widgets.Color(p.Muted)
	cfg.TextStyleDef.Color = widgets.Color(p.Text)
	cfg.TextStyleDef.Family = st.Family
	ink, surface := widgets.Color(p.Text), widgets.Color(p.Surface)
	cfg.ColorHover = ink.WithOpacity(0.06).Over(surface)
	cfg.ColorActive = ink.WithOpacity(0.12).Over(surface)
	cfg.ColorSeparator = ink.WithOpacity(0.1).Over(surface)
	cfg.ColorBorder = ink.WithOpacity(0.16).Over(surface)
	cfg.FocusRing = nil
	cfg.ColorFocus = cfg.ColorInterior
	cfg.ColorBorderFocus = cfg.ColorBorder
	cfg.SizeBorder = 0

	sizeNamed(size).Set(&cfg)
	st.Shape(&cfg)
	return gogui.ThemeMaker(cfg)
}
