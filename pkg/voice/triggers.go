package voice

import (
	"fmt"
	"maps"

	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type Setting = setting.Setting[schema.Voice]

const (
	ActionsGroup  setting.Group = "Actions"
	CommandsGroup setting.Group = "Commands"
)

func NewTable(
	actions, commands []string,
	labels func() []setting.Option,
) *setting.Table[schema.Voice] {
	rows := []Setting{setting.Flag(
		Setting{Name: "actions", Group: ActionsGroup, Icon: "mdi:account-voice"},
		func(v *schema.Voice) *bool { return &v.Actions },
	)}
	for _, name := range actions {
		rows = append(rows, item(schema.ActionKey(name), ActionsGroup, "mdi:gesture-tap-button"))
	}
	if len(commands) > 0 {
		rows = append(rows, setting.Flag(
			Setting{
				Name:  "commands",
				Group: CommandsGroup,
				ID:    "voice_commands",
				Icon:  "mdi:microphone-message",
			},
			func(v *schema.Voice) *bool { return &v.Commands },
		), Setting{
			Name:    "label",
			Group:   CommandsGroup,
			Icon:    "mdi:label-outline",
			Kind:    setting.Choice,
			Options: []setting.Option{{Value: "", Key: "none"}},
			More:    labels,
			Read:    func(v *schema.Voice) string { return v.Label },
			Write: func(v *schema.Voice, s string) error {
				v.Label = s
				return nil
			},
		})
	}
	for _, name := range commands {
		rows = append(rows, item(schema.CommandKey(name), CommandsGroup, "mdi:text-to-speech"))
	}
	return setting.NewTable(
		"voice",
		[]setting.Group{ActionsGroup, CommandsGroup},
		rows,
		setting.Messages{Text: say.T, Missing: say.Missing},
	)
}

func ActionSwitch(name string) string { return say.T("voice.setting." + schema.ActionKey(name)) }

func item(key string, group setting.Group, icon string) Setting {
	return Setting{
		Name:  key,
		Group: group,
		Icon:  icon,
		Kind:  setting.Toggle,
		Read:  func(v *schema.Voice) string { return setting.OnOff(!v.Off[key]) },
		Write: func(v *schema.Voice, s string) error {
			on, ok := setting.Boolean(s)
			if !ok {
				return fmt.Errorf("%s takes on or off, not %q", key, s)
			}
			off := maps.Clone(v.Off)
			if off == nil {
				off = map[string]bool{}
			}
			if on {
				delete(off, key)
			} else {
				off[key] = true
			}
			v.Off = off
			return nil
		},
	}
}
