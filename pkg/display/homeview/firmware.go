package homeview

import (
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/say"
	schema "github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const clockStep = 15

var (
	firmwareSwitches = []string{
		"show_home_button",
		"page_buttons",
		"swipe_between_pages",
		"back_to_page_1",
		"back_to_page_1_on_standby",
		"auto_standby",
		"night_mode",
	}
	firmwareNumbers = []string{"back_to_page_1_after", "standby_after"}
	firmwareClocks  = []string{"night_starts", "night_ends"}
)

func (view *Renderer) firmwarePage() *shell.Page {
	return &shell.Page{
		Title: schema.HomeFirmware.Label(),
		Build: func() ([]menu.Row, []func(int)) {
			engine := view.options.Engine
			themed, turn, bottom := engine.Themed(), engine.Turn(), engine.Bottom()
			bottoms := schema.TesseraBottoms()
			labels := make([]string, len(bottoms))
			for i, b := range bottoms {
				labels[i] = b.Label()
			}
			rows := []menu.Row{
				{Label: say.T("home.tessera.themed"), Kind: menu.Toggle, On: themed},
				{Label: say.T("home.tessera.turn"), Kind: menu.Chevron, Value: turn.Label()},
				{
					Label:   say.T("home.tessera.bottom"),
					Kind:    menu.Segments,
					Options: labels,
					Level:   max(slices.Index(bottoms, bottom), 0),
				},
			}
			taps := []func(int){
				func(int) { engine.SetThemed(!themed) },
				func(int) { view.options.Shell.Push(view.turnPage()) },
				func(i int) { engine.SetBottom(bottoms[i]) },
			}
			if f := view.firmware; f != nil {
				r, t := firmwareRows(f)
				rows, taps = append(rows, r...), append(taps, t...)
			}
			return rows, taps
		},
	}
}

func firmwareRows(f FirmwareSettings) ([]menu.Row, []func(int)) {
	var (
		rows []menu.Row
		taps []func(int)
	)
	for _, id := range firmwareSwitches {
		s, ok := f.Switch(id)
		if !ok {
			continue
		}
		on := s.Get()
		rows = append(rows, menu.Row{Label: say.T("home.firmware." + id), Kind: menu.Toggle, On: on})
		taps = append(taps, func(int) { s.OnCommand(!on) })
	}
	for _, id := range firmwareNumbers {
		n, ok := f.Number(id)
		if !ok || n.Max <= n.Min {
			continue
		}
		steps := stepsOf(n.Min, n.Max, n.Step)
		at := func(level int) float64 { return n.Min + float64(steps.Index(level))*n.Step }
		v := float64(n.Get())
		rows = append(rows, menu.Row{
			Label: say.T("home.firmware." + id),
			Kind:  menu.Slider,
			Level: steps.Level(int(math.Round((v - n.Min) / n.Step))),
			Snap:  steps.Snap,
			Value: seconds(v),
		})
		taps = append(taps, func(level int) { n.OnCommand(float32(at(level))) })
	}
	day := menu.Steps(24 * 60 / clockStep)
	for _, id := range firmwareClocks {
		t, ok := f.Clock(id)
		if !ok {
			continue
		}
		minutes, ok := clockMinutes(t.Get())
		if !ok {
			continue
		}
		rows = append(rows, menu.Row{
			Label: say.T("home.firmware." + id),
			Kind:  menu.Slider,
			Level: day.Level(minutes / clockStep),
			Snap:  day.Snap,
			Value: t.Get(),
		})
		taps = append(taps, func(level int) {
			m := day.Index(level) * clockStep
			t.OnCommand(fmt.Sprintf("%02d:%02d", m/60, m%60))
		})
	}
	return rows, taps
}

func (view *Renderer) turnPage() *shell.Page {
	return &shell.Page{
		Title: say.T("home.tessera.turn"),
		Build: func() ([]menu.Row, []func(int)) {
			engine := view.options.Engine
			now := engine.Turn()
			var (
				rows []menu.Row
				taps []func(int)
			)
			for _, t := range schema.TesseraTurns() {
				rows = append(rows, menu.Row{Label: t.Label(), Chosen: t == now})
				taps = append(taps, func(int) { engine.SetTurn(t) })
			}
			return rows, taps
		},
	}
}

func stepsOf(lo, hi, step float64) menu.StepScale {
	if step <= 0 {
		step = 1
	}
	return menu.Steps(int(math.Round((hi-lo)/step)) + 1)
}

func clockMinutes(s string) (int, bool) {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil || h > 23 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func seconds(v float64) string {
	s := int(v)
	switch {
	case s >= 3600 && s%3600 == 0:
		return strconv.Itoa(s/3600) + " h"
	case s >= 60 && s%60 == 0:
		return strconv.Itoa(s/60) + " min"
	}
	return strconv.Itoa(s) + " s"
}
