package dashboard

import (
	"path/filepath"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/display/clockview"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

func section[T any](t *testing.T, defaults T) storage.Binding[T, T] {
	t.Helper()
	st, err := storage.Load(filepath.Join(t.TempDir(), "state.json"), defaults, nil)
	if err != nil {
		t.Fatal(err)
	}
	return storage.Bind(func() *storage.Store[T] { return st }, func(c *T) *T { return c })
}

type redraws int

func (r *redraws) Redraw() { *r++ }

type rig struct {
	*Controls
	prefs Preferences
	drawn *redraws
}

func dash(t *testing.T) rig {
	t.Helper()
	p := Preferences{
		Screen: section(t, schema.DefaultScreen()),
		Clock:  section(t, schema.DefaultClock()),
	}
	drawn := new(redraws)
	return rig{NewControls(Dependencies{Preferences: p, Display: drawn}), p, drawn}
}

func (r rig) clock() schema.Clock   { return r.prefs.Clock.Read() }
func (r rig) screen() schema.Screen { return r.prefs.Screen.Read() }

func (r rig) sel(name string) *esphome.Select { return r.Entity(name).(*esphome.Select) }

func (r rig) toggle(name string) *esphome.Switch { return r.Entity(name).(*esphome.Switch) }

func TestEverySelectReachesBothSides(t *testing.T) {
	d := dash(t)

	t.Run("face", func(t *testing.T) {
		for _, want := range clockview.Names() {
			d.SetFace(want)

			if got := d.clock().Face; got != want {
				t.Errorf("the file says %q, want %q", got, want)
			}
			if got := d.sel("face").Get(); got != want.Label() {
				t.Errorf("the entity says %q, want %q", got, want.Label())
			}
		}
	})

	t.Run("position", func(t *testing.T) {
		for _, want := range schema.Positions() {
			d.SetPosition(want)

			if got := d.clock().Position; got != want {
				t.Errorf("the file says %q, want %q", got, want)
			}
			if got := d.sel("position").Get(); got != want.Label() {
				t.Errorf("the entity says %q, want %q", got, want.Label())
			}
		}
	})

	t.Run("size", func(t *testing.T) {
		for _, want := range schema.Sizes() {
			d.SetSize(want)

			if got := d.clock().Size; got != want {
				t.Errorf("the file says %q, want %q", got, want)
			}
			if got := d.sel("size").Get(); got != want.Label() {
				t.Errorf("the entity says %q, want %q", got, want.Label())
			}
		}
	})

	t.Run("color", func(t *testing.T) {
		for _, want := range schema.Inks() {
			d.SetInk(want)

			if got := d.clock().Ink; got != want {
				t.Errorf("the file says %q, want %q", got, want)
			}
			if got := d.sel("color").Get(); got != want.Label() {
				t.Errorf("the entity says %q, want %q", got, want.Label())
			}
		}
	})

	t.Run("hours", func(t *testing.T) {
		for _, want := range schema.HourFormats() {
			d.SetHours(want)

			if got := d.screen().Hours; got != want {
				t.Errorf("the file says %q, want %q", got, want)
			}
			if got := d.sel("hours").Get(); got != want.Label() {
				t.Errorf("the entity says %q, want %q", got, want.Label())
			}
		}
	})
}

func TestEverySwitchReachesBothSides(t *testing.T) {
	d := dash(t)

	for _, want := range []bool{true, false, true} {
		d.SetDate(want)

		if got := d.clock().Date; got != want {
			t.Errorf("the date in the file is %v, want %v", got, want)
		}
		if got := d.toggle("date").Get(); got != want {
			t.Errorf("the date entity is %v, want %v", got, want)
		}
	}
}

func TestEveryCommandFromHomeAssistantIsSaved(t *testing.T) {
	d := dash(t)

	for _, want := range clockview.Names() {
		d.sel("face").OnCommand(want.Label())

		if got := d.clock().Face; got != want {
			t.Errorf("%q from Home Assistant saved as %q", want, got)
		}
	}

	for _, want := range schema.Inks() {
		d.sel("color").OnCommand(want.Label())

		if got := d.clock().Ink; got != want {
			t.Errorf("%q from Home Assistant saved as %q", want, got)
		}
	}

	d.toggle("date").OnCommand(false)
	if d.clock().Date {
		t.Error("turning the date off from Home Assistant did not save")
	}
	if *d.drawn == 0 {
		t.Error("a saved change did not redraw")
	}
}

func TestAnUnknownLabelChangesNothing(t *testing.T) {
	d := dash(t)

	d.SetFace(schema.Face("analog"))
	d.sel("face").OnCommand("Sundial")

	if got := d.clock().Face; got != schema.Face("analog") {
		t.Errorf("an unknown face left the setting on %q", got)
	}
}

func TestRestorePutsEveryEntityBack(t *testing.T) {
	d := dash(t)

	s, c := schema.DefaultScreen(), schema.DefaultClock()
	c.Face = schema.Face("words")
	c.Position = schema.PositionTop
	c.Size = schema.SizeSmall
	c.Ink = schema.InkAmber
	c.Date = false
	s.Hours = schema.TwelveHour

	d.Restore(s, c)

	for _, at := range []struct {
		name string
		got  string
		want string
	}{
		{"face", d.sel("face").Get(), c.Face.Label()},
		{"position", d.sel("position").Get(), c.Position.Label()},
		{"size", d.sel("size").Get(), c.Size.Label()},
		{"color", d.sel("color").Get(), c.Ink.Label()},
		{"hours", d.sel("hours").Get(), s.Hours.Label()},
	} {
		if at.got != at.want {
			t.Errorf("after restoring, %s says %q, want %q", at.name, at.got, at.want)
		}
	}

	if d.toggle("date").Get() != c.Date {
		t.Errorf("after restoring, the date is %v, want %v", d.toggle("date").Get(), c.Date)
	}
}

func TestEveryEntityTakesACommand(t *testing.T) {
	d := dash(t)

	for _, e := range d.Entities() {
		switch v := e.(type) {
		case *esphome.Select:
			if v.OnCommand == nil {
				t.Errorf("%s takes no command", v.ObjectID)
			}
		case *esphome.Switch:
			if v.OnCommand == nil {
				t.Errorf("%s takes no command", v.ObjectID)
			}
		default:
			t.Errorf("%T is neither a select nor a switch", e)
		}
	}
}
