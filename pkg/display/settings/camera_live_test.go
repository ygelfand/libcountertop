package settings

import (
	"testing"

	"github.com/ygelfand/libcountertop/pkg/camera/live"
	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func livePages(t *testing.T) (*CameraPages[live.Knobs], *live.Camera) {
	t.Helper()
	saved := schema.DefaultCamera()
	cam := live.New(live.Options{
		Vendor: live.Vendor{
			EVMin:    -2,
			EVMax:    2,
			Scenes:   []string{"auto", "night"},
			Balances: []string{"auto", "daylight", "shade"},
			Effects:  []string{"none", "mono"},
			ISOs:     []string{"auto", "100"},
			Levels:   true,
			Rates:    []int{15, 30},
		},
		Streams: live.Streams{
			Main:        []string{"1280x720", "640x480"},
			MainDefault: "1280x720",
			Sub:         []int{640, 320},
			SubDefault:  640,
		},
		Saved: func() schema.Camera { return saved },
	})
	pages := NewCameraPages(CameraOptions[live.Knobs]{
		Table:  cam.Table,
		Read:   cam.Saved,
		Push:   func(shell.View) {},
		Redraw: func() {},
		Wrap:   func(p *shell.Page) *shell.Page { return p },
	})
	return pages, cam
}

func TestTheCameraPageIsTheSections(t *testing.T) {
	p, _ := livePages(t)
	rows, acts := p.Page().Build()
	shown := p.Shown()

	if len(rows) != len(shown)+1 || len(acts) != len(rows) {
		t.Fatalf("%d rows against %d sections and a reset", len(rows), len(shown))
	}
	if last := rows[len(rows)-1]; last.Kind == menu.Chevron || acts[len(acts)-1] == nil {
		t.Errorf("the last row is %+v, want the reset action", last)
	}
	for i, g := range shown {
		if rows[i].Kind != menu.Chevron || (acts[i] == nil && rows[i].Open == nil) {
			t.Errorf("%s does not open", g)
		}
	}
}

func TestASectionIsTheKnobTable(t *testing.T) {
	p, cam := livePages(t)
	var seen int
	for _, g := range p.Shown() {
		rows, acts := p.Section(g).Build()
		want := cam.Table().In(g)
		if len(rows) != len(want) || len(rows) != len(acts) {
			t.Fatalf("%s: %d rows, %d actions, %d knobs", g, len(rows), len(acts), len(want))
		}
		seen += len(rows)
		for i, s := range want {
			if rows[i].Label != s.Title() {
				t.Errorf("%s row %d is %q and the knob is %q", g, i, rows[i].Label, s.Title())
			}
			switch s.Kind {
			case setting.Number:
				if rows[i].Kind != menu.Slider || rows[i].Level < 0 || rows[i].Level > 100 {
					t.Errorf("%s drew as %v at %d", s.Name, rows[i].Kind, rows[i].Level)
				}
			case setting.Choice:
				if rows[i].Kind != menu.Chevron {
					t.Errorf("%s is a choice and drew as %v", s.Name, rows[i].Kind)
				}
			}
		}
	}
	if seen != len(cam.Table().Rows()) {
		t.Errorf("the sections show %d knobs and the table has %d", seen, len(cam.Table().Rows()))
	}
}

func TestAChoiceOpensItsOptions(t *testing.T) {
	p, cam := livePages(t)
	s, ok := cam.Table().Find("whitebalance")
	if !ok {
		t.Fatal("no white balance knob")
	}
	rows, acts := p.Choice(s).Build()
	if len(rows) != len(s.Options) {
		t.Fatalf("%d rows against %d options", len(rows), len(s.Options))
	}
	var marked int
	for i, o := range s.Options {
		if rows[i].Label != s.Label(o) {
			t.Errorf("row %d is %q, want %q", i, rows[i].Label, s.Label(o))
		}
		if rows[i].Chosen {
			marked++
		}
		if acts[i] == nil {
			t.Errorf("%q cannot be picked", s.Label(o))
		}
	}
	if marked != 1 {
		t.Errorf("%d options are marked as in effect, want exactly the one that is", marked)
	}
}
