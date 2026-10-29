package thermostat

import (
	"context"
	"encoding/json"
	"maps"
	"strings"
	"sync"
	"testing"

	"github.com/ygelfand/go-esphome-device/api"

	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/states"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type section struct {
	mu sync.Mutex
	at schema.Thermostat
}

func (s *section) Read() schema.Thermostat {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.at.Clone()
}

func (s *section) Update(change func(*schema.Thermostat)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(&s.at)
	return nil
}

type call struct {
	action string
	data   map[string]string
}

type house struct {
	mu     sync.Mutex
	limits string
	calls  []call
}

func (h *house) Template(context.Context, string) (json.RawMessage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return json.RawMessage(h.limits), nil
}

func (h *house) Entities(context.Context, homeassistant.Filter) ([]homeassistant.Entity, error) {
	return nil, nil
}

func (h *house) Call(_ context.Context, action string, data map[string]string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, call{action, maps.Clone(data)})
	return nil
}

func (h *house) Watch(func(homeassistant.Access)) func() { return func() {} }

func (h *house) sent() []call {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]call(nil), h.calls...)
}

func report(s *states.States, entity, attribute, state string) {
	_ = s.Handle(context.Background(), nil, &api.HomeAssistantStateResponse{
		EntityId: entity, Attribute: attribute, State: state,
	})
}

func setup(entities ...string) (*Thermostats, *states.States, *house) {
	s := states.New()
	h := &house{limits: `{}`}
	t := New(Options{
		Controller: h,
		States:     s,
		Settings:   &section{at: schema.Thermostat{Entities: entities}},
	})
	return t, s, h
}

func TestAThermostatReadsWhatHomeAssistantReports(t *testing.T) {
	th, s, _ := setup("climate.hall")
	report(s, "climate.hall", "", "heat")
	report(s, "climate.hall", "friendly_name", "Hall")
	report(s, "climate.hall", "hvac_action", "heating")
	report(s, "climate.hall", "current_temperature", "70")
	report(s, "climate.hall", "temperature", "72")
	report(s, "climate.hall", "current_humidity", "None")

	got := th.States()
	if len(got) != 1 {
		t.Fatalf("states = %d, want 1", len(got))
	}
	st := got[0]
	if st.Mode != Heat || st.Name != "Hall" || st.Action != Heating {
		t.Errorf("mode %q name %q action %q, want heat Hall heating", st.Mode, st.Name, st.Action)
	}
	if st.Current == nil || *st.Current != 70 || st.Target == nil || *st.Target != 72 {
		t.Errorf("current %v target %v, want 70 and 72", st.Current, st.Target)
	}
	if st.Humidity != nil {
		t.Errorf("humidity = %v from None, want unknown", *st.Humidity)
	}
}

func TestLimitsComeFromTheTemplate(t *testing.T) {
	got := map[string]limits{}
	raw := `{"climate.hall": {"modes": ["off", "heat", "cool", "heat_cool"], "fans": ["auto", "on"],
		"min": 45, "max": 95, "step": null, "features": 3}}`
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	l := got["climate.hall"].read("°F")
	if len(l.Modes) != 4 || l.Modes[3] != HeatCool || l.Min != 45 || l.Max != 95 {
		t.Errorf("limits %+v, want four modes and 45..95", l)
	}
	if l.Step != 1 {
		t.Errorf("step = %v with no step attribute in °F, want 1", l.Step)
	}
	if got["climate.hall"].read("").Step != 0.5 {
		t.Error("with no unit and no step attribute the step should be 0.5")
	}
}

func TestARangeNeedsTheFeatureAndBothEnds(t *testing.T) {
	low, high := 68.0, 74.0
	s := State{Mode: HeatCool, Low: &low, High: &high}
	if s.Ranged() {
		t.Error("ranged without the range feature")
	}
	s.features = featureRange
	if !s.Ranged() {
		t.Error("not ranged with the feature, heat_cool and both ends")
	}
	s.Mode = Heat
	if s.Ranged() {
		t.Error("ranged in heat mode")
	}
}

func TestNoMoreThanFourAreChosen(t *testing.T) {
	th, _, _ := setup()
	for _, e := range []string{"climate.a", "climate.b", "climate.c", "climate.d", "climate.e"} {
		if err := th.Choose(e, true); err != nil {
			t.Fatal(err)
		}
	}
	if got := th.Chosen(); len(got) != schema.ThermostatsMost || got[3] != "climate.d" {
		t.Errorf("chosen = %v, want the first four", got)
	}
	if err := th.Choose("climate.b", false); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(th.Chosen(), " "); got != "climate.a climate.c climate.d" {
		t.Errorf("chosen = %q after dropping b", got)
	}
}

func TestSettingARangeSendsBothEnds(t *testing.T) {
	th, _, h := setup("climate.hall")
	if err := th.SetRange(context.Background(), "climate.hall", 68, 74.5); err != nil {
		t.Fatal(err)
	}
	if err := th.SetMode(context.Background(), "climate.hall", Cool); err != nil {
		t.Fatal(err)
	}
	sent := h.sent()
	if len(sent) != 2 {
		t.Fatalf("sent %d calls, want 2", len(sent))
	}
	r := sent[0]
	if r.action != "climate.set_temperature" || r.data["target_temp_low"] != "68" ||
		r.data["target_temp_high"] != "74.5" || r.data["entity_id"] != "climate.hall" {
		t.Errorf("range call = %+v", r)
	}
	if m := sent[1]; m.action != "climate.set_hvac_mode" || m.data["hvac_mode"] != "cool" {
		t.Errorf("mode call = %+v", m)
	}
}

func TestOnlyChosenThermostatsAreChanged(t *testing.T) {
	th, _, h := setup("climate.hall")
	if err := th.SetTemperature(context.Background(), "climate.garage", 60); err != ErrUnknown {
		t.Errorf("err = %v, want ErrUnknown", err)
	}
	if len(h.sent()) != 0 {
		t.Error("a call reached Home Assistant for an entity nobody chose")
	}
}
