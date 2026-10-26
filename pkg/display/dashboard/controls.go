package dashboard

import (
	"log/slog"

	"github.com/ygelfand/libcountertop/pkg/display/clockview"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

type Setup struct {
	Clock schema.Clock
	Hours schema.HourFormat
}

type (
	Setting = setting.Setting[Setup]
	Table   = setting.Table[Setup]
)

const Group setting.Group = "Clock"

func NewTable() *Table {
	return setting.NewTable("clock", []setting.Group{Group}, []Setting{
		setting.Pick(
			Setting{Name: "hours", Group: Group, ID: "clock_format", Icon: "mdi:clock-outline"},
			schema.HourFormats(),
			func(s *Setup) *schema.HourFormat { return &s.Hours },
		),
		setting.Pick(Setting{Name: "face", Group: Group, Icon: "mdi:clock-digital"},
			clockview.Names(), func(s *Setup) *schema.Face { return &s.Clock.Face }),
		setting.Pick(Setting{Name: "position", Group: Group, Icon: "mdi:align-vertical-center"},
			schema.Positions(), func(s *Setup) *schema.Position { return &s.Clock.Position }),
		setting.Pick(Setting{Name: "align", Group: Group, Icon: "mdi:align-horizontal-center"},
			schema.Aligns(), func(s *Setup) *schema.Align { return &s.Clock.Align }),
		setting.Pick(Setting{Name: "size", Group: Group, Icon: "mdi:format-size"},
			schema.Sizes(), func(s *Setup) *schema.Size { return &s.Clock.Size }),
		setting.Pick(Setting{Name: "color", Group: Group, Icon: "mdi:palette-outline"},
			schema.Inks(), func(s *Setup) *schema.Ink { return &s.Clock.Ink }),
		setting.Flag(Setting{Name: "date", Group: Group, Icon: "mdi:calendar-blank-outline"},
			func(s *Setup) *bool { return &s.Clock.Date }),
	}, setting.Messages{Text: say.T, Missing: say.Missing})
}

type Redraw interface{ Redraw() }

type Preferences struct {
	Screen storage.Section[schema.Screen]
	Clock  storage.Section[schema.Clock]
}

func (p Preferences) Read() Setup {
	return Setup{Clock: p.Clock.Read(), Hours: p.Screen.Read().Hours}
}

func (p Preferences) write(was, now Setup) error {
	if now.Hours != was.Hours {
		if err := p.Screen.Update(func(s *schema.Screen) { s.Hours = now.Hours }); err != nil {
			return err
		}
	}
	if now.Clock != was.Clock {
		return p.Clock.Update(func(c *schema.Clock) { *c = now.Clock })
	}
	return nil
}

type Dependencies struct {
	Preferences Preferences
	Display     Redraw
	DeviceID    uint32
}

type Controls struct {
	*setting.Controls[Setup]

	preferences Preferences
	display     Redraw
}

func NewControls(d Dependencies) *Controls {
	c := &Controls{preferences: d.Preferences, display: d.Display}
	c.Controls = &setting.Controls[Setup]{
		Table:  NewTable(),
		Device: d.DeviceID,
		Read:   d.Preferences.Read,
		Save:   c.save,
	}
	return c
}

func (c *Controls) Settings() *setting.Controls[Setup] { return c.Controls }

func (c *Controls) Restore(s schema.Screen, clock schema.Clock) {
	c.PublishFrom(Setup{Clock: clock, Hours: s.Hours})
}

func (c *Controls) save(s Setting, v string) error {
	return c.apply(func(at *Setup) error { return s.Write(at, v) })
}

func (c *Controls) apply(change func(*Setup) error) error {
	was := c.preferences.Read()
	now := was
	if err := change(&now); err != nil {
		return err
	}
	err := c.preferences.write(was, now)
	if c.preferences.Read() != was {
		c.display.Redraw()
	}
	return err
}

func (c *Controls) commit(name string, change func(*Setup)) {
	if err := c.apply(func(at *Setup) error { change(at); return nil }); err != nil {
		slog.Error("saving a setting failed", "setting", name, "err", err)
	}
	c.Publish()
}

func (c *Controls) SetHours(v schema.HourFormat) {
	c.commit("hours", func(s *Setup) { s.Hours = v })
}

func (c *Controls) SetFace(v schema.Face) {
	c.commit("face", func(s *Setup) { s.Clock.Face = v })
}

func (c *Controls) SetPosition(v schema.Position) {
	c.commit("position", func(s *Setup) { s.Clock.Position = v })
}

func (c *Controls) SetAlign(v schema.Align) {
	c.commit("align", func(s *Setup) { s.Clock.Align = v })
}

func (c *Controls) SetSize(v schema.Size) {
	c.commit("size", func(s *Setup) { s.Clock.Size = v })
}

func (c *Controls) SetInk(v schema.Ink) {
	c.commit("color", func(s *Setup) { s.Clock.Ink = v })
}

func (c *Controls) SetDate(v bool) {
	c.commit("date", func(s *Setup) { s.Clock.Date = v })
}
