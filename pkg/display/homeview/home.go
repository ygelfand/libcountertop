package homeview

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/settings"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/calendar"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/homecontrol"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/todo"
	"github.com/ygelfand/libcountertop/pkg/say"
	schema "github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

func (view *Renderer) Body(w *gogui.Window) gogui.View {
	master, stop := view.gate(w)
	if stop != nil {
		return Column(stop)
	}

	engine := view.options.Engine
	marks := view.options.Marks
	views := []gogui.View{master}
	if saver := view.options.Saver; saver != nil {
		views = append(views, view.options.UI.Row("saver", "home",
			marks.Row(say.T("home.saver"), schema.SaverModes(), saver.Saver()),
			marks.Pick(schema.SaverModes(), saver.SetSaver), false))
	}
	views = append(views,
		view.options.UI.Row("mode", "home",
			marks.Row(say.T("home.dashboard"), schema.HomeModes(), engine.Speaks()),
			marks.Pick(schema.HomeModes(), engine.SetMode), false),
		view.options.UI.Row("native", "home",
			menu.Row{Label: schema.HomeNative.Label(), Kind: menu.Chevron, Dim: !engine.Native()},
			func(int) { view.options.Shell.Push(view.NativePage()) }, false),
		view.options.UI.Row("tessera", "home",
			menu.Row{Label: schema.HomeTessera.Label(), Kind: menu.Chevron, Dim: !engine.Tessera()},
			func(int) { view.options.Shell.Push(view.TesseraPage()) }, false),
	)
	says := say.T("weather.off")
	if wc := view.options.Weather.Read(); wc.Entity != "" {
		says = wc.Entity
	}
	views = append(views, widgets.Divider(), view.options.UI.Row("weather", "home",
		menu.Row{Label: say.T("weather.title"), Kind: menu.Chevron, Value: says},
		func(int) { view.options.Shell.Push(view.options.WeatherEntities()) }, false))
	if t := view.options.Todo; t != nil {
		list := say.T("todo.off")
		if e := t.Entity(); e != "" {
			list = t.Title(e)
		}
		views = append(views, view.options.UI.Row("todo", "home",
			menu.Row{Label: say.T("todo.title"), Kind: menu.Chevron, Value: list},
			func(int) { view.options.Shell.Push(view.TodoLists()) }, false))
	}
	if c := view.options.Calendar; c != nil {
		chosen := say.T("calendar.off")
		if n := len(c.Chosen()); n > 0 {
			chosen = say.F("calendar.chosen", map[string]any{"N": n})
		}
		views = append(views, view.options.UI.Row("calendar", "home",
			menu.Row{Label: say.T("calendar.app"), Kind: menu.Chevron, Value: chosen},
			func(int) { view.options.Shell.Push(view.CalendarPage()) }, false))
	}
	if page := view.options.Cameras; page != nil {
		views = append(views, view.options.UI.Row("cameras", "home",
			menu.Row{Label: say.T("cameras.app"), Kind: menu.Chevron},
			func(int) { view.options.Shell.Push(page()) }, false))
	}
	return Column(views)
}

func (view *Renderer) CalendarPage() *shell.Page {
	c := view.options.Calendar
	c.Fetch()
	return &shell.Page{
		Title: say.T("calendar.app"),
		Build: func() ([]menu.Row, []func(int)) {
			spans := schema.CalendarSpans()
			steps := menu.Steps(len(spans))
			span := c.Span()
			rows := []menu.Row{{
				Label: say.T("calendar.setting.span"),
				Kind:  menu.Slider,
				Level: steps.Level(slices.Index(spans, span)),
				Snap:  steps.Snap,
				Value: span.Label(),
			}}
			taps := []func(int){func(level int) { c.SetSpan(spans[steps.Index(level)]) }}
			list, err, fetched := c.Offered()
			switch {
			case !fetched:
				return append(rows, menu.Row{Label: say.T("home.loading")}), append(taps, nil)
			case err != nil:
				return append(rows,
						menu.Row{Label: say.T("home.failed"), Hint: err.Error()},
						menu.Row{Label: say.T("home.retry")},
					),
					append(taps, nil, func(int) { c.Fetch() })
			case len(list) == 0:
				return append(rows, menu.Row{Label: say.T("calendar.none")}), append(taps, nil)
			}
			chosen := c.Chosen()
			for _, e := range list {
				rows = append(rows, menu.Row{
					Label:  e.Name,
					Hint:   e.ID,
					Chosen: slices.Contains(chosen, e.ID),
				})
				taps = append(taps, func(int) { c.Toggle(e.ID) })
			}
			return rows, taps
		},
	}
}

