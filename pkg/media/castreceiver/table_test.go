package castreceiver

import (
	"testing"

	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func TestAnEmptySkipListIsSavedEmpty(t *testing.T) {
	c := schema.DefaultCast()
	if err := NewTable().MustRow("youtube.skip").Write(&c, ""); err != nil {
		t.Fatal(err)
	}
	if c.YouTube.Skip == nil || len(c.YouTube.Skip) != 0 {
		t.Fatalf("skip is %#v", c.YouTube.Skip)
	}
}
