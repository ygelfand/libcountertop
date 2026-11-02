package lovelaceview

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

const page = `
var calls = [], bus = [], stored = {}, timers = [];
var view = {
	children: [],
	getElementById(id) { return this.children.find((c) => c.id === id) || null; },
	appendChild(c) { this.children.push(c); },
};
var root = { style: { props: {}, setProperty(k, v) { this.props[k] = v; } } };
var ha = null;
function dashboardOpens() {
	var panel = { shadowRoot: { querySelector: (s) => (s === "hui-root" ? { shadowRoot: view } : null) } };
	var main = { shadowRoot: { querySelector: (s) => (s === "partial-panel-resolver > *" ? panel : null) } };
	ha = { shadowRoot: { querySelector: (s) => (s === "home-assistant-main" ? main : null) } };
}
var document = {
	documentElement: root,
	querySelector: (s) => (s === "home-assistant" ? ha : null),
	createElement: (tag) => ({ tag }),
};
var localStorage = { setItem(k, v) { stored[k] = v; } };
function setInterval(f) { timers.push(f); }
function setTimeout(f) { f(); }
function tick() { timers.forEach((f) => f()); }
var window = {
	countertopAuth(s) { calls.push(JSON.parse(s)); },
	externalBus(m) { bus.push(m); },
};
`

func loadBridge(t *testing.T) *goja.Runtime {
	t.Helper()
	vm := goja.New()
	if _, err := vm.RunString(page); err != nil {
		t.Fatalf("fake page: %v", err)
	}
	if _, err := vm.RunString(bridge); err != nil {
		t.Fatalf("bridge.js: %v", err)
	}
	return vm
}

func run(t *testing.T, vm *goja.Runtime, js string) string {
	t.Helper()
	v, err := vm.RunString("JSON.stringify(" + js + ")")
	if err != nil {
		t.Fatalf("%s: %v", js, err)
	}
	return v.String()
}

func do(t *testing.T, vm *goja.Runtime, js string) {
	t.Helper()
	if _, err := vm.RunString(js); err != nil {
		t.Fatalf("%s: %v", js, err)
	}
}

func TestBridgeHidesTheSidebar(t *testing.T) {
	vm := loadBridge(t)
	if got := run(t, vm, "stored.dockedSidebar"); got != `"\"always_hidden\""` {
		t.Errorf("dockedSidebar stored as %s", got)
	}
}

func TestBridgeAsksTheDeviceForSignIn(t *testing.T) {
	vm := loadBridge(t)
	do(
		t,
		vm,
		`window.externalApp.getExternalAuth(JSON.stringify({callback: "externalAuthSetToken"}))`,
	)
	do(
		t,
		vm,
		`window.externalApp.revokeExternalAuth(JSON.stringify({callback: "externalAuthRevokeToken"}))`,
	)
	want := `[{"kind":"get","callback":"externalAuthSetToken"},{"kind":"revoke","callback":"externalAuthRevokeToken"}]`
	if got := run(t, vm, "calls"); got != want {
		t.Errorf("binding calls %s, want %s", got, want)
	}
}

func TestBridgeOffersNoExtraAppFeatures(t *testing.T) {
	vm := loadBridge(t)
	do(t, vm, `window.externalApp.externalBus(JSON.stringify({id: 7, type: "config/get"}))`)
	var answers []struct {
		ID      int            `json:"id"`
		Type    string         `json:"type"`
		Success bool           `json:"success"`
		Result  map[string]any `json:"result"`
	}
	if err := json.Unmarshal([]byte(run(t, vm, "bus")), &answers); err != nil {
		t.Fatal(err)
	}
	if len(answers) != 1 || answers[0].ID != 7 || answers[0].Type != "result" ||
		!answers[0].Success {
		t.Fatalf("answers %+v", answers)
	}
	for _, feature := range []string{"hasSettingsScreen", "hasAssist", "canCommissionMatter", "canWriteTag"} {
		if on, ok := answers[0].Result[feature].(bool); !ok || on {
			t.Errorf("%s = %v", feature, answers[0].Result[feature])
		}
	}
}

func TestBridgeHidesTheHeaderOnceTheDashboardOpens(t *testing.T) {
	vm := loadBridge(t)
	do(t, vm, "tick()")
	if got := run(t, vm, "view.children.length"); got != "0" {
		t.Fatalf("styled %s children before the dashboard opened", got)
	}
	do(t, vm, "dashboardOpens(); tick(); tick();")
	if got := run(t, vm, "view.children.length"); got != "1" {
		t.Fatalf("added %s styles, want one", got)
	}
	css := run(t, vm, "view.children[0].textContent")
	if !strings.Contains(css, ".header{display:none") {
		t.Errorf("style %s", css)
	}
	if got := run(t, vm, `root.style.props["--header-height"]`); got != `"0px"` {
		t.Errorf("--header-height %s", got)
	}
}
