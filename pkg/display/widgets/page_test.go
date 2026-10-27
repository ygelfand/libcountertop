package widgets

import (
	"testing"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
)

func TestPageBodyHandlesAnUnavailableSidePanel(t *testing.T) {
	for _, present := range []bool{false, true} {
		name := "unavailable"
		var side gogui.View
		if present {
			name = "available"
			side = gogui.Column(gogui.ContainerCfg{})
		}
		t.Run(name, func(t *testing.T) {
			page := &shell.Page{
				Build: func() ([]menu.Row, []func(int)) {
					return []menu.Row{{Label: "setting"}}, nil
				},
				Side: func(*gogui.Window) gogui.View { return side },
			}
			beside := false
			render := PageRenderer{
				Row: func(string, string, menu.Row, func(int), bool) gogui.View {
					return gogui.Column(gogui.ContainerCfg{})
				},
				Beside: func(_ *gogui.Window, got, body gogui.View) gogui.View {
					beside = true
					if got == nil || got != side {
						t.Fatal("Beside received the wrong side panel")
					}
					return body
				},
			}
			w := gogui.SimpleWindow("page", 128, 128, &struct{}{}, func(*gogui.Window) {})
			layout := gogui.GenerateViewLayout(PageBody(w, page, render), w)
			if beside != present {
				t.Errorf("side-panel renderer called = %v, want %v", beside, present)
			}
			if len(layout.Children) != 1 {
				t.Fatalf("page body has %d rows, want 1", len(layout.Children))
			}
		})
	}
}
