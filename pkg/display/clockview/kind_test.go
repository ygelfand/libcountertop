package clockview

import (
	"testing"

	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func TestEveryFaceIsNamedOnceAndLabelled(t *testing.T) {
	seen := map[schema.Face]bool{}
	for _, f := range Faces {
		if seen[f.Name] {
			t.Errorf("%q is listed twice", f.Name)
		}
		seen[f.Name] = true
		if f.Draw == nil {
			t.Errorf("%q draws nothing", f.Name)
		}
		if key := "face." + string(f.Name); say.T(key) == key {
			t.Errorf("%q has no label", f.Name)
		}
	}
	if !seen[schema.DefaultFace] {
		t.Errorf("the default face %q is not drawn", schema.DefaultFace)
	}
}

func TestEveryCountdownIsAFaceThatCounts(t *testing.T) {
	for _, f := range Faces {
		if f.Countdown == "" {
			continue
		}
		to, ok := Of(f.Countdown)
		if !ok {
			t.Errorf("%q counts down on %q, which is not a face", f.Name, f.Countdown)
			continue
		}
		if to.Countdown != "" {
			t.Errorf("%q counts down on %q, which counts down on %q", f.Name, to.Name, to.Countdown)
		}
	}
}
