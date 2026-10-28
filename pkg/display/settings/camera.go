package settings

import (
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
)

type CameraOptions[K any] struct {
	Table   func() *setting.Table[K]
	Read    func() K
	Save    func(string, string) error
	Reset   func() error
	Push    func(shell.View)
	Redraw  func()
	Preview func(ui.Rect) shell.Feed
	Wrap    func(*shell.Page) *shell.Page
}
type CameraPages[K any] struct {
	options  CameraOptions[K]
	mu       sync.Mutex
	armed    time.Time
	root     *shell.Page
	sections map[setting.Group]*shell.Page
	choices  map[string]*shell.Page
}

func NewCameraPages[K any](o CameraOptions[K]) *CameraPages[K] {
	return &CameraPages[K]{
		options:  o,
		sections: map[setting.Group]*shell.Page{},
		choices:  map[string]*shell.Page{},
	}
}
func (p *CameraPages[K]) Page() *shell.Page {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.root == nil {
		p.root = p.makePage()
	}
	return p.root
}
func (p *CameraPages[K]) Section(g setting.Group) *shell.Page {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sections[g] == nil {
		p.sections[g] = p.makeSection(g)
	}
	return p.sections[g]
}
func (p *CameraPages[K]) Choice(s setting.Setting[K]) *shell.Page {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.choices[s.Name] == nil {
		p.choices[s.Name] = p.makeChoice(s)
	}
	return p.choices[s.Name]
}
func (p *CameraPages[K]) open(makePage func() *shell.Page) func(int) {
	return func(int) { p.options.Push(makePage()) }
}
func (p *CameraPages[K]) reset(int) {
	p.mu.Lock()
	if time.Since(p.armed) >= resetWindow {
		p.armed = time.Now()
		p.mu.Unlock()
		time.AfterFunc(resetWindow, p.options.Redraw)
		p.options.Redraw()
		return
	}
	p.armed = time.Time{}
	p.mu.Unlock()
	if err := p.options.Reset(); err != nil {
		slog.Error("resetting the camera settings", "err", err)
	}
	p.options.Redraw()
}

func (p *CameraPages[K]) Shown() []setting.Group {
	var out []setting.Group
	for _, g := range p.options.Table().Groups() {
		if len(p.options.Table().In(g)) > 0 {
			out = append(out, g)
		}
	}
	return out
}

func (p *CameraPages[K]) save(name, value string) {
	if err := p.options.Save(name, value); err != nil {
		slog.Error("the camera setting could not be saved", "setting", name, "err", err)
	}
}

const resetWindow = 3 * time.Second

func (p *CameraPages[K]) makePage() *shell.Page {
	return p.options.Wrap(&shell.Page{
		Title:   say.T("settings.camera"),
		Preview: p.options.Preview,
		Build: func() ([]menu.Row, []func(int)) {
			sh := p.options.Read()

			var rows []menu.Row
			var taps []func(int)

			for _, g := range p.Shown() {
				if in := p.options.Table().In(g); len(in) == 1 {
					row, tap := p.Row(in[0], &sh)
					row.Label = p.options.Table().Title(g)
					rows = append(rows, row)
					taps = append(taps, tap)
					continue
				}
				rows = append(rows, menu.Row{
					Label: p.options.Table().Title(g),
					Open:  func() shell.View { return p.Section(g) },
					Kind:  menu.Chevron,
					Value: p.options.Table().Sums(g, &sh),
				})
				taps = append(taps, nil)
			}
			p.mu.Lock()
			armed := time.Since(p.armed) < resetWindow
			p.mu.Unlock()
			label := say.T("camera.reset")
			if armed {
				label = say.T("camera.reset.confirm")
			}
			rows = append(rows, menu.Row{Label: label})
			taps = append(taps, p.reset)

			return rows, taps
		},
	})
}

func (p *CameraPages[K]) watch(page *shell.Page) *shell.Page {
	page.Preview = p.options.Preview
	return p.options.Wrap(page)
}

func (p *CameraPages[K]) makeSection(g setting.Group) *shell.Page {
	return p.watch(&shell.Page{
		Title: p.options.Table().Title(g),
		Build: func() ([]menu.Row, []func(int)) {
			sh := p.options.Read()

			var rows []menu.Row
			var taps []func(int)

			for _, s := range p.options.Table().In(g) {
				row, tap := p.Row(s, &sh)
				rows = append(rows, row)
				taps = append(taps, tap)
			}
			return rows, taps
		},
	})
}

func (p *CameraPages[K]) Row(s setting.Setting[K], sh *K) (menu.Row, func(int)) {
	row := menu.Row{Label: s.Title()}
	idle := s.Dim(sh)

	switch s.Kind {
	case setting.Toggle:
		row.Kind = menu.Toggle
		row.On = s.On(sh)
		return row, Dimmed(&row, idle, func(int) {
			p.save(s.Name, setting.OnOff(!row.On))
		})

	case setting.Number:
		row.Kind = menu.Slider
		row.Level = s.Percent(s.Level(sh))
		row.Snap = func(level int) int { return s.Percent(s.Raw(level)) }

		if s.Scale == setting.Linear {
			row.Value = s.Read(sh)
			if s.Unit != "" {
				row.Value += " " + s.Unit
			}
		}
		return row, Dimmed(&row, idle, func(level int) {
			p.save(s.Name, strconv.Itoa(s.Raw(level)))
		})

	case setting.Choice:
		row.Kind = menu.Chevron
		row.Value = s.Visible(sh)
		return row, Dimmed(&row, idle, p.open(func() *shell.Page { return p.Choice(s) }))
	}

	row.Kind = menu.Plain
	row.Value = s.Read(sh)
	return row, nil
}

func Dimmed(row *menu.Row, idle bool, act func(int)) func(int) {
	if !idle {
		return act
	}
	row.Dim = true
	return nil
}

func (p *CameraPages[K]) makeChoice(s setting.Setting[K]) *shell.Page {
	return p.watch(&shell.Page{
		Title: s.Title(),
		Build: func() ([]menu.Row, []func(int)) {
			cfg := p.options.Read()
			knob := s
			if current, ok := p.options.Table().Find(knob.Name); ok {
				knob = current
			}
			at := knob.Read(&cfg)

			rows := make([]menu.Row, 0, len(knob.Options))
			taps := make([]func(int), 0, len(knob.Options))

			for _, o := range knob.Options {
				rows = append(rows, menu.Row{
					Label:  knob.Label(o),
					Kind:   menu.Plain,
					Chosen: o.Value == at,
				})
				taps = append(taps, func(int) {
					p.save(knob.Name, o.Value)
				})
			}
			return rows, taps
		},
	})
}
