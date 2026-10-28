package settings

import (
	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/say"
)

func PowerPage(push func(shell.View), restart, reboot func()) *shell.Page {
	open := func(v shell.View) func(int) { return func(int) { push(v) } }
	return &shell.Page{
		Title: say.T("power.title"),
		Build: func() ([]menu.Row, []func(int)) {
			return []menu.Row{
				{Label: say.T("power.restart"), Kind: menu.Chevron},
				{Label: say.T("power.reboot"), Kind: menu.Chevron},
			}, []func(int){
				open(ConfirmPage(say.T("power.restart"),
					say.T("power.restart.what"), restart)),
				open(ConfirmPage(say.T("power.reboot"),
					say.T("power.reboot.what"), reboot)),
			}
		},
	}
}
func ConfirmPage(title, what string, do func()) *shell.Page {
	return &shell.Page{
		Title: title,
		Build: func() ([]menu.Row, []func(int)) {
			return []menu.Row{
				{Label: what, Kind: menu.Plain},
				{Label: title},
			}, []func(int){
				nil,
				func(int) { do() },
			}
		},
	}
}
