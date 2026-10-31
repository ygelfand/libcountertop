package homeassistant

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNamedPagesAskForOneSlice(t *testing.T) {
	got := namedPageTemplate("binary_sensor", 200)
	if !strings.Contains(got, `(states["binary_sensor"] | list)[200:300]`) {
		t.Errorf("page template does not slice entities 200 to 300:\n%s", got)
	}
}

func TestParseNamedReadsPairs(t *testing.T) {
	got, err := ParseNamed(json.RawMessage(
		`[["binary_sensor.front_door_motion", "Front door Motion"], ["camera.side_door_fluent", "Side door Fluent"]]`,
	))
	if err != nil {
		t.Fatal(err)
	}
	want := []Named{
		{Entity: "binary_sensor.front_door_motion", Name: "Front door Motion"},
		{Entity: "camera.side_door_fluent", Name: "Side door Fluent"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d is %v, want %v", i, got[i], want[i])
		}
	}
}
