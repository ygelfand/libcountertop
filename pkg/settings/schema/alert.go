package schema

import "github.com/ygelfand/libcountertop/pkg/say"

type AlertStyle string

const (
	AlertScreen AlertStyle = ""
	AlertNone   AlertStyle = "none"
	AlertAnswer AlertStyle = "answer"
	AlertBanner AlertStyle = "banner"
	AlertCard   AlertStyle = "card"
	AlertFull   AlertStyle = "full"
	AlertToast  AlertStyle = "toast"
)

const DefaultAlertStyle = AlertBanner

func (s AlertStyle) Label() string {
	switch s {
	case AlertScreen:
		return say.T("alertstyle.screen")
	case AlertNone:
		return say.T("alertstyle.none")
	case AlertAnswer:
		return say.T("alertstyle.answer")
	case AlertBanner:
		return say.T("alertstyle.banner")
	case AlertCard:
		return say.T("alertstyle.card")
	case AlertFull:
		return say.T("alertstyle.full")
	case AlertToast:
		return say.T("alertstyle.toast")
	}
	return string(s)
}

func AlertStyles() []AlertStyle {
	return []AlertStyle{AlertBanner, AlertCard, AlertFull, AlertToast}
}
