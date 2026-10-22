package settings

import (
	"log/slog"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/say"
)

type CertificateStatus interface{ Says() string }

type WebAccess struct {
	Admin       AdminPassword
	Certificate CertificateStatus
}

func (w WebAccess) Page(address func() string) *shell.Page {
	return &shell.Page{Title: say.T("webaccess.title"), Build: func() ([]menu.Row, []func(int)) {
		rows := []menu.Row{
			{
				Glyph: gogui.IconGlobe,
				Label: say.T("webaccess.address"),
				Kind:  menu.Plain,
				Value: "https://" + address() + "/",
			},
		}
		acts := []func(int){nil}
		if a := w.Admin; a != nil {
			rows = append(rows, menu.Row{
				Glyph: gogui.IconLock,
				Label: say.T("access.setting.password"),
				Kind:  menu.Password,
				Value: a.Password(),
				Save: func(v string) {
					if err := a.SetPassword(v); err != nil {
						slog.Warn("saving the admin password failed", "err", err)
					}
				},
			})
			acts = append(acts, nil)
		}
		if c := w.Certificate; c != nil {
			rows = append(rows, menu.Row{
				Glyph: "mdi:certificate-outline",
				Label: say.T("certs.group.certificate"),
				Kind:  menu.Plain,
				Value: c.Says(),
			})
			acts = append(acts, nil)
		}
		return rows, acts
	}}
}
