package schema

import "github.com/ygelfand/libcountertop/pkg/say"

type Lovelace struct {
	User           string            `json:"user"`
	Dashboard      LovelaceDashboard `json:"dashboard"`
	View           LovelaceView      `json:"view"`
	SaverDashboard LovelaceDashboard `json:"saver_dashboard"`
	SaverView      LovelaceView      `json:"saver_view"`
}

type LovelaceDashboard string

func (d LovelaceDashboard) Label() string {
	if d == "" {
		return say.T("lovelace.default")
	}
	return string(d)
}

type LovelaceView string

func (v LovelaceView) Label() string {
	if v == "" {
		return say.T("lovelace.first")
	}
	return string(v)
}

type LovelaceUser string

func (u LovelaceUser) Label() string { return string(u) }
