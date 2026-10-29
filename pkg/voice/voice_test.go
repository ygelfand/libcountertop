package voice

import (
	"errors"
	"regexp"
	"slices"
	"testing"
)

type engine struct {
	pushed [][]string
	fail   error
}

func (e *engine) Keywords(words []string) error {
	if e.fail != nil {
		return e.fail
	}
	e.pushed = append(e.pushed, slices.Clone(words))
	return nil
}

func TestNormalize(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Set a timer for ten minutes.", "set a timer for 10 minutes"},
		{"set a timer for forty five seconds", "set a timer for 45 seconds"},
		{"Set a timer for forty-five seconds.", "set a timer for 45 seconds"},
		{"Set a timer for 45 seconds.", "set a timer for 45 seconds"},
		{"twenty three minutes", "23 minutes"},
		{"one hundred and twenty", "120"},
		{"a hundred", "100"},
		{"a timer", "a timer"},
		{"one two three", "1 2 3"},
		{"What's the weather?", "what's the weather"},
		{"  Open   Radar  ", "open radar"},
		{"", ""},
	} {
		if got := Normalize(tc.in); got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestKeywordHitSendsTheWholeSegment(t *testing.T) {
	c := NewCommands(&engine{}, nil)
	var got []Heard
	c.Listen(Hook{Keywords: []string{"radar"}, Heard: func(h Heard) { got = append(got, h) }})

	c.Hear("Open Radar.")
	c.Hear("what's the weather like")

	if len(got) != 1 || got[0].Text != "open radar" {
		t.Fatalf("heard %+v, want one segment %q", got, "open radar")
	}
}

func TestKeywordIsWholeWords(t *testing.T) {
	c := NewCommands(&engine{}, nil)
	called := 0
	c.Listen(Hook{Keywords: []string{"radio"}, Heard: func(Heard) { called++ }})

	c.Hear("the radiologist called")

	if called != 0 {
		t.Errorf("a keyword inside another word called the hook %d times", called)
	}
}

func TestMatchFiltersAndReturnsGroups(t *testing.T) {
	c := NewCommands(&engine{}, nil)
	var got []Heard
	c.Listen(Hook{
		Keywords: []string{"timer"},
		Match: regexp.MustCompile(
			`^set a timer for (?P<n>\d+) (?P<unit>seconds?|minutes?|hours?)$`,
		),
		Heard: func(h Heard) { got = append(got, h) },
	})

	c.Hear("Cancel the timer.")
	c.Hear("Set a timer for twenty three minutes.")

	if len(got) != 1 {
		t.Fatalf("heard %d segments, want 1: %+v", len(got), got)
	}
	if got[0].Groups["n"] != "23" || got[0].Groups["unit"] != "minutes" {
		t.Errorf("groups %v, want n=23 unit=minutes", got[0].Groups)
	}
}

func TestEveryHookOnAKeywordHears(t *testing.T) {
	c := NewCommands(&engine{}, nil)
	var a, b int
	c.Listen(Hook{Keywords: []string{"stop"}, Heard: func(Heard) { a++ }})
	c.Listen(Hook{Keywords: []string{"stop"}, Heard: func(Heard) { b++ }})

	c.Hear("stop")

	if a != 1 || b != 1 {
		t.Errorf("hooks heard %d and %d, want 1 each", a, b)
	}
}

func TestKeywordsFollowLiveHooks(t *testing.T) {
	e := &engine{}
	c := NewCommands(e, nil)

	cancelRadar := c.Listen(Hook{Keywords: []string{"radar"}, Heard: func(Heard) {}})
	cancelStop := c.Listen(Hook{Keywords: []string{"stop", "radar"}, Heard: func(Heard) {}})
	cancelRadar()
	cancelRadar()
	cancelStop()

	want := [][]string{{"radar"}, {"radar", "stop"}, nil}
	if len(e.pushed) != len(want) {
		t.Fatalf("pushed %v, want %v", e.pushed, want)
	}
	for i := range want {
		if !slices.Equal(e.pushed[i], want[i]) {
			t.Errorf("push %d = %v, want %v", i, e.pushed[i], want[i])
		}
	}
}

func TestPhraseGroupsJoinTheKeywords(t *testing.T) {
	e := &engine{}
	c := NewCommands(e, nil)
	c.Listen(Hook{Keywords: []string{"radar"}, Heard: func(Heard) {}})
	c.Phrases("home", []string{"Kitchen", " Office ", "radar"})
	c.Phrases("peers", []string{"athena"})
	c.Phrases("home", nil)

	want := [][]string{
		{"radar"},
		{"Kitchen", "Office", "radar"},
		{"Kitchen", "Office", "athena", "radar"},
		{"athena", "radar"},
	}
	if len(e.pushed) != len(want) {
		t.Fatalf("pushed %v, want %v", e.pushed, want)
	}
	for i := range want {
		if !slices.Equal(e.pushed[i], want[i]) {
			t.Errorf("push %d = %v, want %v", i, e.pushed[i], want[i])
		}
	}
}

func TestCanceledHookHearsNothing(t *testing.T) {
	c := NewCommands(&engine{}, nil)
	called := 0
	cancel := c.Listen(Hook{Keywords: []string{"radar"}, Heard: func(Heard) { called++ }})
	cancel()

	c.Hear("radar")

	if called != 0 {
		t.Errorf("a canceled hook was called %d times", called)
	}
}

func TestFailedPushIsRetriedOnTheNextChange(t *testing.T) {
	e := &engine{fail: errors.New("not running")}
	c := NewCommands(e, nil)
	c.Listen(Hook{Keywords: []string{"radar"}, Heard: func(Heard) {}})

	e.fail = nil
	c.Listen(Hook{Keywords: []string{"radio"}, Heard: func(Heard) {}})

	if len(e.pushed) != 1 || !slices.Equal(e.pushed[0], []string{"radar", "radio"}) {
		t.Errorf("pushed %v, want one push of [radar radio]", e.pushed)
	}
}

func TestHookMayListenFromItsHandler(t *testing.T) {
	c := NewCommands(&engine{}, nil)
	c.Listen(Hook{Keywords: []string{"radar"}, Heard: func(Heard) {
		c.Listen(Hook{Keywords: []string{"back"}, Heard: func(Heard) {}})
	}})

	c.Hear("radar")

	if got := c.Keywords(); !slices.Equal(got, []string{"back", "radar"}) {
		t.Errorf("keywords %v, want [back radar]", got)
	}
}
