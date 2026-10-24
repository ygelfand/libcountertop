package preferences

import (
	"slices"
	"testing"
)

func TestProviderReadsLiveSettingsAndSavesThroughProduct(t *testing.T) {
	live := Settings{YouTube: YouTube{Skip: []string{"intro"}}}
	var saved []string
	p := Provider{
		Read:   func() Settings { return live },
		Prefix: "speaker",
		Save:   func(d, m, v string) error { saved = []string{d, m, v}; return nil },
	}
	if !slices.Equal(p.Settings().YouTube.Skip, []string{"intro"}) {
		t.Fatal(p.Settings())
	}
	live.YouTube.Skip = []string{"outro"}
	if !slices.Equal(p.Settings().YouTube.Skip, []string{"outro"}) {
		t.Fatal("cached preferences")
	}
	if err := p.SaveScreens("d", "m", "v"); err != nil || len(saved) != 3 || saved[2] != "v" {
		t.Fatal(saved, err)
	}
	if p.RequestIDPrefix() != "speaker" {
		t.Fatal(p.RequestIDPrefix())
	}
}

func TestAnEmptyProviderFallsBackToDefaults(t *testing.T) {
	var p Provider
	if !slices.Equal(p.Settings().YouTube.Skip, []string{"sponsor"}) ||
		p.RequestIDPrefix() != "countertop" {
		t.Fatal(p.Settings(), p.RequestIDPrefix())
	}
	if err := p.SaveScreens("d", "m", "v"); err != nil {
		t.Fatal(err)
	}
}
