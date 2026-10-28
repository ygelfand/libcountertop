package shell

import (
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
)

type Page struct {
	Title  string
	Reveal string
	OnOpen func()
	Search *Search

	Build func() ([]menu.Row, []func(level int))

	Tiles func() ([]menu.Cell, []func(int))

	Preview func(at ui.Rect) Feed

	Side func(*gogui.Window) gogui.View

	Actions []Action

	Busy func() (Busy, bool)
}

type Search struct {
	Query   *string
	Results func() *Page
}

func SettingsPage(view View) *Page {
	if page, ok := view.(*Page); ok {
		return page
	}
	if provider, ok := view.(interface{ SettingsPage() *Page }); ok {
		return provider.SettingsPage()
	}
	return nil
}

type Busy struct {
	Text    string
	Detail  string
	Outcome Outcome
	Close   string
	Dismiss func()
}

type Outcome int

const (
	Working Outcome = iota
	Succeeded
	Failed
)

type Feed struct {
	Live  bool
	Glyph string
	Note  string
}

type Action struct {
	Label string
	Glyph string
	Role  Role
	Do    func()
}

type Role int

const (
	Primary Role = iota
	Plain
	Danger
)

func (p *Page) Covers() bool { return true }

func (p *Page) Opened() {
	if p.OnOpen != nil {
		p.OnOpen()
	}
}

func (p *Page) Timeout() time.Duration { return SettingsTimeout }
