package screen

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type (
	Setting = setting.Setting[schema.Screen]
	Table   = setting.Table[schema.Screen]
)

const Panel setting.Group = "Panel"

func NewTable(uiSize func() string) *Table {
	var t *Table
	t = setting.NewTable(
		"screen",
		[]setting.Group{Panel},
		rows(func() *Table { return t }, uiSize),
		setting.Messages{Text: say.T, Missing: say.Missing},
	)
	return t
}

func rows(table func() *Table, uiSize func() string) []Setting {
	return append([]Setting{
		{
			Name:  "backlight",
			Group: Panel,
			ID:    "backlight", Icon: "mdi:brightness-6",
			Kind: setting.Number, Min: 0, Max: 100, Unit: "%", Slider: true,
			Read: func(s *schema.Screen) string { return strconv.Itoa(s.Backlight) },
			Write: func(s *schema.Screen, v string) error {
				n, err := table().MustRow("backlight").Number(v)
				if err != nil {
					return err
				}
				s.Backlight = n
				return nil
			},
		},
		{
			Name:  "auto",
			Group: Panel,
			ID:    "screen_mode",
			Icon:  "mdi:brightness-auto",
			Kind:  setting.Toggle,
			Read: func(s *schema.Screen) string {
				return setting.OnOff(s.Mode == schema.ModeAuto)
			},
			Write: func(s *schema.Screen, v string) error {
				on, ok := setting.Boolean(v)
				if !ok {
					return table().MustRow("auto").Bad(v, "on or off")
				}
				s.Mode = schema.ModeManual
				if on {
					s.Mode = schema.ModeAuto
				}
				return nil
			},
		},
		{
			Name:  "style",
			Group: Panel,
			ID:    "screen_style", Icon: "mdi:shape-outline",
			Kind: setting.Choice, Options: plain(style.Names()),
			Read: func(s *schema.Screen) string {
				if s.Style == "" {
					return style.Standard
				}
				return s.Style
			},
			Write: func(s *schema.Screen, v string) error {
				if !slices.Contains(style.Names(), v) {
					return table().MustRow("style").Bad(v, "")
				}
				s.Style = v
				return nil
			},
		},
		{
			Name:  "size",
			Group: Panel,
			ID:    "screen_size", Icon: "mdi:resize",
			Kind: setting.Choice, Options: plain(schema.ScreenSizes()),
			Read: func(s *schema.Screen) string {
				if s.Size == "" {
					return uiSize()
				}
				return s.Size
			},
			Write: func(s *schema.Screen, v string) error {
				if !slices.Contains(schema.ScreenSizes(), v) {
					return table().MustRow("size").Bad(v, "")
				}
				s.Size = v
				return nil
			},
		},
		{
			Name:  "theme",
			Group: Panel,
			ID:    "theme", Icon: "mdi:palette",
			Kind: setting.Choice, Options: themes(),
			Read: func(s *schema.Screen) string { return s.Theme },
			Write: func(s *schema.Screen, v string) error {
				if _, ok := theme.ByName(v); !ok && v != theme.Follow && v != theme.CustomName {
					return table().MustRow("theme").Bad(v, "")
				}
				s.Theme = v
				return nil
			},
		},
		{
			Name:  "drawer",
			Group: Panel,
			ID:    "drawer_edge", Icon: "mdi:gesture-swipe-horizontal",
			Kind: setting.Choice, Options: edges(),
			Read: func(s *schema.Screen) string { return string(s.Drawer) },
			Write: func(s *schema.Screen, v string) error {
				for _, e := range schema.Edges() {
					if string(e) == v {
						s.Drawer = e
						return nil
					}
				}
				return table().MustRow("drawer").Bad(v, "")
			},
		},
		setting.Pick(
			Setting{Name: "drawer.hide", Group: Panel, ID: "dock_hide", Icon: "mdi:dock-right"},
			schema.DockDelays(),
			func(s *schema.Screen) *schema.Delay { return &s.DockHide },
		),
		setting.Pick(
			Setting{Name: "alert", Group: Panel, Icon: "mdi:message-alert-outline"},
			schema.AlertStyles(),
			func(s *schema.Screen) *schema.AlertStyle { return &s.Alert },
		),
		setting.Pick(
			Setting{Name: "timer", Group: Panel, Icon: "mdi:timer-outline"},
			schema.TimerStyles(),
			func(s *schema.Screen) *schema.TimerStyle { return &s.Timer },
		),
		setting.Pick(
			Setting{Name: "keyboard", Group: Panel, Icon: "mdi:keyboard-outline"},
			schema.KeyboardSizes(),
			func(s *schema.Screen) *schema.KeyboardSize { return &s.Keyboard },
		),
		languageRow(Setting{Name: "language", Group: Panel, Icon: "mdi:translate"}),
		setting.Flag(Setting{
			Name: "marks", Group: Panel, ID: "privacy_marks", Icon: "mdi:shield-account-outline",
		}, func(s *schema.Screen) *bool { return &s.Marks }),
	}, customColors(table)...)
}

func customColors(table func() *Table) []Setting {
	out := make([]Setting, 0, len(theme.CustomFields))
	for _, field := range theme.CustomFields {
		name := "custom." + field
		out = append(out, Setting{
			Name:  name,
			Group: Panel,
			ID:    "theme_custom_" + field, Icon: "mdi:eyedropper-variant",
			Kind: setting.Text,
			Read: func(s *schema.Screen) string { return s.Custom.Theme().Color(field).String() },
			Write: func(s *schema.Screen, v string) error {
				if err := theme.CheckColor(v); err != nil {
					return table().MustRow(name).Bad(v, err.Error())
				}
				at, _ := s.Custom.Field(field)
				*at = strings.TrimSpace(v)
				return nil
			},
		})
	}
	return out
}

func plain(values []string) []setting.Option {
	out := make([]setting.Option, 0, len(values))
	for _, v := range values {
		out = append(out, setting.Option{Value: v, Label: v})
	}
	return out
}

func themes() []setting.Option {
	out := []setting.Option{{Value: theme.Follow, Label: theme.Follow}}
	for _, name := range theme.Names() {
		out = append(out, setting.Option{Value: name, Label: name})
	}
	return append(out, setting.Option{Value: theme.CustomName, Label: theme.CustomName})
}

func edges() []setting.Option {
	out := make([]setting.Option, 0, len(schema.Edges()))
	for _, e := range schema.Edges() {
		out = append(out, setting.Option{Value: string(e), Label: e.Label()})
	}
	return out
}

func languageRow(s Setting) Setting {
	s.Kind = setting.Choice
	for _, tag := range say.Languages() {
		s.Options = append(s.Options, setting.Option{Value: tag, Label: say.Name(tag)})
	}
	s.Read = func(c *schema.Screen) string {
		if c.Language == "" {
			return say.Chosen()
		}
		return c.Language
	}
	s.Write = func(c *schema.Screen, v string) error {
		for _, o := range s.Options {
			if strings.EqualFold(o.Value, v) || strings.EqualFold(o.Label, v) {
				c.Language = o.Value
				return nil
			}
		}
		return fmt.Errorf("%s takes %s, not %q", s.Name, strings.Join(s.Values(), " or "), v)
	}
	return s
}
