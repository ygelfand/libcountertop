package message

import (
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type Message struct {
	Title   string
	Body    string
	Tone    schema.Tone
	Icon    string
	Picture Picture
	Swatch  string
	Keys    []Key
	Flash   bool
	Style   schema.AlertStyle
	Chime   schema.Chime
	Repeat  time.Duration
	Ended   func(End)
}

type Picture struct {
	Src    string
	Width  int
	Height int
}

type Key struct {
	Text   string
	Swatch string
}

type End struct {
	Key int
	Why string
}

const (
	EndKey      = "key"
	EndTimeout  = "timeout"
	EndReplaced = "replaced"
	EndCleared  = "cleared"
	EndTapped   = "tapped"
	EndSwiped   = "swiped"
	EndClosed   = "closed"
)

func Swatch(name string, palette theme.Theme) (theme.Color, bool) {
	switch name {
	case "red":
		return palette.Danger, true
	case "orange":
		return palette.Warning, true
	case "yellow":
		return palette.Roles.Warm, true
	case "green":
		return palette.Success, true
	case "mint":
		return palette.Roles.Air, true
	case "blue":
		return palette.Roles.Cool, true
	case "purple":
		return palette.Roles.Choice, true
	case "pink":
		return palette.Roles.Heat, true
	case "gray", "grey":
		return palette.Muted, true
	}
	return theme.Color{}, false
}

func (m Message) Tint(palette theme.Theme) theme.Color {
	if c, ok := Swatch(m.Swatch, palette); ok {
		return c
	}
	return m.Tone.Color(palette)
}
