package mealie

import (
	"context"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/actions"
	"github.com/ygelfand/libcountertop/pkg/match"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/voice"
)

const (
	ActionName  = "recipes"
	CookCommand = "cook"

	voiceWait = 3 * time.Second
)

func (k *Kitchen) VoiceAction() actions.Action {
	trigger := func(key, operation string) actions.Trigger {
		return actions.Trigger{
			Key:  "actions.recipes.sentence." + key,
			Args: map[string]string{"operation": operation},
		}
	}
	return actions.Action{
		Name:  ActionName,
		Title: say.T("actions.recipes"),
		Args: []esphome.Arg{
			{
				Name:        "operation",
				Type:        esphome.ArgString,
				Description: "find, show, cook, next, previous, repeat, amount or timer.",
				Example:     "cook",
			},
			{Name: "recipe", Type: esphome.ArgString, Description: "For find, show and cook."},
			{Name: "ingredient", Type: esphome.ArgString, Description: "For amount."},
		},
		Triggers: []actions.Trigger{
			trigger("find", "find"),
			trigger("find_named", "find"),
			trigger("show", "show"),
			trigger("cook", "cook"),
			trigger("next", "next"),
			trigger("next_what", "next"),
			trigger("previous", "previous"),
			trigger("repeat", "repeat"),
			trigger("amount", "amount"),
			trigger("amount_many", "amount"),
			trigger("timer", "timer"),
		},
		Run: k.voice,
	}
}

func (k *Kitchen) voice(c esphome.Call) (string, error) {
	operation := c.String("operation")
	switch operation {
	case "next":
		return k.spokenMove(1), nil
	case "previous":
		return k.spokenMove(-1), nil
	case "repeat":
		return k.spokenMove(0), nil
	case "amount":
		return k.spokenAmount(c.String("ingredient")), nil
	case "timer":
		return k.spokenTimer(), nil
	}
	if !k.Configured() {
		return say.Speak("actions.recipes.unset"), nil
	}
	said := strings.TrimSpace(c.String("recipe"))
	if said == "" {
		return say.Speak("actions.recipes.which"), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), voiceWait)
	defer cancel()
	best, found, err := k.Find(ctx, said)
	switch {
	case errors.Is(err, ErrToken):
		return say.Speak("recipes.status.token"), nil
	case err != nil:
		return say.Speak("actions.recipes.unreachable"), nil
	case len(found) == 0:
		return say.SpeakF("actions.recipes.none", map[string]any{"Query": said}), nil
	}
	switch operation {
	case "find":
		k.Asked.Emit(Show{Kind: ShowBrowse, Query: said})
		return say.SpeakF("actions.recipes.found", map[string]any{
			"Query": said, "N": len(found), "Recipe": best.Name,
		}), nil
	case "show":
		k.Asked.Emit(Show{Kind: ShowRecipe, Slug: best.Slug})
		return say.SpeakF("actions.recipes.showing", map[string]any{"Recipe": best.Name}), nil
	}
	r, err := k.fetch(ctx, best.Slug)
	if err != nil {
		return say.Speak("actions.recipes.unreachable"), nil
	}
	k.Cook(r)
	k.Asked.Emit(Show{Kind: ShowCook, Slug: r.Slug})
	if len(r.Steps) == 0 {
		return say.SpeakF("actions.recipes.cooking", map[string]any{"Recipe": r.Name}), nil
	}
	return say.SpeakF("actions.recipes.cooking", map[string]any{"Recipe": r.Name}) + " " +
		spokenStep(Session{Recipe: r}), nil
}

func (k *Kitchen) spokenMove(by int) string {
	before, ok := k.Session()
	if !ok {
		return say.Speak("actions.recipes.closed")
	}
	if len(before.Recipe.Steps) == 0 {
		return say.Speak("actions.recipes.nosteps")
	}
	s, moved := k.Move(by)
	switch {
	case by > 0 && !moved:
		return say.Speak("actions.recipes.last")
	case by < 0 && !moved:
		return say.Speak("actions.recipes.first")
	}
	return spokenStep(s)
}

func spokenStep(s Session) string {
	step := s.Recipe.Steps[s.Step]
	return say.SpeakF("actions.recipes.step", map[string]any{
		"N": s.Step + 1, "Of": len(s.Recipe.Steps), "Text": Plain(step.Text),
	})
}

func (k *Kitchen) spokenAmount(said string) string {
	s, ok := k.Session()
	if !ok {
		return say.Speak("actions.recipes.closed")
	}
	said = strings.TrimSpace(said)
	in, ok := pickIngredient(s.Recipe.Ingredients, said)
	if !ok {
		return say.SpeakF("actions.recipes.missing", map[string]any{"Ingredient": said})
	}
	return say.SpeakF("actions.recipes.amount", map[string]any{"Amount": in.Scaled(s.Scale())})
}

func pickIngredient(all []Ingredient, said string) (Ingredient, bool) {
	if said == "" {
		return Ingredient{}, false
	}
	candidates := make([]match.Candidate, 0, len(all))
	for n, in := range all {
		if name := in.Name(); name != "" {
			candidates = append(candidates, match.Candidate{Key: strconv.Itoa(n), Name: name})
		}
	}
	if key, ok := match.Closest(said, candidates); ok {
		n, _ := strconv.Atoi(key)
		return all[n], true
	}
	want := voice.Normalize(said)
	for _, in := range all {
		if strings.Contains(voice.Normalize(in.Text()), want) {
			return in, true
		}
	}
	return Ingredient{}, false
}

func (k *Kitchen) spokenTimer() string {
	if _, ok := k.Session(); !ok {
		return say.Speak("actions.recipes.closed")
	}
	d, ok := k.StepTimer()
	if !ok {
		return say.Speak("actions.recipes.notimer")
	}
	return say.SpeakF("actions.recipes.timer", map[string]any{"Minutes": Minutes(d)})
}

var (
	markdownLink  = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	markdownMarks = strings.NewReplacer("**", "", "__", "", "`", "", "#", "")
	spaces        = regexp.MustCompile(`\s+`)
)

func Plain(text string) string {
	text = markdownLink.ReplaceAllString(text, "$1")
	text = markdownMarks.Replace(text)
	return strings.TrimSpace(spaces.ReplaceAllString(text, " "))
}

func Minutes(d time.Duration) int { return int(math.Ceil(d.Minutes())) }
