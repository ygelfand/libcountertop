package schema

import (
	"slices"
	"testing"

	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

func homes(s *storage.Store[root]) HomeWriter {
	return NewHomeWriter(storage.Field(s, func(r *root) *Home { return &r.Home }))
}

func TestHomePicksSurviveARestart(t *testing.T) {
	s, path := load(t)

	want := HomePick{
		Labels:   []string{"kitchen"},
		Areas:    []string{"kitchen"},
		Entities: []string{"light.a"},
	}
	if err := homes(s).Pick("lights", want); err != nil {
		t.Fatal(err)
	}
	if err := homes(s).Pick("switches", HomePick{All: true}); err != nil {
		t.Fatal(err)
	}

	again, err := storage.Load(path, root{}, cloneRoot)
	if err != nil {
		t.Fatal(err)
	}
	home := again.Get().Home
	got := home.Picked("lights")
	if got.All || !slices.Equal(got.Labels, want.Labels) || !slices.Equal(got.Areas, want.Areas) ||
		!slices.Equal(got.Entities, want.Entities) {
		t.Errorf("lights came back as %+v", got)
	}
	if !home.Picked("switches").All {
		t.Errorf("switches came back as %+v", home.Picked("switches"))
	}
}

func TestHomePickDoesNotChangeWhatWasRead(t *testing.T) {
	s, _ := load(t)

	if err := homes(s).Pick("lights", HomePick{Entities: []string{"light.a"}}); err != nil {
		t.Fatal(err)
	}
	before := s.Get().Home

	if err := homes(s).Pick("lights", HomePick{}); err != nil {
		t.Fatal(err)
	}
	if got := before.Picked("lights").Entities; !slices.Equal(got, []string{"light.a"}) {
		t.Errorf("an earlier read changed to %v", got)
	}
	if got := s.Get().Home.Picked("lights"); !got.Empty() {
		t.Errorf("cleared lights read as %+v", got)
	}
}

func TestAllKeepsTheManualPicks(t *testing.T) {
	s, _ := load(t)

	p := HomePick{Areas: []string{"kitchen"}, Entities: []string{"light.a"}}
	p.All = true
	if err := homes(s).Pick("lights", p); err != nil {
		t.Fatal(err)
	}
	p.All = false
	if err := homes(s).Pick("lights", p); err != nil {
		t.Fatal(err)
	}
	got := s.Get().Home.Picked("lights")
	if got.All || !slices.Equal(got.Areas, []string{"kitchen"}) ||
		!slices.Equal(got.Entities, []string{"light.a"}) {
		t.Errorf("after All on and off: %+v", got)
	}
}

func TestATesseraGridKeepsAPageWithinItsCells(t *testing.T) {
	for _, tt := range []struct{ in, fallback, want TesseraGrid }{
		{TesseraGrid{}, TesseraGrid{Columns: 8, Rows: 5}, TesseraGrid{Columns: 8, Rows: 5}},
		{TesseraGrid{Columns: 12, Rows: 12}, TesseraGrid{}, TesseraGrid{Columns: 12, Rows: 5}},
		{TesseraGrid{Columns: 20, Rows: 2}, TesseraGrid{}, TesseraGrid{Columns: 12, Rows: 2}},
		{TesseraGrid{Columns: 8, Rows: 8}, TesseraGrid{}, TesseraGrid{Columns: 8, Rows: 8}},
	} {
		if got := tt.in.Or(tt.fallback); got != tt.want {
			t.Errorf("%+v.Or(%+v) = %+v, want %+v", tt.in, tt.fallback, got, tt.want)
		}
	}
}

func TestMigrateHomeMovesTesseraNativeToEmbedded(t *testing.T) {
	for in, want := range map[HomeMode]HomeMode{
		HomeTessera:  HomeFirmware,
		HomeFirmware: HomeFirmware,
		HomeNative:   HomeNative,
		HomeLovelace: HomeLovelace,
	} {
		h := Home{Mode: in}
		if err := MigrateHome(nil, &h); err != nil {
			t.Fatal(err)
		}
		if h.Mode != want {
			t.Errorf("MigrateHome(%q) = %q, want %q", in, h.Mode, want)
		}
	}
	if slices.Contains(HomeModes(), HomeTessera) {
		t.Error("HomeModes still offers Tessera native")
	}
	if got := (Home{Mode: HomeLovelace}).Speaks(); got != HomeLovelace {
		t.Errorf("Speaks() = %q for the Home Assistant mode", got)
	}
}
