package asr

import (
	"regexp"
	"slices"
	"strings"
)

type aliases struct {
	forms []string
	match *regexp.Regexp
	shown map[string]string
}

func parseAliases(lines []string) aliases {
	a := aliases{shown: map[string]string{}}
	var spoken []string
	for _, line := range lines {
		shown, rest, ok := strings.Cut(line, "=")
		shown = strings.TrimSpace(shown)
		if shown == "" {
			continue
		}
		a.forms = append(a.forms, shown)
		if !ok {
			continue
		}
		for form := range strings.SplitSeq(rest, "|") {
			form = strings.Join(strings.Fields(form), " ")
			if form == "" || strings.EqualFold(form, shown) {
				continue
			}
			a.forms = append(a.forms, form)
			a.shown[strings.ToLower(form)] = shown
			spoken = append(spoken, regexp.QuoteMeta(form))
		}
	}
	slices.Sort(a.forms)
	a.forms = slices.Compact(a.forms)
	if len(spoken) > 0 {
		slices.SortFunc(spoken, func(x, y string) int { return len(y) - len(x) })
		a.match = regexp.MustCompile(`(?i)\b(?:` + strings.Join(spoken, "|") + `)\b`)
	}
	return a
}

func (a aliases) rewrite(text string) string {
	if a.match == nil {
		return text
	}
	return a.match.ReplaceAllStringFunc(text, func(s string) string {
		return a.shown[strings.ToLower(strings.Join(strings.Fields(s), " "))]
	})
}
