package settings

import (
	"log/slog"
	"slices"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/previews"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/visual"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
	tz "github.com/ygelfand/libcountertop/pkg/timezone"
)

type BasicState struct {
	Screen              config.Screen
	Clock               config.Clock
	Idle                config.Idle
	Visual              config.Visual
	PosterLabel, UISize string
}
type ScreenControl interface {
	Set(string, string) error
	SetDrawer(config.Edge)
	SetDockHide(config.Delay)
	SetBacklight(int)
	SetMode(config.ScreenMode)
	Use(string)
}
type PosterControl interface {
	SetEnabled(bool)
	Next()
	SetEvery(config.PosterEvery)
}
type VisualControl interface {
	SetMaxFPS(int)
	SetSeed(int)
}
type EngineControl interface {
	LowMemory() bool
	PageMemory() int
	SetLowMemory(bool)
	SetPageMemory(int)
}
type PrivacyControl interface{ SetMarks(bool) }
type ADBControl interface {
	ADB() bool
	SetADB(bool)
}
type ZoneControl interface {
	ChosenZone() string
	SetZone(string)
}
type HoursControl interface{ SetHours(config.HourFormat) }
type BasicDependencies struct {
	Preferences           Preferences
	Shell                 *shell.Shell
	Screen                ScreenControl
	Poster                PosterControl
	Visuals               VisualControl
	Engine                EngineControl
	Privacy               PrivacyControl
	Access                ADBControl
	Zone                  ZoneControl
	Dashboard             HoursControl
	Clocks                *ClockPages
	Idle                  *IdlePages
	UISize                func() string
	PowerPage, VisualPage func() shell.View
	SyncPage              func() shell.View
	Media                 MediaIdle
	Home                  DashboardMode
	HA                    HomeAccess
	Marks                 ModeMarks
	Alerts                AlertSettings
	Updates               Updates
	Lock                  PinLock
	HomeKey               HomeKey
	Previews              previews.Kit
}
type HomeKey interface {
	HomeKey() string
	SetHomeKey(string) error
}
type AdminPassword interface {
	Password() string
	SetPassword(string) error
}
type PinLock interface {
	Locked() bool
	Change()
	Clear()
}
type AlertSettings interface {
	Current() config.Alerts
	Change(name, value string) error
	Preview(config.Chime)
	Tones() []setting.Option
}
type DashboardMode interface {
	Enabled() bool
	Speaks() config.HomeMode
	SetMode(config.HomeMode)
}
type BasicPages struct{ options BasicDependencies }

func NewBasicPages(o BasicDependencies) *BasicPages { return &BasicPages{options: o} }
func (p *BasicPages) open(makePage func() shell.View) func(int) {
	return func(int) { p.options.Shell.Push(makePage()) }
}
func (p *BasicPages) useTheme(name string) func(int) { return func(int) { p.setTheme(name) } }
func (p *BasicPages) useZone(name string) func(int) {
	return func(int) { p.options.Zone.SetZone(name) }
}

func (p *BasicPages) Debug() *shell.Page {
	return &shell.Page{
		Title: say.T("debug.title"),
		Build: func() ([]menu.Row, []func(int)) {
			fps, seed := p.state().Visual.MaxFPS, p.state().Visual.Seed
			adb := p.options.Access.ADB()
			engine := p.options.Engine
			lean, pageMB := engine.LowMemory(), engine.PageMemory()
			return []menu.Row{
					{
						Glyph: gogui.IconTerminal,
						Label: say.T("system.adb"),
						Hint:  say.T("system.adb.hint"),
						Kind:  menu.Toggle,
						On:    adb,
					},
					{Label: say.T("debug.visuals"), Kind: menu.Chevron},
					{
						Label: say.T("debug.fps"),
						Kind:  menu.Slider,
						Level: FpsLevel(fps),
						Snap:  FpsSnap,
						Value: FpsSays(fps),
					},
					{
						Label: say.T("debug.seed"),
						Kind:  menu.Slider,
						Level: SeedLevel(seed),
						Value: SeedSays(seed),
					},
					{Label: say.T("diag.setting.low_memory"), Kind: menu.Toggle, On: lean},
					{
						Label: say.T("diag.setting.page_memory"),
						Kind:  menu.Slider,
						Level: PageMemoryLevel(pageMB),
						Snap:  PageMemorySnap,
						Value: PageMemorySays(pageMB),
					},
				}, []func(int){
					func(int) { p.options.Access.SetADB(!adb) },
					p.open(p.options.VisualPage),
					func(level int) { p.options.Visuals.SetMaxFPS(config.MaxFPSSteps[FpsIndex(level)]) },
					func(level int) { p.options.Visuals.SetSeed(SeedOf(level)) },
					func(int) { engine.SetLowMemory(!lean) },
					func(level int) {
						engine.SetPageMemory(config.PageMemorySteps[PageMemoryIndex(level)])
					},
				}
		},
	}
}

