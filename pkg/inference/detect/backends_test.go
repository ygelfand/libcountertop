package detect

import (
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/inference/wake"
)

type fake struct {
	loaded map[string]float64
	fed    int
	closed bool
}

func (f *fake) load(key string, _ wake.Model, _ int) error {
	f.loaded[key] = 0
	return nil
}

func (f *fake) unload(key string) { delete(f.loaded, key) }

func (f *fake) feed([]int16) (map[string]float64, bool) {
	f.fed++
	return f.loaded, true
}

func (f *fake) close() { f.closed = true }

func fakes(t *testing.T) map[wake.Kind]*fake {
	t.Helper()

	made := map[wake.Kind]*fake{}
	was := build
	build = func(k wake.Kind) (backend, error) {
		f := &fake{loaded: map[string]float64{}}
		made[k] = f
		return f, nil
	}
	t.Cleanup(func() { build = was })
	return made
}

func model(id string, k wake.Kind) wake.Model {
	return wake.Model{ID: id, Phrase: id, Kind: k, Path: id + ".tflite"}
}

func TestSlotsOfDifferentKindsBothRun(t *testing.T) {
	made := fakes(t)
	e := New(2, nil)

	fired := make(chan int, 2)
	e.OnDetect = func(slot int) { fired <- slot }

	if err := e.Use(0, model("glados", wake.KindOpenWakeWord)); err != nil {
		t.Fatalf("Use(0): %v", err)
	}
	if err := e.Use(1, model("hey_jarvis", wake.KindMicroWakeWord)); err != nil {
		t.Fatalf("Use(1): %v", err)
	}
	if len(made) != 2 {
		t.Fatalf("built %d engines, want one per kind", len(made))
	}

	made[wake.KindOpenWakeWord].loaded["glados"] = 0.1
	made[wake.KindMicroWakeWord].loaded["hey_jarvis"] = 0.99
	e.score([]int16{0})

	for k, f := range made {
		if f.fed != 1 {
			t.Errorf("%s fed %d times, want 1", k, f.fed)
		}
	}

	select {
	case slot := <-fired:
		if slot != 1 {
			t.Errorf("slot %d fired, want 1", slot+1)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing fired")
	}
	select {
	case slot := <-fired:
		t.Errorf("slot %d fired too, want only the one over its threshold", slot+1)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestAnEngineGoesWithItsLastSlot(t *testing.T) {
	made := fakes(t)
	e := New(2, nil)

	if err := e.Use(0, model("glados", wake.KindOpenWakeWord)); err != nil {
		t.Fatalf("Use(0): %v", err)
	}
	if err := e.Use(1, model("hey_jarvis", wake.KindMicroWakeWord)); err != nil {
		t.Fatalf("Use(1): %v", err)
	}

	e.Clear(1)
	if !made[wake.KindMicroWakeWord].closed {
		t.Error("microWakeWord stayed up with no slot using it")
	}
	if made[wake.KindOpenWakeWord].closed {
		t.Error("openWakeWord went with the other engine's slot")
	}
	if len(e.backends) != 1 {
		t.Errorf("%d engines still up, want 1", len(e.backends))
	}

	e.Clear(0)
	if !made[wake.KindOpenWakeWord].closed {
		t.Error("openWakeWord stayed up with nothing loaded")
	}
	if len(e.backends) != 0 {
		t.Errorf("%d engines still up, want none", len(e.backends))
	}
}

func TestOneModelInTwoSlots(t *testing.T) {
	made := fakes(t)
	e := New(2, nil)

	for slot := range 2 {
		if err := e.Use(slot, model("glados", wake.KindOpenWakeWord)); err != nil {
			t.Fatalf("Use(%d): %v", slot, err)
		}
	}

	e.Clear(0)
	f := made[wake.KindOpenWakeWord]
	if f.closed {
		t.Error("the engine closed while a slot was still using it")
	}
	if _, ok := f.loaded["glados"]; !ok {
		t.Error("the model was unloaded while a slot was still using it")
	}

	e.Clear(1)
	if _, ok := f.loaded["glados"]; ok {
		t.Error("the model stayed loaded with no slot using it")
	}
	if !f.closed {
		t.Error("the engine stayed up with nothing loaded")
	}
}

func TestOnePryonWordInTwoSlotsLoadsTwice(t *testing.T) {
	f := &fake{loaded: map[string]float64{}}
	e := New(2, nil)
	e.backends[wake.KindPryon] = f

	fired := make(chan int, 2)
	e.OnDetect = func(slot int) { fired <- slot }

	for slot := range 2 {
		if err := e.Use(slot, model("en-US/ALEXA", wake.KindPryon)); err != nil {
			t.Fatalf("Use(%d): %v", slot, err)
		}
	}
	if len(f.loaded) != 2 {
		t.Fatalf("loaded %v, want one per slot", f.loaded)
	}

	f.loaded[e.slots[1].key] = 0.3
	e.score([]int16{0})
	select {
	case slot := <-fired:
		if slot != 1 {
			t.Errorf("slot %d fired, want 2", slot+1)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing fired")
	}
	select {
	case slot := <-fired:
		t.Errorf("slot %d fired too, want only the slot whose decoder accepted", slot+1)
	case <-time.After(100 * time.Millisecond):
	}

	kept := e.slots[1].key
	e.Clear(0)
	if len(f.loaded) != 1 {
		t.Errorf("loaded %v after clearing slot 1, want only slot 2's", f.loaded)
	}
	if _, ok := f.loaded[kept]; !ok {
		t.Error("clearing slot 1 unloaded slot 2's decoder")
	}
}

func TestThresholdsMapOntoPryonSensitivity(t *testing.T) {
	cases := []struct {
		slot      int
		threshold float64
		want      int
	}{
		{0, wake.DefaultThreshold, 0},
		{0, 0.5, -10},
		{0, 0.99, 10},
		{0, 0.92, 5},
		{0, 0.675, -5},
		{0, 0.2, -10},
		{StopSlot, 0.7, 0},
		{StopSlot, 0.5, -10},
		{StopSlot, 0.99, 10},
	}
	for _, c := range cases {
		e := New(StopSlot+1, nil)
		e.Threshold = func(int) float64 { return c.threshold }
		if got := e.sensitivity(c.slot); got != c.want {
			t.Errorf("slot %d at %v: sensitivity %d, want %d", c.slot+1, c.threshold, got, c.want)
		}
	}
}

func TestMovingAPryonSliderReloadsItsSlot(t *testing.T) {
	f := &fake{loaded: map[string]float64{}}
	e := New(1, nil)
	e.backends[wake.KindPryon] = f
	threshold := wake.DefaultThreshold
	e.Threshold = func(int) float64 { return threshold }

	if err := e.Use(0, model("pryon_alexa", wake.KindPryon)); err != nil {
		t.Fatalf("Use: %v", err)
	}
	before := e.slots[0].key

	e.score([]int16{0})
	if e.slots[0].key != before || len(f.loaded) != 1 {
		t.Fatalf("an unchanged slider reloaded: %v", f.loaded)
	}

	threshold = 0.99
	e.score([]int16{0})
	after := e.slots[0].key
	if after == before {
		t.Fatal("the slot kept its decoder after the slider moved")
	}
	if _, ok := f.loaded[before]; ok || len(f.loaded) != 1 {
		t.Errorf("loaded %v, want only %s", f.loaded, after)
	}
}

func TestSwappingWithinAKindKeepsTheEngine(t *testing.T) {
	made := fakes(t)
	e := New(1, nil)

	if err := e.Use(0, model("glados", wake.KindOpenWakeWord)); err != nil {
		t.Fatalf("Use: %v", err)
	}
	if err := e.Use(0, model("wall-e", wake.KindOpenWakeWord)); err != nil {
		t.Fatalf("Use again: %v", err)
	}

	f := made[wake.KindOpenWakeWord]
	if f.closed {
		t.Fatal("the engine was closed under the model that replaced it")
	}
	if _, ok := f.loaded["glados"]; ok {
		t.Error("the model that was replaced is still loaded")
	}
	if _, ok := f.loaded["wall-e"]; !ok {
		t.Error("the model that replaced it is not loaded")
	}
}
