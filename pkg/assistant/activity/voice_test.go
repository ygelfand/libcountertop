package activity

import (
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/actions"
	"github.com/ygelfand/libcountertop/pkg/say"
)

func call(t *testing.T, args map[string]any) esphome.Call {
	t.Helper()
	c, err := esphome.NewCall(args)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func answered(t *testing.T, log *Log, args map[string]any) map[string]any {
	t.Helper()
	var r actions.Registry
	r.Add(log.VoiceAction())
	got, err := r.Actions()[0].Run(call(t, args))
	if err != nil {
		t.Fatal(err)
	}
	answer, ok := got.(map[string]any)["answer"].(map[string]any)
	if !ok {
		t.Fatalf("answered %+v", got)
	}
	return answer
}

func TestThePersonalActionAnswersWithTheSpeakerAndItsReplies(t *testing.T) {
	defer say.UseVoice("")
	say.UseVoice("en")
	log, _ := caught()
	turn := log.Begin(1, "Hey Google")
	turn.Awaits()
	turn.Spoke("Alex", "person.alex", 0.61)

	answer := answered(t, log, map[string]any{"operation": AddToList, "item": "milk"})
	if s, _ := answer["speaker"].(Speaker); s.Match != Known || s.Person != "person.alex" {
		t.Errorf("speaker %+v", answer["speaker"])
	}
	replies, _ := answer["replies"].(map[string]string)
	want := map[string]string{
		"add_to_list.done":    "Added milk to your list.",
		"add_to_list.no_list": "Alex, there's no list set up for you.",
		"unrecognized":        "Sorry, I couldn't recognize your voice.",
	}
	for key, text := range want {
		if replies[key] != text {
			t.Errorf("%s = %q, want %q", key, replies[key], text)
		}
	}
	if _, ok := replies["calendar.none"]; ok {
		t.Error("add_to_list carried a calendar reply")
	}
}

func TestEveryOutcomeHasItsText(t *testing.T) {
	keys := append(
		[]string{
			"calendar.today",
			"calendar.tomorrow",
			"calendar.empty_today",
			"calendar.empty_tomorrow",
		},
		common...)
	for _, list := range outcomes {
		keys = append(keys, list...)
	}
	for _, key := range keys {
		if say.Missing(replyKey + key) {
			t.Errorf("no text for %s", key)
		}
	}
}

func TestAReminderWithoutATimeOffersNoConfirmation(t *testing.T) {
	defer say.UseVoice("")
	say.UseVoice("en")
	log, _ := caught()
	log.Begin(1, "Hey Google")
	replies, _ := answered(t, log, map[string]any{"operation": Remind, "task": "stretch", "minutes": "a few"})["replies"].(map[string]string)
	if text, ok := replies["remind.set"]; ok {
		t.Errorf("offered %q with no time", text)
	}
	replies, _ = answered(t, log, map[string]any{"operation": Remind, "task": "stretch", "hours": "2"})["replies"].(map[string]string)
	if replies["remind.set"] != "I'll send a reminder to your phone in 2 hours." {
		t.Errorf("remind.set = %q", replies["remind.set"])
	}
}

func TestASpokenNumberIsReadAsDigits(t *testing.T) {
	defer say.UseVoice("")
	say.UseVoice("en")
	log, _ := caught()
	log.Begin(1, "Hey Google")
	answer := answered(
		t,
		log,
		map[string]any{"operation": Remind, "task": "stretch", "minutes": "twenty five"},
	)
	if slots, _ := answer["slots"].(map[string]string); slots["minutes"] != "25" {
		t.Errorf("slots %v", answer["slots"])
	}
	replies, _ := answer["replies"].(map[string]string)
	if replies["remind.set"] != "I'll send a reminder to your phone in 25 minutes." {
		t.Errorf("remind.set = %q", replies["remind.set"])
	}
}

func TestAFollowUpPhrasesTheCalendarFromItsData(t *testing.T) {
	defer say.UseVoice("")
	say.UseVoice("en")
	log, _ := caught()
	data := `{"Count": 2, "Events": [{"Summary": "Holiday", "Time": ""}, {"Summary": "Dentist", "Time": "14:00"}]}`
	answer := answered(
		t,
		log,
		map[string]any{"operation": Reply, "key": "calendar.today", "data": data},
	)
	if answer["reply"] != "You have 2 things today: Holiday all day, Dentist at 14:00." {
		t.Errorf("reply %q", answer["reply"])
	}
}

func TestTheCalendarCountsWhatItDoesNotRead(t *testing.T) {
	defer say.UseVoice("")
	say.UseVoice("en")
	log, _ := caught()
	data := `{"Count": 7, "More": 2, "Events": [{"Summary": "Standup", "Time": "09:00"}]}`
	answer := answered(
		t,
		log,
		map[string]any{"operation": Reply, "key": "calendar.today", "data": data},
	)
	if answer["reply"] != "You have 7 things today: Standup at 09:00, and 2 more." {
		t.Errorf("reply %q", answer["reply"])
	}
}

func TestAFollowUpForAnUnknownOutcomeIsEmpty(t *testing.T) {
	log, _ := caught()
	answer := answered(t, log, map[string]any{"operation": Reply, "key": "dance"})
	if answer["reply"] != "" {
		t.Errorf("reply %q", answer["reply"])
	}
}

func TestEveryPersonalTriggerNamesAnOperation(t *testing.T) {
	log, _ := caught()
	for _, tr := range log.VoiceAction().Triggers {
		if _, ok := outcomes[tr.Args["operation"]]; !ok {
			t.Errorf("%s names %q", tr.Key, tr.Args["operation"])
		}
	}
}
