package firmwarerelay

import "testing"

func TestHardwareDiagnosticsStayOnTheHelper(t *testing.T) {
	for _, name := range []string{"Wi-Fi co-processor", "Panel watch", "Panel misses", "Psram Free"} {
		if !blocked[slug(name)] {
			t.Errorf("%q (%s) reaches Home Assistant", name, slug(name))
		}
	}
	for _, name := range []string{"Pressure", "Debug mode", "Auto standby"} {
		if blocked[slug(name)] {
			t.Errorf("%q is blocked", name)
		}
	}
}