func (view *Renderer) Gate(w *gogui.Window) gogui.View {
	if !view.options.Engine.Enabled() {
		return Column([]gogui.View{view.blocked(w, say.T("home.off"), say.T("home.off.hint"), nil)})
	}
	if _, stop := view.gate(w); stop != nil {
		return Column(stop[1:])
	}
	return nil
}

func (view *Renderer) gate(w *gogui.Window) (gogui.View, []gogui.View) {
	ha := view.options.Client
	device := map[string]any{"Name": view.options.Name()}
	on := view.options.Engine.Enabled()
	master := view.options.UI.Row(
		"enabled",
		"home",
		menu.Row{Label: say.T("home.enabled"), Kind: menu.Toggle, On: on},
		func(int) { view.options.Engine.SetEnabled(!on) },
		false,
	)
	switch {
	case !on:
		return master, []gogui.View{master}
	case ha.Access() == homeassistant.Allowed:
		return master, nil
	case !ha.Connected():
		return master, []gogui.View{
			master,
			view.blocked(w, say.T("home.unconnected"), say.F("home.unconnected.hint", device), nil),
		}
	case ha.Access() == homeassistant.Unknown:
		return master, []gogui.View{master, view.blocked(w, say.T("home.checking"), "", nil)}
	case ha.Access() == homeassistant.Refused:
		return master, []gogui.View{
			master,
			view.blocked(
				w,
				say.T("home.refused"),
				say.F("home.refused.hint", device),
				func(int) { ha.Probe() },
			),
		}
	}
	return master, nil
}

func (view *Renderer) blocked(w *gogui.Window, title, hint string, retry func(int)) gogui.View {
	t := gogui.CurrentTheme().Cfg
	_, vh := w.WindowSize()
	side := min(t.TextStyleDef.Size*12, float32(vh)*0.2)
	big := t.TextStyleDef
	big.Size *= 1.3
	views := []gogui.View{
		gogui.Svg(
			gogui.SvgCfg{ID: "esphome", SvgData: view.options.Logo, Width: side, Height: side},
		),
		gogui.Label(title, big),
	}
	if hint != "" {
		views = append(
			views,
			gogui.Text(
				gogui.TextCfg{Text: hint, TextStyle: widgets.Secondary(), Mode: gogui.TextModeWrap},
			),
		)
	}
	if retry != nil {
		views = append(
			views,
			view.options.UI.Row(
				"retry",
				"home",
				menu.Row{Glyph: gogui.IconSync, Label: say.T("home.check")},
				retry,
				true,
			),
		)
	}
	return gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFit,
		HAlign:  gogui.HAlignCenter,
		Padding: gogui.PaddingLarge,
		Spacing: gogui.SpacingLarge,
		Content: views,
	})
}

func Separator() gogui.View {
	gap := gogui.CurrentTheme().Cfg.TextStyleDef.Size * 0.5
	return gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFit,
		Padding: gogui.NewPadding(gap, 0, gap, 0),
		Content: []gogui.View{gogui.Row(gogui.ContainerCfg{
			Height: 2,
			Sizing: gogui.FillFixed,
			Color:  gogui.CurrentTheme().Cfg.ColorBorder,
		})},
	})
}

func Column(views []gogui.View) gogui.View {
	return gogui.Column(
		gogui.ContainerCfg{
			Sizing:  gogui.FillFit,
			Padding: gogui.NoPadding,
			Spacing: gogui.SpacingTight,
			Content: views,
		},
	)
}

