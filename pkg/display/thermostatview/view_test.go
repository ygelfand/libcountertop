package thermostatview

import (
	"testing"
	"time"

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

func TestThreeLandscapeTilesGiveTheFirstTheFullHeight(t *testing.T) {
	rs := tiles(3, 1280, 800, 16)
	if rs[0].h != 800 || rs[1].h >= 800 || rs[1].x != rs[2].x {
		t.Errorf("three tiles %+v, want one full-height tile beside two stacked", rs)
	}
}

func TestChoicesComeFromHomeAssistantLists(t *testing.T) {
	st := thermostat.State{Preset: "eco", Fan: "low", Mode: thermostat.Heat}
	st.Presets = []string{"none", "eco", "boost_max"}
	st.FanModes = []string{"auto", "low"}
	got := options(st, pickPreset)
	if len(got) != 3 || !got[1].current || got[0].current {
		t.Fatalf("presets %+v, want three with eco current", got)
	}
	if got[0].label != "None" || got[2].label != "Boost max" {
		t.Errorf("labels %q %q, want None and Boost max", got[0].label, got[2].label)
	}
	if fans := options(st, pickFan); len(fans) != 2 || !fans[1].current {
		t.Errorf("fans %+v, want low current", fans)
	}
	if modes := options(st, pickMode); len(modes) != 0 {
		t.Errorf("modes %+v with no hvac_modes, want none", modes)
	}
}

func TestAnEaseSettlesOnTheNewValue(t *testing.T) {
	e := &ease{from: 70, to: 74, at: time.Unix(100, 0)}
	mid, moving := e.value(time.Unix(100, 0).Add(easeFor / 2))
	if !moving || mid <= 70 || mid >= 74 {
		t.Errorf("halfway %v moving %v, want between 70 and 74", mid, moving)
	}
	if end, moving := e.value(time.Unix(100, 0).Add(easeFor)); moving || end != 74 {
		t.Errorf("after the ease %v moving %v, want 74 still", end, moving)
	}
}

func TestShapesFollowTheTile(t *testing.T) {
	cases := []struct {
		w, h float32
		want shape
	}{
		{1280, 800, full}, {632, 800, tall}, {800, 632, mid}, {632, 392, small}, {960, 480, small},
		{800, 420, small},
	}
	for _, c := range cases {
		if got := shapeOf(c.w, c.h); got != c.want {
			t.Errorf("%vx%v = %v, want %v", c.w, c.h, got, c.want)
		}
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
