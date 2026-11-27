package actions

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/say"
)

func TestTheDeclarationCarriesEveryActionAndItsTriggers(t *testing.T) {
	var r Registry
	r.Add(Action{
		Name:  "call",
		Title: "Call a device",
		Args:  []esphome.Arg{{Name: "peer", Type: esphome.ArgString}},
		Triggers: []Trigger{
			{Key: "actions.call.sentence.call"},
			{Key: "actions.call.sentence.video", Args: map[string]string{"video": "true"}},
		},
		Run: func(esphome.Call) (string, error) { return "", nil },
	})
	r.Add(Action{Name: "ping", Run: func(esphome.Call) (string, error) { return "", nil }})

	var got declared
	switched := func(name string) string { return "Switch " + name }
	if err := json.Unmarshal(
		[]byte(r.Declaration(all, switched, nil)["actions"]),
		&got,
	); err != nil {
		t.Fatal(err)
	}
	if got.Version != Version || len(got.Actions) != 2 {
		t.Fatalf("declaration = %+v", got)
	}
	call := got.Actions[0]
	if call.Name != "call" || len(call.Args) != 1 ||
		call.Args[0] != (declaredArg{Name: "peer", Type: "string"}) ||
		len(call.Triggers) != 2 || call.Switch != "Switch call" {
		t.Errorf("call declared as %+v", call)
	}
	if call.Triggers[1].Args["video"] != "true" {
		t.Errorf("video trigger lost its args: %+v", call.Triggers[1])
	}
	if s := call.Triggers[1].Sentence; s != "video call [the] {peer}" {
		t.Errorf("video trigger declared %q, want the English sentence", s)
	}
	if len(got.Actions[1].Triggers) != 0 {
		t.Errorf("an action without triggers declared %+v", got.Actions[1].Triggers)
	}
}

func TestASpeechArgIsDeclaredAsSpeech(t *testing.T) {
	var r Registry
	r.Add(Action{
		Name:   "broadcast",
		Args:   []esphome.Arg{{Name: "message", Type: esphome.ArgString}},
		Speech: "message",
	})
	var got declared
	if err := json.Unmarshal([]byte(r.Declaration(all, nil, nil)["actions"]), &got); err != nil {
		t.Fatal(err)
	}
	if arg := got.Actions[0].Args[0]; arg.Type != speechType {
		t.Errorf("message declared as %+v, want speech", arg)
	}
}

func all(string) bool { return true }

var slot = regexp.MustCompile(`\{[a-z_]+\}`)

func slots(sentence string) []string {
	found := slot.FindAllString(sentence, -1)
	slices.Sort(found)
	return slices.Compact(found)
}

func TestEveryLanguageSentenceHasTheEnglishSlots(t *testing.T) {
	ids, err := say.Ids()
	if err != nil {
		t.Fatal(err)
	}
	defer say.UseVoice("")
	english := map[string][]string{}
	say.UseVoice("en")
	for _, id := range ids {
		if strings.Contains(id, ".sentence.") {
			english[id] = slots(say.Speak(id))
		}
	}
	if len(english) == 0 {
		t.Fatal("no voice sentences in the catalog")
	}
	for _, tag := range say.Languages() {
		say.UseVoice(tag)
		for id, want := range english {
			if got := slots(say.Speak(id)); !slices.Equal(got, want) {
				t.Errorf("%s %s uses %v, want %v as in English", tag, id, got, want)
			}
		}
	}
}

func TestASilencedActionDeclaresNoTriggers(t *testing.T) {
	var r Registry
	r.Add(Action{Name: "call", Triggers: []Trigger{{Key: "actions.call.sentence.call"}}})
	r.Add(Action{Name: "broadcast", Triggers: []Trigger{{Key: "actions.broadcast.sentence.ask"}}})
	var got declared
	raw := r.Declaration(func(name string) bool { return name != "call" }, nil, nil)["actions"]
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Actions) != 2 {
		t.Fatalf("a silenced action left the declaration: %+v", got.Actions)
	}
	if len(got.Actions[0].Triggers) != 0 || len(got.Actions[1].Triggers) != 1 {
		t.Errorf("triggers declared %+v, want none for call and one for broadcast", got.Actions)
	}
}

func TestAnActionIsDeclaredOnlyToAnIntegrationThatSpeaksItsProtocol(t *testing.T) {
	var r Registry
	r.Add(Action{Name: "alarm"})
	r.Add(Action{Name: "personal", Protocol: 7})
	names := func(speaks func(int) bool) []string {
		var got declared
		if err := json.Unmarshal(
			[]byte(r.Declaration(all, nil, speaks)["actions"]),
			&got,
		); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, a := range got.Actions {
			out = append(out, a.Name)
		}
		return out
	}
	if got := names(func(p int) bool { return p <= 6 }); !slices.Equal(got, []string{"alarm"}) {
		t.Errorf("to an older integration: %v", got)
	}
	if got := names(
		func(p int) bool { return p <= 7 },
	); !slices.Equal(
		got,
		[]string{"alarm", "personal"},
	) {
		t.Errorf("to a newer integration: %v", got)
	}
	if got := names(nil); !slices.Equal(got, []string{"alarm"}) {
		t.Errorf("with no integration: %v", got)
	}
}

func TestAddingAnActionAgainReplacesIt(t *testing.T) {
	var r Registry
	r.Add(Action{Name: "call", Title: "old"})
	r.Add(Action{Name: "call", Title: "new"})
	if list := r.all(); len(list) != 1 || list[0].Title != "new" {
		t.Errorf("registry = %+v", list)
	}
}

func TestAnActionAnswersWithItsReply(t *testing.T) {
	var r Registry
	r.Add(Action{Name: "ok", Run: func(esphome.Call) (string, error) { return "done", nil }})
	r.Add(
		Action{
			Name: "bad",
			Run:  func(esphome.Call) (string, error) { return "", errors.New("broken") },
		},
	)

	acts := r.Actions()
	if len(acts) != 2 || !acts[0].Answers {
		t.Fatalf("actions = %+v", acts)
	}
	got, err := acts[0].Run(esphome.Call{})
	if err != nil || got.(map[string]any)["reply"] != "done" {
		t.Errorf("ok answered %v, %v", got, err)
	}
	if _, err := acts[1].Run(esphome.Call{}); err == nil {
		t.Error("a failing action answered without an error")
	}
}