func (view *Renderer) Picker(p *homecontrol.Picker) gogui.View {
	s := p.Snapshot()
	search := view.options.UI.Input(gogui.InputCfg{
		ID:            "home-search",
		Text:          s.Query,
		Placeholder:   say.T("home.search"),
		Sizing:        gogui.FillFit,
		OnTextChanged: func(q string, _ gogui.EventCtx) { p.SetQuery(q) },
	}, nil)

	var body []gogui.View
	switch {
	case s.Err != nil:
		body = []gogui.View{
			view.options.UI.Row(
				"failed",
				"picker",
				menu.Row{Label: say.T("home.failed"), Hint: view.failure(s.Err)},
				nil,
				false,
			),
			view.options.UI.Row(
				"retry",
				"picker",
				menu.Row{Glyph: gogui.IconSync, Label: say.T("home.retry")},
				func(int) { p.Retry() },
				true,
			),
		}
		return Column(append([]gogui.View{search}, body...))
	case s.Loading:
		return Column(
			[]gogui.View{
				search,
				view.options.UI.Row(
					"loading",
					"picker",
					menu.Row{Label: say.T("home.loading")},
					nil,
					false,
				),
			},
		)
	}

	var strip []gogui.View
	for _, it := range []struct {
		tab   homecontrol.Tab
		label string
	}{{homecontrol.TabLabels, say.T("home.tab.labels")}, {homecontrol.TabManual, say.T("home.tab.manual")}} {
		st := gogui.CurrentTheme().Cfg.TextStyleDef
		cfg := gogui.ContainerCfg{
			ID:      "home-tab-" + string(it.tab),
			Padding: gogui.PaddingMedium,
			VAlign:  gogui.VAlignMiddle,
		}
		view.options.UI.Toolkit.Tab(&cfg, &st, widgets.Chosen(s.Tab == it.tab))
		cfg.Content = []gogui.View{gogui.Label(it.label, st)}
		pick := it.tab
		strip = append(
			strip,
			view.options.UI.Toolkit.Pressable(
				gogui.Row,
				cfg,
				func(gogui.EventCtx) { p.SetTab(pick) },
			),
		)
	}
	shown := view.labelsTab(p, s)
	if s.Tab == homecontrol.TabManual {
		shown = view.manualTab(p, s)
	}
	tabs := gogui.Row(
		gogui.ContainerCfg{
			Sizing:  gogui.FillFit,
			Padding: gogui.NoPadding,
			Spacing: gogui.SpacingSmall,
			Content: strip,
		},
	)
	return Column([]gogui.View{search, tabs, shown})
}

func (view *Renderer) failure(err error) string {
	device := map[string]any{"Name": view.options.Name()}
	switch {
	case errors.Is(err, homeassistant.ErrNotAllowed):
		return say.F("home.refused.hint", device)
	case errors.Is(err, homeassistant.ErrNotConnected):
		return say.F("home.unconnected.hint", device)
	}
	return ""
}

func (view *Renderer) labelsTab(p *homecontrol.Picker, s homecontrol.Snapshot) gogui.View {
	if len(s.Labels) == 0 {
		return Column(
			[]gogui.View{
				view.options.UI.Row(
					"empty",
					"labels",
					menu.Row{Label: say.T("home.empty")},
					nil,
					false,
				),
			},
		)
	}
	var views []gogui.View
	for i, l := range s.Labels {
		views = append(views, view.options.UI.Row(fmt.Sprintf("label-%d", i), "labels", menu.Row{
			Label: l.Name, Hint: say.F("home.label.count", map[string]any{"N": l.Count}),
			Kind: menu.Toggle, On: l.On, Chosen: l.On,
		}, func(int) { p.ToggleLabel(l.ID) }, false))
	}
	return Column(views)
}

func (view *Renderer) manualTab(p *homecontrol.Picker, s homecontrol.Snapshot) gogui.View {
	views := []gogui.View{view.options.UI.Row("all", "manual", menu.Row{
		Label: say.T("home.everything." + p.Sel.Key),
		Kind:  menu.Toggle, On: s.Pick.All, Chosen: s.Pick.All,
	}, func(int) { p.ToggleAll() }, false)}
	if len(s.Areas) == 0 {
		views = append(
			views,
			view.options.UI.Row(
				"empty",
				"manual",
				menu.Row{Label: say.T("home.empty")},
				nil,
				false,
			),
		)
	}
	for i, a := range s.Areas {
		views = append(views, view.areaHeader(p, s, i, a))
		if !a.Open {
			continue
		}
		for j, e := range a.Entities {
			name := e.Name
			if name == "" {
				name = e.ID
			}
			row := view.options.UI.Row(fmt.Sprintf("entity-%d-%d", i, j), "manual", menu.Row{
				Label: name, Kind: menu.Toggle, On: e.On, Chosen: e.Included, Dim: !e.Available(),
			}, func(int) { p.ToggleEntity(e.ID) }, false)
			views = append(views, gogui.Row(gogui.ContainerCfg{
				Sizing:  gogui.FillFit,
				Padding: gogui.NewPadding(0, 0, 0, reach()),
				Content: []gogui.View{row},
			}))
		}
	}
	return Column(views)
}

