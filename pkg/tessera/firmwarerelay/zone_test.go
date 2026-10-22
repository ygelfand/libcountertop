package firmwarerelay

import (
	"testing"

	"github.com/ygelfand/go-esphome-device/api"
)

func TestParseZone(t *testing.T) {
	mwd := api.DSTRuleType_DST_RULE_TYPE_MONTH_WEEK_DAY
	cases := []struct {
		spec        string
		std, dst    int32
		start, end  *api.DSTRule
		unparseable bool
	}{
		{spec: "UTC0", std: 0, dst: 0},
		{spec: "EST5EDT,M3.2.0,M11.1.0", std: 5 * 3600, dst: 4 * 3600,
			start: &api.DSTRule{Type: mwd, Month: 3, Week: 2, DayOfWeek: 0, TimeSeconds: 7200},
			end:   &api.DSTRule{Type: mwd, Month: 11, Week: 1, DayOfWeek: 0, TimeSeconds: 7200}},
		{spec: "CET-1CEST,M3.5.0,M10.5.0/3", std: -3600, dst: -7200,
			start: &api.DSTRule{Type: mwd, Month: 3, Week: 5, DayOfWeek: 0, TimeSeconds: 7200},
			end:   &api.DSTRule{Type: mwd, Month: 10, Week: 5, DayOfWeek: 0, TimeSeconds: 10800}},
		{spec: "<+0530>-5:30", std: -(5*3600 + 30*60), dst: -(5*3600 + 30*60)},
		{spec: "", unparseable: true},
		{spec: "EST5EDT,M3.2", unparseable: true},
	}
	for _, c := range cases {
		tz, ok := parseZone(c.spec)
		if ok == c.unparseable {
			t.Errorf("%q: parsed %v, want %v", c.spec, ok, !c.unparseable)
			continue
		}
		if !ok {
			continue
		}
		if tz.StdOffsetSeconds != c.std || tz.DstOffsetSeconds != c.dst {
			t.Errorf(
				"%q: offsets %d/%d, want %d/%d",
				c.spec,
				tz.StdOffsetSeconds,
				tz.DstOffsetSeconds,
				c.std,
				c.dst,
			)
		}
		for _, r := range []struct {
			name      string
			got, want *api.DSTRule
		}{{"start", tz.DstStart, c.start}, {"end", tz.DstEnd, c.end}} {
			if (r.got == nil) != (r.want == nil) {
				t.Errorf("%q: %s rule %v, want %v", c.spec, r.name, r.got, r.want)
				continue
			}
			if r.got != nil && (r.got.Type != r.want.Type || r.got.Month != r.want.Month ||
				r.got.Week != r.want.Week || r.got.DayOfWeek != r.want.DayOfWeek ||
				r.got.Day != r.want.Day || r.got.TimeSeconds != r.want.TimeSeconds) {
				t.Errorf("%q: %s rule %v, want %v", c.spec, r.name, r.got, r.want)
			}
		}
	}
}
