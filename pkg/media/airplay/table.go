package airplay

import (
	"slices"

	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type (
	Setting  = setting.Setting[schema.AirPlay]
	Table    = setting.Table[schema.AirPlay]
	Controls = setting.Controls[schema.AirPlay]
)

const Group setting.Group = "AirPlay"

func NewTable() *Table {
	var t *Table
	t = setting.NewTable("media", []setting.Group{Group}, []Setting{
		{
			Name: "airplay", Group: Group, ID: "airplay", Icon: "mdi:apple-airplay",
			Kind: setting.Toggle,
			Read: func(c *schema.AirPlay) string { return setting.OnOff(c.Enabled) },
			Write: func(c *schema.AirPlay, v string) error {
				on, ok := setting.Boolean(v)
				if !ok {
					return t.MustRow("airplay").Bad(v, "on or off")
				}
				c.Enabled = on
				return nil
			},
		},
		{
			Name: "airplay_audio_mode", Group: Group, Icon: "mdi:sync",
			Kind:    setting.Choice,
			Options: audioModes(),
			Read:    func(c *schema.AirPlay) string { return string(c.Audio) },
			Write: func(c *schema.AirPlay, v string) error {
				m := schema.AirPlayAudioMode(v)
				if !slices.Contains(schema.AirPlayAudioModes(), m) {
					return t.MustRow("airplay_audio_mode").Bad(v, "")
				}
				c.Audio = m
				return nil
			},
		},
	}, setting.Messages{Text: say.T, Missing: say.Missing})
	return t
}

func audioModes() []setting.Option {
	modes := schema.AirPlayAudioModes()
	out := make([]setting.Option, len(modes))
	for i, m := range modes {
		out[i] = setting.Option{Value: string(m), Key: string(m)}
	}
	return out
}
