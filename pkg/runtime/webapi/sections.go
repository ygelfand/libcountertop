package webapi

import (
	"slices"

	setting "github.com/ygelfand/libcountertop/pkg/settings"
)

type section struct {
	label   string
	domains []string
}

var sections = []section{
	{"settings.network", []string{"network", "certs", "access"}},
	{"settings.features", []string{
		"cast", "media", "bluetooth", "rtsp", "call",
		"calendar", "todo",
	}},
	{"settings.display", []string{
		"screen", "clock", "idle", "visual", "poster", "photos", "weather", "apps", "cameras",
		"doorbell", "homealarm", "presence",
	}},
	{"home.title", []string{"home"}},
	{"settings.volume", []string{"feedback", "microphone", "speaker"}},
	{"alerts.page", []string{"alerts"}},
	{"settings.camera", []string{"camera"}},
	{"voice.title", []string{"voice"}},
	{"settings.assistants", []string{"assistants", "ring"}},
	{systemSection, []string{"diag"}},
}

const systemSection = "settings.system"

func placed(domain string) (int, string, bool) {
	for i, s := range sections {
		if slices.Contains(s.domains, domain) {
			return i, s.label, true
		}
	}
	return 0, "", false
}

func sectionOf(domain string) (int, string) {
	if order, key, ok := placed(domain); ok {
		return order, key
	}
	return len(sections), systemSection
}

func Unplaced(r *setting.Registry) []string {
	var out []string
	for _, b := range r.All() {
		if _, _, ok := placed(b.Domain()); !ok {
			out = append(out, b.Domain())
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
