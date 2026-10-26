package idleview

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/visual"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

func TestTwoVisualsSplitTheScreenTheLongWay(t *testing.T) {
	if got := Areas(1920, 1200, 1); !slices.Equal(got, []ui.Rect{{W: 1920, H: 1200}}) {
		t.Errorf("one: %v", got)
	}
	if got := Areas(1920, 1200, 2); !slices.Equal(
		got,
		[]ui.Rect{{W: 960, H: 1200}, {X: 960, W: 960, H: 1200}},
	) {
		t.Errorf("landscape: %v", got)
	}
	if got := Areas(1200, 1920, 2); !slices.Equal(
		got,
		[]ui.Rect{{W: 1200, H: 960}, {Y: 960, W: 1200, H: 960}},
	) {
		t.Errorf("portrait: %v", got)
	}
	if Areas(1200, 1920, 0) != nil {
		t.Error("areas for no visual")
	}
}

func TestItIsDueOnlyAfterTheWait(t *testing.T) {
	if due(0, time.Hour) {
		t.Error("never came up")
	}
	if due(time.Minute, 30*time.Second) {
		t.Error("came up before the wait")
	}
	if !due(time.Minute, 61*time.Second) {
		t.Error("did not come up after the wait")
	}
}

func TestTheClockTakesItsColoursFromTheVisualUnderIt(t *testing.T) {
	var light theme.Theme
	for _, th := range theme.All {
		if !th.Dark {
			light = th
			break
		}
	}
	dark := []visual.Traits{{}, {}}
	areas := Areas(1920, 1200, 2)

	one := Pieces(light, dark, areas, ui.Rect{X: 100, Y: 100, W: 400, H: 200})
	if len(one) != 1 || !one[0].Palette.Dark ||
		theme.Contrast(one[0].Palette.Text, one[0].Palette.Background) < 4.5 {
		t.Errorf("a light theme over a dark visual gave %+v", one)
	}
	if got := Pieces(light, dark, areas, ui.Rect{X: 700, Y: 100, W: 600, H: 200}); len(got) != 1 ||
		got[0].Clip.W != 0 {
		t.Errorf("a clock across two dark visuals was split: %+v", got)
	}
	if got := Pieces(light, nil, nil, ui.Rect{W: 400, H: 200}); len(got) != 1 ||
		got[0].Palette != light {
		t.Error("with no visual the clock left the theme")
	}
}

func TestAClockAcrossDifferentVisualsIsSplitAtTheSeam(t *testing.T) {
	palette := theme.Default()
	areas := Areas(1920, 1200, 2)
	box := ui.Rect{X: 700, Y: 400, W: 600, H: 300}

	got := Pieces(palette, []visual.Traits{{}, {Light: true}}, areas, box)
	if len(got) != 2 {
		t.Fatalf("%d pieces", len(got))
	}
	if got[0].Clip != (ui.Rect{X: 700, Y: 400, W: 260, H: 300}) ||
		got[1].Clip != (ui.Rect{X: 960, Y: 400, W: 340, H: 300}) {
		t.Errorf("clips %v %v", got[0].Clip, got[1].Clip)
	}
	if !got[0].Palette.Dark || got[1].Palette.Dark {
		t.Error("each half did not take its own visual's tone")
	}
}

func TestOnlyChosenVisualsAreDrawn(t *testing.T) {
	got := Chosen(schema.Idle{Second: schema.IdleVisual{Kind: "orb"}})
	if len(got) != 1 || got[0].Kind != "orb" {
		t.Errorf("chosen %v", got)
	}
}

func TestNoneThenPhotosAreOfferedFirst(t *testing.T) {
	row := NewTable().MustRow("visual1")
	labels := row.Labels()
	if labels[0] != KindLabel("") || labels[1] != KindLabel(schema.IdlePhotos) ||
		len(labels) != len(visual.Built())+2 {
		t.Errorf("labels %v", labels)
	}
	if k := row.ValueOf(KindLabel("orb")); k != "orb" {
		t.Errorf("orb by label: %q", k)
	}
}

func (r *rig) sel(name string) *esphome.Select {
	return r.knobs.Entity(name).(*esphome.Select)
}

type rig struct {
	*Idle
	store *storage.Store[schema.Idle]
	shell *shell.Shell
	ready bool
	still time.Duration
	free  bool
}

