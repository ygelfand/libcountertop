package schema

import "github.com/ygelfand/libcountertop/pkg/say"

const (
	DoorbellSlots = 4
	DoorbellUser  = "admin"
)

type Doorbell struct {
	Chime   Chime                       `json:"chime"`
	Notify  Chime                       `json:"notify"`
	RingFor Delay                       `json:"ring_for"`
	Style   AlertStyle                  `json:"style"`
	Visitor AlertStyle                  `json:"visitor"`
	Tab     bool                        `json:"tab"`
	Doors   [DoorbellSlots]DoorbellDoor `json:"doors"`
}

func DoorbellStyles() []AlertStyle {
	return append([]AlertStyle{AlertScreen, AlertNone}, AlertStyles()...)
}

func VisitorStyles() []AlertStyle { return append([]AlertStyle{AlertAnswer}, DoorbellStyles()...) }

func DefaultDoorbell() Doorbell {
	return Doorbell{Chime: ChimeRing, Notify: ChimeDing, RingFor: Delay30s, Visitor: AlertAnswer}
}

func DoorbellRingSpans() []Delay {
	return []Delay{
		DelayNever,
		Delay5s,
		Delay10s,
		Delay15s,
		Delay30s,
		Delay45s,
		Delay1m,
		Delay2m,
		Delay3m,
		Delay5m,
	}
}

func DoorbellChimes() []Chime { return append(AlertChimes(), ChimeRing) }

type DoorbellDoor struct {
	Name     string          `json:"name"`
	UID      string          `json:"uid"`
	Host     string          `json:"host"`
	User     string          `json:"user"`
	Password string          `json:"password"`
	Stream   DoorbellStream  `json:"stream"`
	Camera   string          `json:"camera"`
	Device   string          `json:"device"`
	Channel  int             `json:"channel"`
	Sensors  DoorbellSensors `json:"sensors"`
	Video    DoorbellVideo   `json:"video"`
	Events   DoorbellFeed    `json:"events"`
	Talk     DoorbellTalk    `json:"talk"`
}

type DoorbellStream string

const (
	DoorbellMain DoorbellStream = ""
	DoorbellSub  DoorbellStream = "sub"
)

func DoorbellStreams() []DoorbellStream { return []DoorbellStream{DoorbellMain, DoorbellSub} }

func (s DoorbellStream) Label() string {
	if s == DoorbellMain {
		return say.T("doorbell.stream.main")
	}
	return say.T("doorbell.stream." + string(s))
}

func (d DoorbellDoor) Set() bool { return d.UID != "" || d.Host != "" || d.Device != "" }

func (d DoorbellDoor) Signed() bool { return d.Password != "" }

type DoorbellSensors struct {
	Visitor string `json:"visitor"`
	Person  string `json:"person"`
	Package string `json:"package"`
	Vehicle string `json:"vehicle"`
	Animal  string `json:"animal"`
}

type DoorbellVideo string

const (
	VideoAuto      DoorbellVideo = ""
	VideoReolink   DoorbellVideo = "reolink"
	VideoRTSP      DoorbellVideo = "rtsp"
	VideoAssistant DoorbellVideo = "ha"
)

func DoorbellVideos() []DoorbellVideo {
	return []DoorbellVideo{VideoAuto, VideoReolink, VideoRTSP, VideoAssistant}
}

func (v DoorbellVideo) Label() string {
	if v == VideoAuto {
		return say.T("doorbell.source.auto")
	}
	return say.T("doorbell.source." + string(v))
}

type DoorbellFeed string

const (
	FeedAuto      DoorbellFeed = ""
	FeedReolink   DoorbellFeed = "reolink"
	FeedONVIF     DoorbellFeed = "onvif"
	FeedAssistant DoorbellFeed = "ha"
)

func DoorbellFeeds() []DoorbellFeed {
	return []DoorbellFeed{FeedAuto, FeedReolink, FeedONVIF, FeedAssistant}
}

func (f DoorbellFeed) Label() string {
	if f == FeedAuto {
		return say.T("doorbell.source.auto")
	}
	return say.T("doorbell.source." + string(f))
}

type DoorbellTalk string

const (
	TalkAuto    DoorbellTalk = ""
	TalkReolink DoorbellTalk = "reolink"
	TalkONVIF   DoorbellTalk = "onvif"
)

func DoorbellTalks() []DoorbellTalk { return []DoorbellTalk{TalkAuto, TalkReolink, TalkONVIF} }

func (t DoorbellTalk) Label() string {
	if t == TalkAuto {
		return say.T("doorbell.source.auto")
	}
	return say.T("doorbell.source." + string(t))
}
