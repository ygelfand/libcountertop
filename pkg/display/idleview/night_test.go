package idleview

import (
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func darkRig(t *testing.T, mode schema.Night, after schema.Delay) (*rig, *float64) {
	t.Helper()
	r := newRig(t)
	lux := 1.0
	r.options.Ambient = func() (float64, bool) { return lux, true }
	r.SetAfter(after)
	r.SetNight(mode)
	return r, &lux
}

func (r *rig) saverRanFor(d time.Duration) {
	r.mu.Lock()
	r.shown = time.Now().Add(-d)
	r.mu.Unlock()
}

func (r *rig) nightIs(t *testing.T, want schema.Night) {
	t.Helper()
	r.mu.Lock()
	got := r.night
	r.mu.Unlock()
	if got != want || r.View().Night() != want {
		t.Fatalf("night is %q (view %q), want %q", got, r.View().Night(), want)
	}
}

func TestNightFollowsTheSaverAfterItsDelay(t *testing.T) {
	r, _ := darkRig(t, schema.NightClock, schema.Delay1m)
	var heard []schema.Night
	r.Night.Listen(func(n schema.Night) { heard = append(heard, n) })
	wait := r.store.Get().NightAfter.After()

	r.still = time.Minute + time.Second
	r.step()
	if !r.shell.Visible(r.View()) {
		t.Fatal("the saver did not come up")
	}
	r.nightIs(t, schema.NightNone)

	r.saverRanFor(wait / 2)
	r.step()
	r.nightIs(t, schema.NightNone)

	r.saverRanFor(wait + time.Second)
	r.step()
	r.nightIs(t, schema.NightClock)
	if len(heard) != 1 || heard[0] != schema.NightClock {
		t.Errorf("heard %v, want [clock]", heard)
	}
}

func TestAWakeEndsTheNightOnTheDashboard(t *testing.T) {
	r, _ := darkRig(t, schema.NightOff, schema.Delay1m)
	r.still = time.Minute + time.Second
	r.step()
	r.saverRanFor(r.store.Get().NightAfter.After() + time.Second)
	r.step()
	r.nightIs(t, schema.NightOff)

	r.Wake()
	r.step()
	r.nightIs(t, schema.NightNone)
	if r.shell.Visible(r.View()) {
		t.Error("the saver stayed up after the wake")
	}
}

func TestLightBackWakesToTheSaverAndRestartsTheWait(t *testing.T) {
	r, lux := darkRig(t, schema.NightDim, schema.Delay1m)
	r.still = time.Minute + time.Second
	r.step()
	r.saverRanFor(r.store.Get().NightAfter.After() + time.Second)
	r.step()
	r.nightIs(t, schema.NightDim)

	*lux = 100
	r.step()
	r.nightIs(t, schema.NightNone)
	if !r.shell.Visible(r.View()) {
		t.Fatal("light coming back took the saver down")
	}

	*lux = 1
	r.step()
	r.nightIs(t, schema.NightNone)
}

func TestNightHoldsThroughLightAtTheThreshold(t *testing.T) {
	r, lux := darkRig(t, schema.NightClock, schema.Delay1m)
	edge := float64(r.store.Get().NightLux)
	r.still = time.Minute + time.Second
	r.step()
	r.saverRanFor(r.store.Get().NightAfter.After() + time.Second)
	*lux = edge - 0.1
	r.step()
	r.nightIs(t, schema.NightClock)

	*lux = edge + 0.1
	r.step()
	r.nightIs(t, schema.NightClock)

	*lux = edge * 2
	r.step()
	r.nightIs(t, schema.NightNone)
}

func TestWithoutASaverNightStandsInForIt(t *testing.T) {
	r, _ := darkRig(t, schema.NightClock, schema.DelayNever)
	wait := r.store.Get().NightAfter.After()

	r.still = wait / 2
	r.step()
	if r.shell.Visible(r.View()) {
		t.Fatal("night came up before its wait")
	}

	r.still = wait + time.Second
	r.step()
	if !r.shell.Visible(r.View()) {
		t.Fatal("night did not bring the screen down")
	}
	r.nightIs(t, schema.NightClock)
}

func TestNightNeedsALightReading(t *testing.T) {
	r, _ := darkRig(t, schema.NightOff, schema.DelayNever)
	r.options.Ambient = func() (float64, bool) { return 0, false }
	r.still = time.Hour
	r.step()
	r.nightIs(t, schema.NightNone)
	if r.shell.Visible(r.View()) {
		t.Error("night came up with no light reading")
	}
}

func TestQuietNightsDrawNoVisuals(t *testing.T) {
	c := schema.DefaultIdle()
	c.First = schema.IdleVisual{Kind: "orb", Source: schema.SourceMic}
	c.Face = schema.Face("analog")
	c.Placement = schema.Placement{
		Position: schema.PositionTop,
		Align:    schema.AlignLeft,
		Size:     schema.SizeSmall,
	}

	if got := Dark(c, schema.DefaultFace, schema.NightDim); got != c {
		t.Errorf("dim changed the saver to %+v", got)
	}

	clock := Dark(c, schema.DefaultFace, schema.NightClock)
	if len(Chosen(clock)) != 0 || clock.Face != schema.Face("analog") ||
		clock.Position != schema.PositionCenter || clock.Align != schema.AlignCenter ||
		clock.Size != schema.SizeLarge {
		t.Errorf("clock night drew %+v", clock)
	}

	c.Face = schema.FaceNone
	if got := Dark(c, schema.DefaultFace, schema.NightClock); got.Face != schema.DefaultFace {
		t.Errorf("a saver with no clock fell back to %q, want the dashboard face", got.Face)
	}

	off := Dark(c, schema.DefaultFace, schema.NightOff)
	if len(Chosen(off)) != 0 || off.Face != schema.FaceNone {
		t.Errorf("off night drew %+v", off)
	}
}