func newRig(t *testing.T) *rig {
	t.Helper()
	st, err := storage.Load(filepath.Join(t.TempDir(), "state.json"), schema.DefaultIdle(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{store: st, shell: shell.New(), ready: true, free: true}
	r.Idle = NewIdle(Options{
		Settings: storage.Bind(
			func() *storage.Store[schema.Idle] { return st },
			func(c *schema.Idle) *schema.Idle { return c },
		),
		Shell:    r.shell,
		View:     New(Dependencies{Read: func() Settings { return Settings{} }, Shell: r.shell}),
		Ready:    func() bool { return r.ready },
		Activity: []Activity{func() (time.Duration, bool) { return r.still, true }},
		Free:     func() bool { return r.free },
	})
	return r
}

func TestTheIdleScreenComesUpOnlyWhenQuietAndFree(t *testing.T) {
	r := newRig(t)
	for _, d := range schema.Delays() {
		if d.After() > 0 {
			r.SetAfter(d)
			break
		}
	}
	after := r.store.Get().After.After()
	if after <= 0 {
		t.Fatal("no delay brings the idle screen up")
	}

	r.still = after / 2
	r.step()
	if r.shell.Visible(r.View()) {
		t.Fatal("came up before the wait")
	}

	r.still, r.free = after+time.Second, false
	r.step()
	if r.shell.Visible(r.View()) {
		t.Fatal("came up while something else holds the screen")
	}

	r.free = true
	r.step()
	if !r.shell.Visible(r.View()) {
		t.Fatal("did not come up once quiet and free")
	}

	r.Wake()
	if r.shell.Visible(r.View()) {
		t.Error("waking left the idle screen up")
	}
}

func TestAnActivityThatIsNotKnownDoesNotCount(t *testing.T) {
	r := newRig(t)
	r.options.Activity = append(
		r.options.Activity,
		func() (time.Duration, bool) { return 0, false },
	)
	r.still = time.Hour
	if got := r.quiet(); got != time.Hour {
		t.Errorf("quiet for %v, want an hour", got)
	}
}

func TestEverySettingReachesBothSides(t *testing.T) {
	r := newRig(t)

	r.SetFace(schema.Face("analog"))
	r.SetPosition(schema.PositionTop)
	r.SetAlign(schema.AlignRight)
	r.SetSize(schema.SizeSmall)
	r.SetKind(1, "orb")
	r.SetSource(1, schema.SourceMic)
	r.SetKind(0, "no-such-visual")

	got := r.store.Get()
	if got.Face != schema.Face("analog") || got.Position != schema.PositionTop ||
		got.Align != schema.AlignRight || got.Size != schema.SizeSmall {
		t.Errorf("saved %+v", got)
	}
	if got.Second.Kind != "orb" || got.Second.Source != schema.SourceMic {
		t.Errorf("the second visual saved as %+v", got.Second)
	}
	if got.First != schema.DefaultIdle().First {
		t.Errorf("an unbuilt visual changed the first slot to %+v", got.First)
	}
	if r.sel("visual2").Get() != KindLabel("orb") ||
		r.sel("visual2.source").Get() != schema.SourceMic.Label() {
		t.Error("the second visual's entities did not follow")
	}
}

func TestEveryCommandFromHomeAssistantIsSaved(t *testing.T) {
	r := newRig(t)

	for _, want := range Faces() {
		r.sel("face").OnCommand(want.Label())
		if got := r.store.Get().Face; got != want {
			t.Errorf("%q from Home Assistant saved as %q", want, got)
		}
	}
	r.sel("visual1").OnCommand(KindLabel(""))
	if got := r.store.Get().First.Kind; got != "" {
		t.Errorf("none from Home Assistant saved as %q", got)
	}
	r.sel("face").OnCommand("Sundial")
	if got := r.store.Get().Face; got != Faces()[len(Faces())-1] {
		t.Errorf("an unknown face moved the setting to %q", got)
	}
}

func TestRestorePutsEveryEntityBack(t *testing.T) {
	r := newRig(t)
	c := schema.DefaultIdle()
	c.Face = schema.Face("words")
	c.Second = schema.IdleVisual{Kind: "orb", Source: schema.SourceSpeaker}
	r.Restore(c)

	if r.sel("face").Get() != c.Face.Label() || r.sel("visual2").Get() != KindLabel("orb") ||
		r.sel("visual2.source").Get() != schema.SourceSpeaker.Label() {
		t.Error("restore left an entity behind")
	}
	for _, e := range r.Entities() {
		switch e := e.(type) {
		case *esphome.Select:
			if e.OnCommand == nil {
				t.Errorf("%v takes no command", e)
			}
		case *esphome.Number:
			if e.OnCommand == nil {
				t.Errorf("%v takes no command", e)
			}
		default:
			t.Errorf("%v is neither a select nor a number", e)
		}
	}
}

func TestAWakeKeepsTheIdleScreenDownForTheWait(t *testing.T) {
	r := newRig(t)
	for _, d := range schema.Delays() {
		if d.After() > 0 {
			r.SetAfter(d)
			break
		}
	}
	r.still = r.store.Get().After.After() + time.Second
	r.step()
	if !r.shell.Visible(r.View()) {
		t.Fatal("did not come up after the wait")
	}

	r.Wake()
	r.step()
	if r.shell.Visible(r.View()) {
		t.Fatal("came straight back up after a wake with no other activity")
	}
}
