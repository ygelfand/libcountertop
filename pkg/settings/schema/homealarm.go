package schema

import (
	"slices"

	"github.com/ygelfand/libcountertop/pkg/say"
)

type HomeAlarmLook string

const (
	HomeAlarmKeypad    HomeAlarmLook = "keypad"
	HomeAlarmShield    HomeAlarmLook = "shield"
	HomeAlarmMinimal   HomeAlarmLook = "minimal"
	HomeAlarmCard      HomeAlarmLook = "card"
	HomeAlarmRing      HomeAlarmLook = "ring"
	HomeAlarmTerminal  HomeAlarmLook = "terminal"
	HomeAlarmNeon      HomeAlarmLook = "neon"
	HomeAlarmHardware  HomeAlarmLook = "hardware"
	HomeAlarmFlip      HomeAlarmLook = "flip"
	HomeAlarmDial      HomeAlarmLook = "dial"
	HomeAlarmConsole   HomeAlarmLook = "console"
	HomeAlarmBlueprint HomeAlarmLook = "blueprint"
	HomeAlarmMatrix    HomeAlarmLook = "matrix"
	HomeAlarmVault     HomeAlarmLook = "vault"
)

func HomeAlarmLooks() []HomeAlarmLook {
	return []HomeAlarmLook{
		HomeAlarmKeypad,
		HomeAlarmShield,
		HomeAlarmCard,
		HomeAlarmRing,
		HomeAlarmDial,
		HomeAlarmNeon,
		HomeAlarmHardware,
		HomeAlarmFlip,
		HomeAlarmTerminal,
		HomeAlarmConsole,
		HomeAlarmBlueprint,
		HomeAlarmMatrix,
		HomeAlarmVault,
		HomeAlarmMinimal,
	}
}

func (l HomeAlarmLook) Label() string { return say.T("homealarm.look." + string(l)) }

type HomeAlarmCue struct {
	Show  bool  `json:"show"`
	Sound Chime `json:"sound"`
}

type HomeAlarm struct {
	Entity     string        `json:"entity"`
	Look       HomeAlarmLook `json:"look"`
	Tab        bool          `json:"tab"`
	Triggered  HomeAlarmCue  `json:"triggered"`
	Pending    HomeAlarmCue  `json:"pending"`
	Arming     HomeAlarmCue  `json:"arming"`
	Settled    HomeAlarmCue  `json:"settled"`
	SettledFor Delay         `json:"settled_for"`
	Hide       []string      `json:"hide"`
}

func (h HomeAlarm) Clone() HomeAlarm {
	h.Hide = slices.Clone(h.Hide)
	return h
}

func DefaultHomeAlarm() HomeAlarm {
	return HomeAlarm{
		Look:       HomeAlarmKeypad,
		Triggered:  HomeAlarmCue{Show: true, Sound: ChimeTrouble},
		Pending:    HomeAlarmCue{Show: true, Sound: ChimeTimer},
		Arming:     HomeAlarmCue{Sound: ChimeNone},
		Settled:    HomeAlarmCue{Sound: ChimeNone},
		SettledFor: Delay5s,
	}
}

func HomeAlarmHideSpans() []Delay {
	return []Delay{Delay5s, Delay10s, Delay15s, Delay30s, Delay1m, Delay2m, DelayNever}
}