func FpsIndex(level int) int {
	n := len(config.MaxFPSSteps) - 1
	return min(max((level*n+50)/100, 0), n)
}

func FpsLevel(fps int) int {
	i := max(slices.Index(config.MaxFPSSteps, fps), 0)
	return i * 100 / (len(config.MaxFPSSteps) - 1)
}

func FpsSnap(level int) int { return FpsLevel(config.MaxFPSSteps[FpsIndex(level)]) }

func SeedOf(level int) int {
	if level <= 0 {
		return 0
	}
	return max(1, (min(level, 100)*visual.SeedMost+50)/100)
}

func SeedLevel(seed int) int {
	if seed <= 0 {
		return 0
	}
	return max(1, (seed*100+visual.SeedMost/2)/visual.SeedMost)
}

func SeedSays(seed int) string {
	if seed <= 0 {
		return say.T("debug.seed.random")
	}
	return say.F("debug.seed.value", map[string]any{"N": seed})
}

func FpsSays(fps int) string {
	return say.F("debug.fps.value", map[string]any{"N": fps})
}

func PageMemoryIndex(level int) int {
	n := len(config.PageMemorySteps) - 1
	return min(max((level*n+50)/100, 0), n)
}

func PageMemoryLevel(mb int) int {
	i := max(slices.Index(config.PageMemorySteps, mb), 0)
	return i * 100 / (len(config.PageMemorySteps) - 1)
}

func PageMemorySnap(level int) int {
	return PageMemoryLevel(config.PageMemorySteps[PageMemoryIndex(level)])
}

func PageMemorySays(mb int) string {
	if mb == 0 {
		return say.T("diag.option.page_memory.none")
	}
	return say.F("debug.page_memory.value", map[string]any{"N": mb})
}

func (p *BasicPages) Style() *shell.Page {
	return &shell.Page{
		Title: say.T("style.title"),
		Tiles: func() ([]menu.Cell, []func(int)) {
			now := style.ByName(p.state().Screen.Style).Name
			var cells []menu.Cell
			var taps []func(int)
			for _, name := range style.Names() {
				cells = append(
					cells,
					menu.Cell{
						Label:   say.T("style." + name),
						Chosen:  name == now,
						Preview: p.options.Previews.Style(name),
					},
				)
				taps = append(taps, func(int) {
					if err := p.setStyle(name); err != nil {
						slog.Error("the style could not be saved", "style", name, "err", err)
					}
				})
			}
			return cells, taps
		},
	}
}

func (p *BasicPages) Theme() *shell.Page {
	return &shell.Page{
		Title: say.T("theme.title"),

		Tiles: func() ([]menu.Cell, []func(int)) {
			now := p.state().Screen.Theme
			follow, _ := theme.ByName(style.Theme(p.state().Screen.Style, theme.Follow))

			cells := []menu.Cell{
				{
					Label:   say.T("theme.default"),
					Chosen:  now == theme.Follow,
					Preview: p.options.Previews.Theme(follow),
				},
			}
			acts := []func(int){p.useTheme(theme.Follow)}

			for _, t := range theme.All {
				cells = append(cells, menu.Cell{
					Label:   t.Name,
					Chosen:  t.Name == now,
					Preview: p.options.Previews.Theme(t),
				})
				acts = append(acts, p.useTheme(t.Name))
			}
			cells = append(cells, menu.Cell{
				Label:   say.T("theme.custom"),
				Chosen:  now == theme.CustomName,
				Preview: p.options.Previews.Theme(p.state().Screen.Custom.Theme()),
				Edit:    func() { p.options.Shell.Push(p.CustomTheme()) },
			})
			acts = append(acts, p.useTheme(theme.CustomName))
			return cells, acts
		},
	}
}

func ThemeSays(name string) string {
	switch name {
	case theme.Follow:
		return say.T("theme.default")
	case theme.CustomName:
		return say.T("theme.custom")
	}
	return name
}

