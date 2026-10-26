package schema

import "slices"

type Apps struct {
	Order []string `json:"order"`
	Dock  []string `json:"dock"`
}

const (
	AppPlayer    = "player"
	AppCalls     = "calls"
	AppMirror    = "mirror"
	AppShopping  = "shopping"
	AppCalendar  = "calendar"
	AppRadio     = "radio"
	AppRadar     = "radar"
	AppAlarms    = "alarms"
	AppCameras   = "cameras"
	AppLights    = "lights"
	AppSwitches  = "switches"
	AppDoorbell  = "doorbell"
	AppHomeAlarm = "homealarm"
	AppPhotos    = "photos"
)

func DefaultApps() Apps { return Apps{Dock: []string{AppPlayer, AppCalls}} }

func (a Apps) Clone() Apps {
	a.Order = slices.Clone(a.Order)
	a.Dock = slices.Clone(a.Dock)
	return a
}
