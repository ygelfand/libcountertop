package screens

import (
	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/alertview"
	"github.com/ygelfand/libcountertop/pkg/display/message"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const priorityAlert = 30

func (a *App) alert(w *gogui.Window) gogui.View {
	m, ok := a.o.Messages.Current()
	if !ok {
		return nil
	}
	return a.alerts.Build(w, m, a.alertStyle(m))
}

func (a *App) alertCovers() bool {
	m, ok := a.o.Messages.Current()
	return ok && alertview.Covers(a.alertStyle(m))
}

func (a *App) alertStyle(m message.Message) schema.AlertStyle {
	if m.Style != schema.AlertScreen {
		return m.Style
	}
	return a.o.Settings().Screen.Alert
}
