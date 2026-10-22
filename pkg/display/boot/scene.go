package boot

import (
	"github.com/go-gui-org/go-glyph"
	gui "github.com/go-gui-org/go-gui/gui"

	backend "github.com/ygelfand/libcountertop/pkg/display/gogui"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/surface"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/runtime/startup"
)

func Split(w, h int) (logo, list ui.Rect) {
	if w >= h {
		n := int(float64(w) * 0.42)
		return ui.Rect{W: n, H: h}, ui.Rect{X: n, W: w - n, H: h}
	}
	n := int(float64(h) * 0.42)
	return ui.Rect{W: w, H: n}, ui.Rect{Y: n, W: w, H: h - n}
}
func Skip(w, h int) ui.Rect {
	short := min(w, h)
	high := max(int(float64(short)*0.09), 1)
	pad := int(float64(short) * 0.048)
	return ui.Rect{X: w - 3*high - pad, Y: h - high - 2*pad, W: 3 * high, H: high}
}

type Scene struct {
	c             *surface.Client
	id            uint32
	w             *gui.Window
	r             *backend.Renderer
	width, height int
	palette       theme.Theme
	version, skip string
	progress      []startup.Progress
	listed        bool
}

func NewScene(
	c *surface.Client,
	id uint32,
	nw, nh, w, h, rotation int,
	p theme.Theme,
	version, skip string,
) (*Scene, error) {
	if err := c.UIOpen(id, nw, nh, 2); err != nil {
		return nil, err
	}
	s := &Scene{c: c, id: id, width: w, height: h, palette: p, version: version, skip: skip}
	s.w = gui.SimpleWindow("boot", w, h, s, func(w *gui.Window) { w.SetView(s.view) })
	s.w.SetTheme(bootTheme(p))
	r, err := backend.New(surface.UILayer{C: c, ID: id}, s.w)
	if err != nil {
		_ = c.VideoClose(id)
		return nil, err
	}
	s.r = r
	transparent := gui.Color{}
	r.SetClear(&transparent)
	s.w.Config.OnInit(s.w)
	r.SetRotation(rotation / 90)
	if err = s.Draw(nil, false); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}
func (s *Scene) Close() { _ = s.c.VideoClose(s.id) }
func (s *Scene) Draw(p []startup.Progress, listed bool) error {
	s.progress = startup.Display(p)
	s.listed = listed
	s.w.InvalidateLayout()
	_, err := s.r.Render(s.w)
	return err
}
func box(at ui.Rect, bg theme.Color, content ...gui.View) gui.View {
	return gui.Column(
		gui.ContainerCfg{
			Float:        true,
			FloatOffsetX: float32(at.X),
			FloatOffsetY: float32(at.Y),
			Width:        float32(at.W),
			Height:       float32(at.H),
			Sizing:       gui.FixedFixed,
			Padding:      gui.NoPadding,
			Spacing:      gui.NoSpacing,
			ClipContents: true,
			Color:        widgets.Color(bg),
			Content:      content,
		},
	)
}
func text(value string, size float32, c theme.Color) gui.View {
	return gui.Label(
		value,
		gui.TextStyle{
			Family:   style.ByName("standard").Family,
			Typeface: glyph.TypefaceRegular,
			Size:     max(size, 1),
			Color:    widgets.Color(c),
		},
	)
}
func (s *Scene) view(w *gui.Window) gui.View {
	p := s.palette
	short := min(s.width, s.height)
	items := []gui.View{}
	if s.listed {
		_, list := Split(s.width, s.height)
		items = append(items, box(list, p.Background))
		if len(s.progress) > 0 {
			list = progressArea(s.width, s.height)
			row, per, colw := rowLayout(list, len(s.progress))
			top := list.Y + (list.H-per*row)/2
			for i, r := range s.progress {
				x := list.X + (i/per)*colw + row/2
				y := top + (i%per)*row
				mark := p.Muted
				if r.Failed {
					mark = p.Warning
				} else if r.Done {
					mark = p.Accent
				}
				dot := max(row/5, 1)
				items = append(items, box(ui.Rect{X: x, Y: y + (row-dot)/2, W: dot, H: dot}, mark))
				left := x + dot + row/3
				room := max(colw-row-dot-row/3, 1)
				gap := max(row/16, 1)
				name := ui.Rect{X: left, Y: y + row/12, W: room, H: max(row*7/16, 1)}
				items = append(
					items,
					line(w, name, p.Background, r.Name, min(float32(row)*0.36, 22), p.Text),
				)
				if r.Doing != "" {
					ink := p.Muted
					if r.Failed {
						ink = p.Warning
					}
					doing := ui.Rect{
						X: left,
						Y: name.Y + name.H + gap,
						W: room,
						H: max(row*5/16, 1),
					}
					items = append(
						items,
						line(w, doing, p.Background, r.Doing, min(float32(row)*0.26, 16), ink),
					)
				}
			}
		}
		at := Skip(s.width, s.height)
		items = append(
			items,
			centeredBox(at, p.Surface, text(s.skip, float32(at.H)*0.4, p.Text)),
		)
	}
	size := max(float32(short)*0.024, 1)
	items = append(
		items,
		box(
			ui.Rect{
				X: s.width / 2,
				Y: s.height - int(size*3),
				W: s.width/2 - int(size),
				H: int(size * 2),
			},
			p.Background,
			text(s.version, size, p.Muted),
		),
	)
	return gui.Column(
		gui.ContainerCfg{
			Sizing:  gui.FillFill,
			Padding: gui.NoPadding,
			Spacing: gui.NoSpacing,
			Content: items,
		},
	)
}

