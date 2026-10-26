package metrics

import (
	"os"
	"path/filepath"
	"testing"
)

func board(t *testing.T) Reader {
	t.Helper()
	root := t.TempDir()

	for path, body := range map[string]string{
		"sys/class/thermal/thermal_zone0/type":               "mtktswmt\n",
		"sys/class/thermal/thermal_zone0/temp":               "46000\n",
		"sys/class/thermal/thermal_zone1/type":               "mtktscpu\n",
		"sys/class/thermal/thermal_zone1/temp":               "45500\n",
		"sys/class/thermal/thermal_zone2/type":               "mtkts1\n",
		"sys/class/thermal/thermal_zone2/temp":               "43400\n",
		"sys/devices/system/cpu/present":                     "0-3\n",
		"sys/devices/system/cpu/online":                      "0-1\n",
		"proc/uptime":                                        "1234.56 4321.00\n",
		"proc/loadavg":                                       "7.40 7.30 7.25 5/623 6841\n",
		"proc/meminfo":                                       "MemTotal:         482956 kB\nMemFree:           25708 kB\nMemAvailable:     247308 kB\n",
		"sys/bus/iio/devices/iio:device2/illuminance0_input": "556\n",
	} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Reader{Root: root}
}

func TestThermalComesBackByZoneNameAsTheKernelReportsIt(t *testing.T) {
	temps := board(t).Thermal()

	for zone, want := range map[string]float64{"mtktscpu": 45500, "mtktswmt": 46000, "mtkts1": 43400} {
		got, ok := temps[zone]
		if !ok {
			t.Errorf("%s missing", zone)
			continue
		}
		if got != want {
			t.Errorf("%s = %v, want %v", zone, got, want)
		}
	}
	if len(temps) != 3 {
		t.Errorf("found %d zones, want 3", len(temps))
	}
}

func TestWifiSignalLeftAtZeroIsUnknown(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "proc/net/wireless")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	for body, want := range map[string]Reading{
		" wlan0: 0000   59.  -37.  -96.       0      0      0      0      0        0\n": known(-37),
		" wlan0: 0000   59.  219.  160.       0      0      0      0      0        0\n": known(-37),
		" wlan0: 0000    0     0     0        0      0      0      0      0        0\n": {},
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if got, _, _ := (Reader{Root: root}).Wifi(); got != want {
			t.Errorf("Wifi() from %q = %+v, want %+v", body, got, want)
		}
	}
}

func TestCoresReportsPresentAndOnline(t *testing.T) {
	present, online := board(t).Cores()

	if !present.Known || present.Value != 4 {
		t.Errorf("present = %+v, want 4", present)
	}
	if !online.Known || online.Value != 2 {
		t.Errorf("online = %+v, want 2", online)
	}
}

func TestCoreListForms(t *testing.T) {
	for list, want := range map[string]float64{
		"0-3":     4,
		"0-1":     2,
		"0":       1,
		"0-1,3":   3,
		"0,2,4-5": 4,
	} {
		root := t.TempDir()
		path := filepath.Join(root, "sys/devices/system/cpu/online")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(list+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		if got := (Reader{Root: root}).count("sys/devices/system/cpu/online"); got.Value != want {
			t.Errorf("%q counted %v, want %v", list, got.Value, want)
		}
	}
}

func TestLoadAndMemory(t *testing.T) {
	r := board(t)

	one, five := r.Load()
	if !one.Known || one.Value != 7.40 || five.Value != 7.30 {
		t.Errorf("load = %v, %v, want 7.40, 7.30", one.Value, five.Value)
	}

	available, total := r.Memory()
	if !available.Known || available.Value != 247308 {
		t.Errorf("available = %+v, want 247308", available)
	}
	if !total.Known || total.Value != 482956 {
		t.Errorf("total = %+v, want 482956", total)
	}
}

func TestUptimeIsTheFirstField(t *testing.T) {
	if got := board(t).Uptime(); !got.Known || got.Value != 1234.56 {
		t.Errorf("Uptime() = %+v, want 1234.56", got)
	}
}

func TestMissingFilesAreUnknownNotZero(t *testing.T) {
	r := Reader{Root: t.TempDir()}

	if temps := r.Thermal(); len(temps) != 0 {
		t.Errorf("found %d zones on an empty root", len(temps))
	}
	present, online := r.Cores()
	one, _ := r.Load()
	available, _ := r.Memory()

	for name, got := range map[string]Reading{
		"present": present, "online": online, "load": one, "available": available,
		"lux": r.Lux(r.LuxPath()), "uptime": r.Uptime(),
	} {
		if got.Known {
			t.Errorf("%s reported %v, want unknown", name, got.Value)
		}
	}
}

func TestLux(t *testing.T) {
	r := board(t)

	path := r.LuxPath()
	if path == "" {
		t.Fatal("LuxPath() found no sensor")
	}

	if got := r.Lux(path); !got.Known || got.Value != 556 {
		t.Errorf("Lux() = %v, want 556", got)
	}
}

func TestLuxFallsBackToTheVendorDriver(t *testing.T) {
	root := t.TempDir()
	client := filepath.Join(root, "sys/bus/i2c/devices/0-0039")
	if err := os.MkdirAll(client, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(client, "als_lux"), []byte("8\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := Reader{Root: root}
	path := r.LuxPath()
	if want := filepath.Join(client, "als_lux"); path != want {
		t.Fatalf("LuxPath() = %q, want %q", path, want)
	}

	if got := r.Lux(path); !got.Known || got.Value != 8 {
		t.Errorf("Lux() = %v, want 8", got)
	}
}
