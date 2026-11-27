package yttv

import (
	"strings"
	"testing"
	"testing/synctest"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

var spokenChannels = []Channel{
	{Name: "ESPN"},
	{Name: "ESPN2"},
	{Name: "ESPN Deportes"},
	{Name: "FOX"},
	{Name: "Fox News Channel", Station: "FNC"},
	{Name: "NBC", Station: "WNBC"},
}

func TestSpokenChannelNamesFindTheirChannel(t *testing.T) {
	for said, want := range map[string]string{
		"espn":          "ESPN",
		"ESPN two":      "ESPN2",
		"espn 2":        "ESPN2",
		"deportes":      "ESPN Deportes",
		"fox":           "FOX",
		"fox news":      "Fox News Channel",
		"news":          "Fox News Channel",
		"the NBC":       "NBC",
		"wnbc":          "NBC",
		"F N C":         "Fox News Channel",
		"espn deportez": "ESPN Deportes",
		"cartoon":       "",
		"":              "",
	} {
		got, ok := findChannel(spokenChannels, said, time.Now())
		if want == "" {
			if ok {
				t.Errorf("%q found %q, want nothing", said, got.Name)
			}
			continue
		}
		if !ok || got.Name != want {
			t.Errorf("%q found %q (%v), want %q", said, got.Name, ok, want)
		}
	}
}

func TestADeviceSaidInsideTheChannelIsSplitOff(t *testing.T) {
	tvs := []TV{{Entity: "media_player.yttv_den", Name: "Den"}}
	for said, want := range map[string][2]string{
		"espn on the den":  {"espn", "the den"},
		"espn on den":      {"espn", "den"},
		"dancing on ice":   {"dancing on ice", ""},
		"espn":             {"espn", ""},
		"on den":           {"on den", ""},
		"news on on den":   {"news on", "den"},
		"news on kitchen":  {"news on kitchen", ""},
		"espn on the  den": {"espn", "the den"},
	} {
		channel, device := splitDevice(said, tvs)
		if channel != want[0] || device != want[1] {
			t.Errorf("%q split into %q / %q, want %q / %q", said, channel, device, want[0], want[1])
		}
	}
}

func TestWatchingPicksThePanelItWasSaidTo(t *testing.T) {
	tvs := []TV{
		{
			Entity: "media_player.yttv_den",
			Name:   "Den",
			UUID:   "ffffffff-0000-0000-0000-000000000000",
		},
		{
			Entity: "media_player.yttv_kitchen",
			Name:   "Kitchen",
			UUID:   "01234567-89ab-cdef-0123-456789abcdef",
		},
	}
	self := func() string { return "0123456789abcdef0123456789abcdef" }
	g := New(
		Options{Self: self, Settings: &memory{tv: schema.TV{Default: "media_player.yttv_den"}}},
	)

	if tv, reply := g.target(tvs, ""); reply != "" || tv.Entity != "media_player.yttv_kitchen" {
		t.Errorf("unnamed on a panel chose %q (%q), want the panel", tv.Entity, reply)
	}
	if tv, reply := g.target(tvs, "den"); reply != "" || tv.Entity != "media_player.yttv_den" {
		t.Errorf("named den chose %q (%q)", tv.Entity, reply)
	}
	if _, reply := g.target(tvs, "garage"); reply == "" {
		t.Error("an unknown device was accepted")
	}

	elsewhere := New(Options{Settings: &memory{tv: schema.TV{Default: "media_player.yttv_den"}}})
	if tv, reply := elsewhere.target(tvs, ""); reply != "" || tv.Entity != "media_player.yttv_den" {
		t.Errorf("unnamed off a panel chose %q (%q), want the default", tv.Entity, reply)
	}
	if _, reply := New(Options{Settings: &memory{}}).target(tvs, ""); reply == "" {
		t.Error("no panel and no default still chose a device")
	}
}

func TestWatchCastsTheChannelToTheNamedDevice(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ha := &fakeHA{pages: map[string]string{
			"0": `{"version":1,"next":null,"channels":[{"name":"ESPN2","video":"abc"}],` +
				`"tvs":[{"entity_id":"media_player.yttv_den","name":"Den"}]}`,
		}}
		g := New(Options{HA: ha, Settings: &memory{}})
		g.fetch()
		call, err := esphome.NewCall(
			map[string]any{"operation": "watch", "channel": "espn two on the den"},
		)
		if err != nil {
			t.Fatal(err)
		}
		reply, err := g.VoiceAction().Run(call)
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		ha.mu.Lock()
		defer ha.mu.Unlock()
		if len(ha.performed) != 1 || ha.performed[0]["device"] != "media_player.yttv_den" ||
			ha.performed[0]["video_id"] != "abc" {
			t.Errorf("performed %v", ha.performed)
		}
		if !strings.Contains(reply, "ESPN2") || !strings.Contains(reply, "Den") {
			t.Errorf("reply %q", reply)
		}
	})
}

func TestVoiceWithoutAGuideAsksNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ha := &fakeHA{}
		g := New(Options{HA: ha, Settings: &memory{}})
		call, err := esphome.NewCall(map[string]any{"operation": "watch", "channel": "espn"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := g.VoiceAction().Run(call); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		ha.mu.Lock()
		defer ha.mu.Unlock()
		if len(ha.performed) != 0 {
			t.Errorf("cast without a guide: %v", ha.performed)
		}
	})
}

func TestShowsAiringNowFindTheirChannel(t *testing.T) {
	now := time.Date(2026, 11, 8, 20, 0, 0, 0, time.UTC)
	airing := func(title string) (string, string, string) {
		return title, now.Add(-time.Hour).
				Format(time.RFC3339),
			now.Add(time.Hour).
				Format(time.RFC3339)
	}
	channels := []Channel{{Name: "ABC"}, {Name: "CBS"}, {Name: "ESPN"}}
	channels[0].Title, channels[0].StartsAt, channels[0].EndsAt = airing("Jeopardy!")
	channels[1].Title, channels[1].StartsAt, channels[1].EndsAt = airing("The Late Show")
	for said, want := range map[string]string{
		"jeopardy":      "ABC",
		"the late show": "CBS",
		"late show":     "CBS",
		"jepardy":       "ABC",
	} {
		got, ok := findChannel(channels, said, now)
		if !ok || got.Name != want {
			t.Errorf("%q found %q (%v), want %q", said, got.Name, ok, want)
		}
	}
	if got, ok := findChannel(channels, "jeopardy", now.Add(2*time.Hour)); ok {
		t.Errorf("a show that ended still found %q", got.Name)
	}
}
