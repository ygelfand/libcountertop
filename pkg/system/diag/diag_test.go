package diag

import (
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/system/metrics"
)

type memory struct{ d schema.Diag }

func (m *memory) Read() schema.Diag { return m.d }

func (m *memory) Update(f func(*schema.Diag)) error {
	f(&m.d)
	return nil
}

func fresh(t *testing.T, o Options) *Diag {
	t.Helper()
	if o.Settings == nil {
		o.Settings = &memory{d: schema.DefaultDiag()}
	}
	if o.StateDir == "" {
		o.StateDir = t.TempDir()
	}
	return New(o)
}

func TestObjectIDsAreDistinct(t *testing.T) {
	d := fresh(t, Options{
		Zones: []Zone{
			{
				ID:        "cpu_temperature",
				Name:      "CPU temperature",
				Zones:     []string{"cpu"},
				PerDegree: Millidegrees,
			},
		},
		Network: func() string { return "home" },
		Cores:   4,
		Hold:    func(int) error { return nil },
	})
	seen := map[string]bool{}
	for _, e := range d.Entities() {
		id := objectID(e)
		if id == "" {
			t.Errorf("%T has no object id", e)
			continue
		}
		if seen[id] {
			t.Errorf("two entities are called %q", id)
		}
		seen[id] = true
	}
	for _, want := range []string{"metrics_interval", "min_cores", "cpu_temperature", "wifi_network"} {
		if !seen[want] {
			t.Errorf("no %q entity", want)
		}
	}
}

func TestAZoneReadsTheHottestOfItsSourcesInItsUnit(t *testing.T) {
	thermal := map[string]float64{"core0": 389, "core1": 510, "gpu": 441, "other": 99000}
	for _, c := range []struct {
		zone  Zone
		want  float64
		found bool
	}{
		{Zone{Zones: []string{"core0", "core1"}, PerDegree: Decidegrees}, 51, true},
		{Zone{Zones: []string{"gpu"}, PerDegree: Decidegrees}, 44.1, true},
		{Zone{Zones: []string{"other"}, PerDegree: Millidegrees}, 99, true},
		{Zone{Zones: []string{"missing"}, PerDegree: Millidegrees}, 0, false},
		{Zone{Zones: []string{"gpu"}}, 0, false},
	} {
		got, found := c.zone.degrees(thermal)
		if got != c.want || found != c.found {
			t.Errorf("%v per %v = %v, %v; want %v, %v",
				c.zone.Zones, c.zone.PerDegree, got, found, c.want, c.found)
		}
	}
}

func TestNoNetworkNoCoresLeavesThemOut(t *testing.T) {
	for _, e := range fresh(t, Options{}).Entities() {
		switch id := objectID(e); id {
		case "wifi_network", "min_cores":
			t.Errorf("%q is listed without being asked for", id)
		}
	}
}

func TestMinCoresHoldsBeforeSaving(t *testing.T) {
	var held []int
	d := fresh(t, Options{Cores: 4, Hold: func(n int) error {
		held = append(held, n)
		return nil
	}})
	if err := d.Controls().Change("min_cores", "3"); err != nil {
		t.Fatal(err)
	}
	if len(held) != 1 || held[0] != 3 {
		t.Errorf("held %v, want [3]", held)
	}
	if got := d.o.Settings.Read().MinCores; got != 3 {
		t.Errorf("saved min cores %d, want 3", got)
	}
}

func TestSetLeavesUnknownReadingsAlone(t *testing.T) {
	s := &esphome.Sensor{Base: esphome.Base{ObjectID: "test"}}
	s.Set(42)

	set(s, metrics.Reading{Value: 99, Known: false})
	if got := s.Get(); got != 42 {
		t.Errorf("an unknown reading overwrote the entity with %v", got)
	}

	set(s, metrics.Reading{Value: 7, Known: true})
	if got := s.Get(); got != 7 {
		t.Errorf("a known reading did not reach the entity: %v", got)
	}
}

func TestSampleOnAMachineWithNothing(t *testing.T) {
	d := fresh(t, Options{})
	d.Sample()
	d.Sample()
}

func objectID(e esphome.Entity) string {
	switch v := e.(type) {
	case *esphome.Sensor:
		return v.ObjectID
	case *esphome.TextSensor:
		return v.ObjectID
	case *esphome.Number:
		return v.ObjectID
	case *esphome.Button:
		return v.ObjectID
	case *esphome.Switch:
		return v.ObjectID
	}
	return ""
}
