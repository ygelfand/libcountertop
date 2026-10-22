package homecontrol

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type Selection struct {
	Key    string
	Filter homeassistant.Filter
	engine *Engine
	read   func() config.HomePick
	write  func(config.HomePick) error
}

func (e *Engine) Choice(
	key string,
	f homeassistant.Filter,
	read func() config.HomePick,
	write func(config.HomePick) error,
) Selection {
	return Selection{Key: key, Filter: f, engine: e, read: read, write: write}
}

func (s Selection) Name() string { return say.T("home.selection." + s.Key) }

func (s Selection) Pick() config.HomePick {
	if s.read != nil {
		return s.read().Clone()
	}
	return s.engine.options.Settings.Read().Picked(s.Key).Clone()
}

func Includes(p config.HomePick, e homeassistant.Entity) bool {
	return p.All ||
		slices.Contains(p.Entities, e.ID) ||
		(e.AreaID != "" && slices.Contains(p.Areas, e.AreaID)) ||
		slices.ContainsFunc(e.Labels, func(l string) bool { return slices.Contains(p.Labels, l) })
}

func Summary(p config.HomePick) string {
	switch {
	case p.All:
		return say.T("home.all")
	case p.Empty():
		return say.T("home.none")
	}
	var parts []string
	for _, part := range []struct {
		kind string
		n    int
	}{
		{"labels", len(p.Labels)},
		{"areas", len(p.Areas)},
		{"entities", len(p.Entities)},
	} {
		if part.n > 0 {
			parts = append(parts, say.F("home.picked."+part.kind, map[string]any{"N": part.n}))
		}
	}
	return strings.Join(parts, ", ")
}

func (s Selection) Controlled() bool {
	return s.engine.Native() && s.engine.options.Settings.Read().Control[s.Key]
}

func (e *Engine) Enabled() bool { return e.options.Settings.Read().Enabled }

func (e *Engine) SetEnabled(on bool) { e.set(enabled, setting.OnOff(on)) }

func (e *Engine) Speaks() config.HomeMode {
	h := e.options.Settings.Read()
	if !h.Enabled {
		return ""
	}
	return h.Speaks()
}

func (e *Engine) Native() bool { return e.Speaks() == config.HomeNative }

func (e *Engine) Tessera() bool { return e.Speaks().Tessera() }

func (e *Engine) Firmware() bool { return e.Speaks() == config.HomeFirmware }

func (e *Engine) SetMode(m config.HomeMode) { e.set(dashboardMode, string(m)) }

func (e *Engine) Grid() config.TesseraGrid {
	return e.options.Settings.Read().Tessera.Or(e.options.Grid)
}

func (e *Engine) SetGrid(g config.TesseraGrid) {
	g = g.Or(e.Grid())
	e.set(tesseraColumns, strconv.Itoa(g.Columns))
	e.set(tesseraRows, strconv.Itoa(g.Rows))
}

func (s Selection) GroupFrom() int { return s.engine.options.Settings.Read().Grouped(s.Key) }

func (s Selection) SetGroupFrom(n int) {
	s.engine.set("group."+s.Key, strconv.Itoa(min(max(n, 0), config.HomeGroupMost)))
}

func (s Selection) SetControlled(on bool) {
	s.engine.set("control."+s.Key, setting.OnOff(on))
}

func (e *Engine) Combined() bool {
	return e.options.Settings.Read().Combine && e.selections[0].Controlled()
}

func (e *Engine) SetCombined(on bool) { e.set(combine, setting.OnOff(on)) }

func (e *Engine) SelectionPage(s Selection) *shell.Page {
	return &shell.Page{
		Title: s.Name(),
		Build: func() ([]menu.Row, []func(int)) {
			on, from := s.Controlled(), s.GroupFrom()
			rows := []menu.Row{
				{Label: say.T("home.enable"), Kind: menu.Toggle, On: on},
				{Label: say.T("home.devices"), Kind: menu.Chevron, Value: Summary(s.Pick())},
				{
					Label: say.T("home.group"),
					Hint:  groupHint(from),
					Kind:  menu.Slider,
					Level: groupLevel(from),
					Snap:  groupSnap,
					Value: strconv.Itoa(from),
				},
			}
			taps := []func(int){
				func(int) { s.SetControlled(!on) },
				func(int) { e.options.Shell.Push(e.NewPicker(s)) },
				func(level int) { s.SetGroupFrom(groupOf(level)) },
			}
			if s.Key == e.selections[0].Key {
				combine := e.options.Settings.Read().Combine
				rows = append(
					rows,
					menu.Row{Label: say.T("home.combine"), Kind: menu.Toggle, On: combine},
				)
				taps = append(taps, func(int) { e.SetCombined(!combine) })
			}
			return rows, taps
		},
	}
}

func groupOf(level int) int {
	return min(max((level*config.HomeGroupMost+50)/100, 0), config.HomeGroupMost)
}

func groupLevel(n int) int { return n * 100 / config.HomeGroupMost }

func groupSnap(level int) int { return groupLevel(groupOf(level)) }

func groupHint(n int) string {
	switch n {
	case 0:
		return say.T("home.group.none")
	case 1:
		return say.T("home.group.all")
	}
	return say.F("home.group.from", map[string]any{"N": n})
}

type Home struct{}

func (e *Engine) Page() *Home        { return e.home }
func (*Home) Covers() bool           { return true }
func (*Home) Timeout() time.Duration { return shell.SettingsTimeout }

type Dashboard struct{}

func (e *Engine) Dashboard() *Dashboard   { return e.dashboard }
func (*Dashboard) Covers() bool           { return true }
func (*Dashboard) Timeout() time.Duration { return shell.SettingsTimeout }

func (e *Engine) ReturnHome() bool { return e.options.Settings.Read().Return }

func (e *Engine) SetReturnHome(on bool) { e.set(tesseraReturn, setting.OnOff(on)) }

func (e *Engine) Themed() bool { return e.options.Settings.Read().Themed }

func (e *Engine) SetThemed(on bool) { e.set(tesseraThemed, setting.OnOff(on)) }

func (e *Engine) Turn() config.TesseraTurn { return e.options.Settings.Read().Turn }

func (e *Engine) SetTurn(t config.TesseraTurn) { e.set(tesseraTurn, string(t)) }

func (e *Engine) Bottom() config.TesseraBottom { return e.options.Settings.Read().Bottom }

func (e *Engine) Follow() bool { return e.options.Settings.Read().Follow }

func (e *Engine) SetFollow(on bool) { e.set(tesseraFollow, setting.OnOff(on)) }

func (e *Engine) SetBottom(b config.TesseraBottom) { e.set(tesseraBottom, string(b)) }

func (e *Engine) Landscape() bool { return e.Tessera() && e.options.Settings.Read().Wide }

func (e *Engine) Portrait() bool { return e.Tessera() && e.options.Settings.Read().Tall }

func (e *Engine) SetLandscape(on bool) { e.set(tesseraWide, setting.OnOff(on)) }

func (e *Engine) SetPortrait(on bool) { e.set(tesseraTall, setting.OnOff(on)) }