func (view *Renderer) areaHeader(
	p *homecontrol.Picker,
	s homecontrol.Snapshot,
	i int,
	a homecontrol.AreaRow,
) gogui.View {
	t := gogui.CurrentTheme().Cfg
	name := a.Name
	if a.ID == "" {
		name = say.T("home.noarea")
	}
	glyph := gogui.IconArrowRight
	if a.Open {
		glyph = gogui.IconArrowDown
	}
	id := fmt.Sprintf("area-%d", i)
	opener := view.options.UI.Toolkit.Pressable(gogui.Row, gogui.ContainerCfg{
		ID:      id + "-open",
		Sizing:  gogui.FillFit,
		VAlign:  gogui.VAlignMiddle,
		Padding: gogui.NoPadding,
		Spacing: gogui.SpacingMedium,
		Content: []gogui.View{
			icon(glyph, t.ColorTextSecondary),
			gogui.Column(
				gogui.ContainerCfg{
					Sizing:  gogui.FillFit,
					Padding: gogui.NoPadding,
					Content: []gogui.View{
						gogui.Label(name, gogui.TextStyle{}),
						gogui.Label(
							say.F(
								"home.area.count",
								map[string]any{"N": a.Included, "Of": len(a.Entities)},
							),
							widgets.Secondary(),
						),
					},
				},
			),
		},
	}, func(gogui.EventCtx) { p.Expand(a.ID) })

	content := []gogui.View{opener}
	if a.ID != "" {
		content = append(content, view.options.UI.Toolkit.Pressable(gogui.Row, gogui.ContainerCfg{
			ID:      id + "-switch",
			Padding: gogui.NoPadding,
			VAlign:  gogui.VAlignMiddle,
			Content: []gogui.View{view.options.UI.Toolkit.Kit().Toggle(id+"-sw", a.On)},
		}, func(gogui.EventCtx) { p.ToggleArea(a.ID) }))
	}
	cfg := gogui.ContainerCfg{
		ID:      id,
		Sizing:  gogui.FillFit,
		VAlign:  gogui.VAlignMiddle,
		Padding: gogui.PaddingMedium,
		Spacing: gogui.SpacingMedium,
		Radius:  gogui.RadiusMedium,
		Content: content,
	}
	state := style.Rest
	switch {
	case a.On || s.Pick.All:
		state = style.Chosen
	case a.Included > 0:
		state = style.Partial
	}
	view.options.UI.Toolkit.ListRow(&cfg, state)
	return gogui.Row(cfg)
}

type Client interface {
	Connected() bool
	Access() homeassistant.Access
	Probe()
}
type Dependencies struct {
	Engine          *homecontrol.Engine
	Client          Client
	Name            func() string
	Weather         storage.Section[schema.Weather]
	WeatherEntities func() shell.View
	Todo            *todo.Todo
	Calendar        *calendar.Calendar
	Cameras         func() shell.View
	Shell           *shell.Shell
	Logo            string
	Marks           settings.ModeMarks
	Saver           SaverMode
	UI              *widgets.UI
}

type FirmwareSettings interface {
	Switch(id string) (*esphome.Switch, bool)
	Number(id string) (*esphome.Number, bool)
	Clock(id string) (*esphome.Text, bool)
}

type SaverMode interface {
	Saver() schema.HomeMode
	SetSaver(schema.HomeMode)
}

type Listener interface {
	Hint() (title, body string)
	Turns() bool
}

type Renderer struct {
	options Dependencies
	tessera  Listener
	firmware FirmwareSettings
	grid     gridDraft
}

const gridSettle = 400 * time.Millisecond

type gridDraft struct {
	mu    sync.Mutex
	held  *schema.TesseraGrid
	timer *time.Timer
}

func (d *gridDraft) move(g schema.TesseraGrid, commit func(schema.TesseraGrid)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.held = &g
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(gridSettle, d.land(commit))
}

func (d *gridDraft) land(commit func(schema.TesseraGrid)) func() {
	return func() {
		d.mu.Lock()
		g := d.held
		d.mu.Unlock()
		if g == nil {
			return
		}
		commit(*g)
		d.mu.Lock()
		if d.held == g {
			d.held = nil
		}
		d.mu.Unlock()
	}
}

func (d *gridDraft) shown(saved schema.TesseraGrid) schema.TesseraGrid {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.held == nil {
		return saved
	}
	return d.held.Or(saved)
}

func (view *Renderer) Listen(l Listener) { view.tessera = l }

func (view *Renderer) UseFirmware(f FirmwareSettings) { view.firmware = f }

func New(o Dependencies) *Renderer { return &Renderer{options: o} }
func reach() float32               { return style.Reach() }
func icon(glyph string, c gogui.Color) gogui.View {
	st := gogui.CurrentTheme().TextStyleIconMedium
	st.Color = c
	return gogui.Label(glyph, st)
}

