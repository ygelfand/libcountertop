package dashboard

import (
	"errors"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type failing struct {
	schema.Clock
	err error
}

func (f *failing) Read() schema.Clock { return f.Clock }

func (f *failing) Update(change func(*schema.Clock)) error {
	change(&f.Clock)
	return f.err
}

func TestASaveThatFailsAfterWritingStillRedraws(t *testing.T) {
	clock := &failing{Clock: schema.DefaultClock(), err: errors.New("directory sync failed")}
	drawn := new(redraws)
	c := NewControls(Dependencies{
		Preferences: Preferences{Screen: section(t, schema.DefaultScreen()), Clock: clock},
		Display:     drawn,
	})

	if err := c.Change("face", string(schema.Face("analog"))); err == nil {
		t.Fatal("the failed write was not reported")
	}
	if clock.Face != schema.Face("analog") || *drawn != 1 {
		t.Fatalf("face %q, %d redraws", clock.Face, *drawn)
	}
}

func TestEntitiesKeepTheirIdentity(t *testing.T) {
	c := NewControls(Dependencies{
		Preferences: Preferences{
			Screen: section(t, schema.DefaultScreen()),
			Clock:  section(t, schema.DefaultClock()),
		},
		Display:  new(redraws),
		DeviceID: 1,
	})
	ids := []string{
		"clock_format",
		"clock_face",
		"clock_position",
		"clock_align",
		"clock_size",
		"clock_color",
		"clock_date",
	}
	for i, entity := range c.Entities() {
		var base *esphome.Base
		switch e := entity.(type) {
		case *esphome.Select:
			base = &e.Base
		case *esphome.Switch:
			base = &e.Base
		}
		if base == nil || base.ObjectID != ids[i] || base.DeviceID != 1 {
			t.Fatalf("entity identity %d changed: %+v", i, base)
		}
	}
}
