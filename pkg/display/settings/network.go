package settings

import (
	"fmt"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/say"
)

type NetworkState struct {
	SSID, Address, MAC, Router string
	Renew                      time.Time
	Verify                     bool
}

func NetworkPage(
	read func() NetworkState,
	setVerify func(bool),
	web *WebAccess,
	stack *shell.Shell,
) *shell.Page {
	return &shell.Page{Title: say.T("network.title"), Build: func() ([]menu.Row, []func(int)) {
		c := read()
		rows := []menu.Row{
			{Label: say.T("network.ssid"), Kind: menu.Plain, Value: c.SSID},
			{Label: say.T("network.address"), Kind: menu.Plain, Value: c.Address},
			{Label: say.T("network.mac"), Kind: menu.Plain, Value: c.MAC},
		}
		acts := []func(int){nil, nil, nil}
		if !c.Renew.IsZero() {
			rows = append(
				rows,
				menu.Row{Label: say.T("network.router"), Kind: menu.Plain, Value: c.Router},
				menu.Row{
					Label: say.T("network.lease"),
					Kind:  menu.Plain,
					Value: LeaseIn(c.Renew),
				},
			)
			acts = append(acts, nil, nil)
		}
		rows = append(
			rows,
			menu.Row{
				Glyph: gogui.IconLock,
				Label: say.T("system.certificates"),
				Hint:  say.T("system.certificates.hint"),
				Kind:  menu.Toggle,
				On:    c.Verify,
			},
		)
		acts = append(acts, func(int) { setVerify(!c.Verify) })
		if web != nil {
			rows = append(rows, menu.Row{
				Glyph: gogui.IconGlobe,
				Label: say.T("webaccess.title"),
				Open:  func() shell.View { return web.Page(func() string { return read().Address }) },
				Kind:  menu.Chevron,
				Value: "https://" + c.Address + "/",
			})
			acts = append(acts, nil)
		}
		return rows, acts
	}}
}
func LeaseIn(at time.Time) string {
	d := time.Until(at)
	if d <= 0 {
		return say.T("network.lease.now")
	}
	if d < time.Hour {
		return say.F("network.lease.minutes", map[string]any{"N": int(d.Minutes())})
	}
	return say.F("network.lease.hours", map[string]any{"N": fmt.Sprintf("%.1f", d.Hours())})
}