func (p *BasicPages) Language() *shell.Page {
	return &shell.Page{
		Title: say.T("settings.language"),
		Build: func() ([]menu.Row, []func(int)) {
			now := say.Chosen()

			var (
				rows []menu.Row
				acts []func(int)
			)
			for _, tag := range say.Languages() {
				rows = append(rows, menu.Row{Label: say.Name(tag), Chosen: tag == now})
				acts = append(acts, func(int) { p.setLanguage(tag) })
			}
			return rows, acts
		},
	}
}

func (p *BasicPages) Zone() *shell.Page {
	return &shell.Page{
		Title: say.T("zone.title"),
		Build: func() ([]menu.Row, []func(int)) {
			now := p.options.Zone.ChosenZone()

			rows := []menu.Row{{Label: "Home Assistant", Chosen: now == ""}}
			acts := []func(int){p.useZone("")}

			for _, z := range tz.In("") {
				rows = append(rows, menu.Row{Label: z.Name, Chosen: z.Name == now})
				acts = append(acts, p.useZone(z.Name))
			}

			for _, region := range tz.Regions() {
				rows = append(rows, menu.Row{
					Label: region, Kind: menu.Chevron, Value: ChosenIn(region, now),
				})
				acts = append(acts, p.open(func() shell.View { return p.Region(region) }))
			}
			return rows, acts
		},
	}
}

func ChosenIn(region, now string) string {
	if now != "" && tz.Region(now) == region {
		return now[len(region)+1:]
	}
	return ""
}

func (p *BasicPages) Region(region string) *shell.Page {
	return &shell.Page{
		Title: region,
		Build: func() ([]menu.Row, []func(int)) {
			now := p.options.Zone.ChosenZone()

			var (
				rows []menu.Row
				acts []func(int)
			)
			for _, z := range tz.In(region) {
				rows = append(rows, menu.Row{
					Label: z.Name[len(region)+1:], Chosen: z.Name == now,
				})
				acts = append(acts, p.useZone(z.Name))
			}
			return rows, acts
		},
	}
}

func (p *BasicPages) state() BasicState { return p.options.Preferences.Basic(p.options.UISize()) }
func (p *BasicPages) setTheme(name string) {
	if err := p.options.Preferences.Screen.Update(
		func(c *config.Screen) { c.Theme = name },
	); err == nil {
		p.options.Screen.Use(name)
	}
}
func (p *BasicPages) setLanguage(tag string) {
	saveLanguage(p.options.Preferences.Screen, p.options.Shell, tag)
}
func (p *BasicPages) setKeyboard(v config.KeyboardSize) {
	saveKeyboard(p.options.Preferences.Screen, p.options.Shell, v)
}

func saveLanguage(section storage.Section[config.Screen], stack *shell.Shell, tag string) {
	say.Use(tag)
	saveScreen(section, stack, "screen.language", func(c *config.Screen) { c.Language = tag })
}

func saveKeyboard(
	section storage.Section[config.Screen],
	stack *shell.Shell,
	v config.KeyboardSize,
) {
	saveScreen(section, stack, "screen.keyboard", func(c *config.Screen) { c.Keyboard = v })
}
func (p *BasicPages) setVolumeEdge(v config.Edge) {
	saveScreen(
		p.options.Preferences.Screen,
		p.options.Shell,
		"screen.volume",
		func(c *config.Screen) { c.Volume = v },
	)
}
func (p *BasicPages) setAlert(v config.AlertStyle) {
	if err := p.options.Screen.Set("alert", string(v)); err != nil {
		slog.Error("saving the alert style failed", "err", err)
	}
}
func (p *BasicPages) setTimer(v config.TimerStyle) {
	if err := p.options.Screen.Set("timer", string(v)); err != nil {
		slog.Error("saving the timer style failed", "err", err)
	}
}
func (p *BasicPages) setStyle(v string) error { return p.options.Screen.Set("style", v) }
func (p *BasicPages) setSize(v string) {
	if err := p.options.Screen.Set("size", v); err != nil {
		slog.Error("saving the display size failed", "err", err)
	}
}
func (p *BasicPages) toggleAuto(was bool) {
	mode := config.ModeAuto
	if was {
		mode = config.ModeManual
	}
	p.options.Screen.SetMode(mode)
}
func (p *BasicPages) toggleHours(was config.HourFormat) {
	hours := config.TwentyFourHour
	if was == hours {
		hours = config.TwelveHour
	}
	p.options.Dashboard.SetHours(hours)
}
