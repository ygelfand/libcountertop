package lovelaceview

import (
	"encoding/json"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/compositor"
)

const (
	failedLoad  = `{"frame":{"id":"main","unreachableUrl":"http://ha.local:8123/lovelace"}}`
	loaded      = `{"frame":{"id":"main","url":"http://ha.local:8123/lovelace"}}`
	childFailed = `{"frame":{"id":"ad","parentId":"main","unreachableUrl":"http://ads.example/"}}`
)

func testView(t *testing.T, theme string) *View {
	t.Helper()
	v := NewView(Options{Name: "test", Theme: func() string { return theme }})
	t.Cleanup(func() {
		v.mu.Lock()
		v.stopRetry()
		v.mu.Unlock()
	})
	return v
}

func (v *View) state() (failed, healing bool, wait time.Duration) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.failed, v.healing, v.wait
}

func TestThemeScriptStoresTheChosenTheme(t *testing.T) {
	for _, name := range []string{"Kitchen", `A "quoted" \ theme`} {
		script := testView(t, name).themeScript()
		const head, tail = `try { localStorage.setItem("selectedTheme", `, `); } catch (e) {}`
		if !strings.HasPrefix(script, head) || !strings.HasSuffix(script, tail) {
			t.Fatalf("%q: script %s", name, script)
		}
		var stored string
		if err := json.Unmarshal(
			[]byte(strings.TrimSuffix(strings.TrimPrefix(script, head), tail)),
			&stored,
		); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		var chosen struct{ Theme string }
		if err := json.Unmarshal([]byte(stored), &chosen); err != nil || chosen.Theme != name {
			t.Errorf("%q: stored %s", name, stored)
		}
	}
}

func TestThemeScriptClearsForTheDefault(t *testing.T) {
	if got := testView(t, "").themeScript(); !strings.Contains(got, `removeItem("selectedTheme")`) {
		t.Errorf("default theme script %s", got)
	}
}

func TestFailedLoadsBackOffToAMinute(t *testing.T) {
	v := testView(t, "")
	want := []time.Duration{
		10 * time.Second,
		20 * time.Second,
		40 * time.Second,
		time.Minute,
		time.Minute,
	}
	for i, next := range want {
		v.navigated(nil, json.RawMessage(failedLoad))
		failed, _, wait := v.state()
		if !failed || wait != next {
			t.Fatalf("failure %d: failed %v, next wait %s, want %s", i+1, failed, wait, next)
		}
	}
}

func TestRecoveryWaitsForTheFirstNewFrame(t *testing.T) {
	v := testView(t, "")
	v.navigated(nil, json.RawMessage(failedLoad))
	v.navigated(nil, json.RawMessage(loaded))
	if failed, healing, wait := v.state(); !failed || !healing || wait != 0 {
		t.Fatalf("after the page loaded: failed %v, healing %v, wait %s", failed, healing, wait)
	}
	v.frame(compositor.Frame{
		Width:  2,
		Height: 2,
		Stride: 8,
		Pixels: make([]byte, 16),
		Damage: []image.Rectangle{image.Rect(0, 0, 1, 1)},
	})
	if failed, healing, _ := v.state(); failed || healing {
		t.Errorf("after the first frame: failed %v, healing %v", failed, healing)
	}
}

func TestChildFrameFailuresAreIgnored(t *testing.T) {
	v := testView(t, "")
	v.navigated(nil, json.RawMessage(childFailed))
	if failed, _, _ := v.state(); failed {
		t.Error("a failed child frame marked the dashboard unreachable")
	}
}