func centeredBox(at ui.Rect, bg theme.Color, content ...gui.View) gui.View {
	return gui.Column(
		gui.ContainerCfg{
			Float:        true,
			FloatOffsetX: float32(at.X),
			FloatOffsetY: float32(at.Y),
			Width:        float32(at.W),
			Height:       float32(at.H),
			Sizing:       gui.FixedFixed,
			Padding:      gui.NoPadding,
			Spacing:      gui.NoSpacing,
			ClipContents: true,
			Color:        widgets.Color(bg),
			HAlign:       gui.HAlignCenter,
			VAlign:       gui.VAlignMiddle,
			Content:      content,
		},
	)
}

func bootTheme(p theme.Theme) gui.Theme {
	cfg := gui.ThemeLight.Cfg
	if p.Dark {
		cfg = gui.ThemeDark.Cfg
	}
	cfg.Name = "boot/" + p.Name
	cfg.TextStyleDef.Family = style.ByName("standard").Family
	cfg.TextStyleDef.Color = widgets.Color(p.Text)
	cfg.ColorBackground = widgets.Color(p.Background)
	cfg.ColorPanel = widgets.Color(p.Surface)
	cfg.ColorTextSecondary = widgets.Color(p.Muted)
	cfg.SizeBorder = 0
	return gui.ThemeMaker(cfg)
}

func line(
	w *gui.Window,
	at ui.Rect,
	bg theme.Color,
	value string,
	size float32,
	ink theme.Color,
) gui.View {
	st := gui.TextStyle{
		Family:   style.ByName("standard").Family,
		Typeface: glyph.TypefaceRegular,
		Size:     max(size, 1),
		Color:    widgets.Color(ink),
	}
	if m := w.TextMeasurer(); m != nil {
		for range 4 {
			height := m.FontHeight(st)
			if height <= float32(at.H) || st.Size <= 1 {
				break
			}
			st.Size = max(st.Size*float32(at.H)/height, 1)
		}
	}
	if w.TextWidth(value, st) > float32(at.W) {
		runes := []rune(value)
		for len(runes) > 0 && w.TextWidth(string(runes)+"…", st) > float32(at.W) {
			runes = runes[:len(runes)-1]
		}
		value = string(runes) + "…"
	}
	return box(
		at,
		bg,
		gui.Text(gui.TextCfg{Text: value, TextStyle: st, Mode: gui.TextModeSingleLine, Clip: true}),
	)
}

func progressArea(w, h int) ui.Rect {
	_, list := Split(w, h)
	skip := Skip(w, h)
	gap := max(min(w, h)/40, 1)
	list.H = max(min(list.H, skip.Y-gap-list.Y), 1)
	return list
}

func rowLayout(in ui.Rect, count int) (row, per, width int) {
	columns := 1
	if count*64 > in.H {
		columns = 2
	}
	per = max((count+columns-1)/columns, 1)
	return max(min(64, in.H/per), 1), per, max(in.W/columns, 1)
}