func (view *Renderer) NativePage() *shell.Page {
	return &shell.Page{
		Title: schema.HomeNative.Label(),
		Build: func() ([]menu.Row, []func(int)) {
			engine := view.options.Engine
			var (
				rows []menu.Row
				taps []func(int)
			)
			for _, s := range engine.Selections() {
				rows = append(rows, menu.Row{
					Label: s.Name(),
					Kind:  menu.Chevron,
					Value: homecontrol.Summary(s.Pick()),
				})
				taps = append(taps, func(int) { view.options.Shell.Push(engine.SelectionPage(s)) })
			}
			rows = append(rows, menu.Row{Label: say.T("home.refresh")})
			taps = append(taps, func(int) { engine.Refresh() })
			return rows, taps
		},
	}
}

func (view *Renderer) TodoLists() *shell.Page {
	t := view.options.Todo
	t.Fetch()
	return &shell.Page{
		Title: say.T("todo.entity"),
		Build: func() ([]menu.Row, []func(int)) {
			shown := t.Completed()
			rows := []menu.Row{
				{Label: say.T("todo.setting.completed"), Kind: menu.Toggle, On: shown},
			}
			taps := []func(int){func(int) { t.SetCompleted(!shown) }}
			list, err, fetched := t.Offered()
			switch {
			case !fetched:
				return append(rows, menu.Row{Label: say.T("home.loading")}), append(taps, nil)
			case err != nil:
				return append(rows,
						menu.Row{Label: say.T("home.failed"), Hint: err.Error()},
						menu.Row{Label: say.T("home.retry")},
					),
					append(taps, nil, func(int) { t.Fetch() })
			case len(list) == 0:
				return append(rows, menu.Row{Label: say.T("todo.none")}), append(taps, nil)
			}
			now := t.Entity()
			for _, e := range list {
				rows = append(rows, menu.Row{Label: e.Name, Hint: e.ID, Chosen: e.ID == now})
				taps = append(taps, func(int) { t.SetEntity(e.ID) })
			}
			return rows, taps
		},
	}
}

func (view *Renderer) TesseraPage() *shell.Page {
	return &shell.Page{
		Title: schema.HomeTessera.Label(),
		Build: func() ([]menu.Row, []func(int)) {
			engine := view.options.Engine
			g := view.grid.shown(engine.Grid())
			back := engine.ReturnHome()
			rows := []menu.Row{
				gridRow(say.T("home.tessera.columns"), g.Columns),
				gridRow(say.T("home.tessera.rows"), g.Rows),
				{Label: say.T("home.tessera.return"), Kind: menu.Toggle, On: back},
			}
			taps := []func(int){
				func(level int) {
					c := gridOf(level)
					r := min(g.Rows, schema.TesseraCells/c)
					view.grid.move(schema.TesseraGrid{Columns: c, Rows: r}, engine.SetGrid)
				},
				func(level int) {
					r := gridOf(level)
					c := min(g.Columns, schema.TesseraCells/r)
					view.grid.move(schema.TesseraGrid{Columns: c, Rows: r}, engine.SetGrid)
				},
				func(int) { engine.SetReturnHome(!back) },
			}
			if t := view.tessera; t != nil && t.Turns() {
				wide, tall := engine.Landscape(), engine.Portrait()
				rows = append(rows,
					menu.Row{Label: say.T("home.tessera.landscape"), Kind: menu.Toggle, On: wide},
					menu.Row{Label: say.T("home.tessera.portrait"), Kind: menu.Toggle, On: tall},
				)
				taps = append(taps,
					func(int) { engine.SetLandscape(!wide) },
					func(int) { engine.SetPortrait(!tall) },
				)
			}
			if engine.Firmware() {
				rows = append(rows, menu.Row{Label: schema.HomeFirmware.Label(), Kind: menu.Chevron})
				taps = append(taps, func(int) { view.options.Shell.Push(view.firmwarePage()) })
			}
			if t := view.tessera; t != nil && engine.Tessera() {
				if title, body := t.Hint(); title != "" {
					rows = append([]menu.Row{{Label: title, Hint: body}}, rows...)
					taps = append([]func(int){nil}, taps...)
				}
			}
			return rows, taps
		},
	}
}

func gridRow(label string, n int) menu.Row {
	return menu.Row{
		Label: label,
		Kind:  menu.Slider,
		Level: gridLevel(n),
		Snap:  func(level int) int { return gridLevel(gridOf(level)) },
		Value: strconv.Itoa(n),
	}
}

func gridOf(level int) int {
	return min(max(1+(level*(schema.TesseraMost-1)+50)/100, 1), schema.TesseraMost)
}

func gridLevel(n int) int { return (n - 1) * 100 / (schema.TesseraMost - 1) }
