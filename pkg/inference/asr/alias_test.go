package asr

import (
	"slices"
	"testing"
)

func TestAliasesSendEveryFormAndShowTheName(t *testing.T) {
	a := parseAliases([]string{
		"ESP32 = ESP thirty two | E S P thirty two",
		"Kitchen",
		"  ",
		"athena = a thena",
	})
	want := []string{"E S P thirty two", "ESP thirty two", "ESP32", "Kitchen", "a thena", "athena"}
	if !slices.Equal(a.forms, want) {
		t.Errorf("forms %q, want %q", a.forms, want)
	}
	for in, out := range map[string]string{
		"flash the esp thirty two now": "flash the ESP32 now",
		"call A Thena":                 "call athena",
		"turn on the kitchen":          "turn on the kitchen",
		"respond":                      "respond",
	} {
		if got := a.rewrite(in); got != out {
			t.Errorf("rewrite %q = %q, want %q", in, got, out)
		}
	}
}

func TestNoAliasesLeaveTextAlone(t *testing.T) {
	a := parseAliases([]string{"radar", "mirror"})
	if got := a.rewrite("show me the radar"); got != "show me the radar" {
		t.Errorf("rewrite changed plain text: %q", got)
	}
}
