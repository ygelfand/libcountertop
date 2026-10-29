package schema

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

type product struct {
	Shared
	Own int `json:"own"`
}

func TestSharedFlattensIntoTheEmbeddingStruct(t *testing.T) {
	data, err := json.Marshal(product{Shared: DefaultShared()})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	want := []string{
		"access",
		"airplay",
		"alerts",
		"api",
		"apps",
		"bluetooth",
		"calendar",
		"call",
		"camera",
		"cameras",
		"cast",
		"certs",
		"clock",
		"diag",
		"doorbell",
		"feedback",
		"frontend",
		"home",
		"home_alarm",
		"idle",
		"lock",
		"media",
		"network",
		"own",
		"photos",
		"poster",
		"radio",
		"rtsp",
		"screen",
		"sendspin",
		"thermostat",
		"time",
		"todo",
		"update",
		"visual",
		"voice",
		"wake",
		"weather",
	}
	got := make([]string, 0, len(top))
	for key := range top {
		got = append(got, key)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("top-level keys = %v, want %v", got, want)
	}
}

func TestSectionsReachTheSharedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := storage.Load(path, product{Shared: DefaultShared()}, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	sections := NewSections(
		func() *storage.Store[product] { return st },
		func(p *product) *Shared { return &p.Shared },
	)

	if err := sections.Writer().API().Adopted(true); err != nil {
		t.Fatalf("Adopted: %v", err)
	}
	if !sections.API.Read().Adopted {
		t.Error("the API section did not read back the write")
	}

	again, err := storage.Load(path, product{Shared: DefaultShared()}, nil)
	if err != nil {
		t.Fatalf("reloading: %v", err)
	}
	if !again.Get().API.Adopted {
		t.Error("the write did not reach the file")
	}
}
