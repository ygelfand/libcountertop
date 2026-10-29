package thermostatview

import (
	"testing"

	"github.com/ygelfand/libcountertop/pkg/homeassistant/thermostat"
)

func TestTilesFillTheScreenWithFewerBeingBigger(t *testing.T) {
	const w, h, gap = 1280, 800, 16
	for n := 1; n <= 4; n++ {
		rs := tiles(n, w, h, gap)
		if len(rs) != n {
			t.Fatalf("%d thermostats got %d tiles", n, len(rs))
		}
		var area float32
		for _, r := range rs {
			if r.x < 0 || r.y < 0 || r.x+r.w > w+0.5 || r.y+r.h > h+0.5 {
				t.Errorf("%d: tile %+v leaves the screen", n, r)
			}
			area += r.w * r.h
		}
		if n == 1 && (rs[0].w != w || rs[0].h != h) {
			t.Errorf("one thermostat is %+v, want the whole screen", rs[0])
		}
		if area < w*h*0.9 {
			t.Errorf("%d tiles cover %.0f of %d", n, area, w*h)
		}
	}
	if two := tiles(2, 800, 1280, gap); two[0].w != 800 {
		t.Errorf("portrait pair %+v, want stacked full-width tiles", two[0])
	}
}

func TestAPendingSetpointShowsUntilHomeAssistantAgrees(t *testing.T) {
	was, want := 70.0, 72.0
	p := &pending{target: &want}
	s := thermostat.State{Target: &was}
	if got := p.over(s); *got.Target != 72 {
		t.Errorf("shown %v, want the pending 72", *got.Target)
	}
	if p.settled(s) {
		t.Error("settled while Home Assistant still says 70")
	}
	s.Target = &want
	if !p.settled(s) {
		t.Error("not settled once Home Assistant reports 72")
	}
}

func TestAPendingRangeReplacesBothEnds(t *testing.T) {
	low, high := 66.0, 75.0
	p := &pending{low: &low, high: &high}
	l0, h0 := 68.0, 74.0
	got := p.over(thermostat.State{Low: &l0, High: &h0})
	if *got.Low != 66 || *got.High != 75 {
		t.Errorf("range %v..%v, want 66..75", *got.Low, *got.High)
	}
}
