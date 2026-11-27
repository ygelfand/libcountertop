package yttv

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/actions"
	"github.com/ygelfand/libcountertop/pkg/match"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/voice"
)

const ActionName = "youtube_tv"

func (g *Guide) VoiceAction() actions.Action {
	trigger := func(key, operation string) actions.Trigger {
		return actions.Trigger{
			Key:  "actions.youtube_tv.sentence." + key,
			Args: map[string]string{"operation": operation},
		}
	}
	return actions.Action{
		Name:  ActionName,
		Title: say.T("actions.youtube_tv"),
		Args: []esphome.Arg{
			{
				Name:        "operation",
				Type:        esphome.ArgString,
				Description: "watch.",
				Example:     "watch",
			},
			{Name: "channel", Type: esphome.ArgString, Example: "ESPN"},
			{
				Name:        "device",
				Type:        esphome.ArgString,
				Description: "For watch; empty plays here, or on the default device.",
			},
		},
		Triggers: []actions.Trigger{
			trigger("watch_on", "watch"),
			trigger("watch", "watch"),
		},
		Run: g.voice,
	}
}

func (g *Guide) voice(c esphome.Call) (string, error) {
	said := strings.TrimSpace(c.String("channel"))
	if said == "" {
		return say.Speak("actions.youtube_tv.channel"), nil
	}
	channels, tvs, err, _ := g.Load()
	if len(channels) == 0 {
		slog.Warn("youtube tv voice: no channel guide", "err", err)
		return say.Speak("tv.failed"), nil
	}

	now := time.Now()
	named := strings.TrimSpace(c.String("device"))
	if named == "" {
		said, named = splitDevice(said, tvs)
	}
	channel, ok := findChannel(channels, said, now)
	if !ok {
		return say.SpeakF("actions.youtube_tv.unknown", map[string]any{"Channel": said}), nil
	}

	return g.watch(channel, tvs, named, now), nil
}

func (g *Guide) watch(channel Channel, tvs []TV, named string, now time.Time) string {
	tv, reply := g.target(tvs, named)
	if reply != "" {
		return reply
	}
	safe.Go("youtube tv cast", func() {
		ctx, cancel := context.WithTimeout(context.Background(), askTime)
		defer cancel()
		if err := g.Cast(ctx, tv.Entity, channel); err != nil {
			slog.Warn(
				"youtube tv cast failed",
				"device", tv.Entity,
				"channel", channel.Name,
				"err", err,
			)
		}
	})
	key, data := "actions.youtube_tv.watching", map[string]any{
		"Channel": channel.Name,
		"Device":  label(tv),
	}
	if g.Mine(tv) {
		key = "actions.youtube_tv.here"
	}
	if a, ok := channel.At(now); ok && a.Title != "" {
		data["Show"] = a.Title
		return say.SpeakF(key, data)
	}
	return say.SpeakF(key+".channel", data)
}

func (g *Guide) target(tvs []TV, named string) (TV, string) {
	if named != "" {
		if tv, ok := findTV(tvs, named); ok {
			return tv, ""
		}
		return TV{}, say.SpeakF("actions.youtube_tv.device", map[string]any{"Device": named})
	}
	for _, tv := range tvs {
		if g.Mine(tv) {
			return tv, ""
		}
	}
	if entity := g.Default(); entity != "" {
		for _, tv := range tvs {
			if tv.Entity == entity {
				return tv, ""
			}
		}
		return TV{Entity: entity}, ""
	}
	return TV{}, say.Speak("actions.youtube_tv.nowhere")
}

func label(tv TV) string {
	if tv.Name != "" {
		return tv.Name
	}
	return tv.Entity
}

func findChannel(channels []Channel, said string, now time.Time) (Channel, bool) {
	var candidates []match.Candidate
	for _, c := range channels {
		names := []string{c.Name, c.Station}
		if a, ok := c.At(now); ok {
			names = append(names, a.Title)
		}
		for _, name := range names {
			if name != "" {
				candidates = append(candidates, match.Candidate{Key: c.Key(), Name: name})
			}
		}
	}
	key, ok := match.Closest(said, candidates)
	if !ok {
		slog.Info("youtube tv voice: no channel or show matched", "said", said)
		return Channel{}, false
	}
	i := slices.IndexFunc(channels, func(c Channel) bool { return c.Key() == key })
	return channels[i], true
}

func findTV(tvs []TV, said string) (TV, bool) {
	candidates := make([]match.Candidate, 0, len(tvs))
	for _, tv := range tvs {
		candidates = append(candidates, match.Candidate{Key: tv.Entity, Name: label(tv)})
	}
	entity, ok := match.Best(said, candidates)
	if !ok {
		return TV{}, false
	}
	i := slices.IndexFunc(tvs, func(tv TV) bool { return tv.Entity == entity })
	return tvs[i], true
}

func splitDevice(said string, tvs []TV) (channel, device string) {
	words := strings.Fields(said)
	connectors := voice.Current().Connectors()
	for i := len(words) - 2; i > 0; i-- {
		if !slices.Contains(connectors, strings.ToLower(words[i])) {
			continue
		}
		rest := strings.Join(words[i+1:], " ")
		if _, ok := findTV(tvs, rest); ok {
			return strings.Join(words[:i], " "), rest
		}
	}
	return said, ""
}
