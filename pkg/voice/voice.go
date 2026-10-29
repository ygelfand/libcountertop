package voice

import (
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
)

type Hook struct {
	Name     string
	Keywords []string
	Match    *regexp.Regexp
	Heard    func(Heard)
}

type Heard struct {
	Text   string
	Groups map[string]string
}

type Engine interface {
	Keywords(words []string) error
}

type Commands struct {
	engine Engine
	allow  func(name string) bool

	mu      sync.Mutex
	next    int
	hooks   map[int]listener
	phrases map[string][]string

	push sync.Mutex
	sent []string
}

type listener struct {
	hook  Hook
	terms []string
}

func NewCommands(engine Engine, allow func(name string) bool) *Commands {
	if allow == nil {
		allow = func(string) bool { return true }
	}
	return &Commands{
		engine:  engine,
		allow:   allow,
		hooks:   map[int]listener{},
		phrases: map[string][]string{},
	}
}

func (c *Commands) Phrases(group string, phrases []string) {
	c.mu.Lock()
	if len(phrases) == 0 {
		delete(c.phrases, group)
	} else {
		c.phrases[group] = slices.Clone(phrases)
	}
	c.mu.Unlock()
	c.publish()
}

func (c *Commands) Live() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, l := range c.hooks {
		if c.allow(l.hook.Name) {
			return true
		}
	}
	return false
}

func (c *Commands) Refresh() { c.publish() }

func (c *Commands) Listen(h Hook) (cancel func()) {
	terms := make([]string, 0, len(h.Keywords))
	for _, k := range h.Keywords {
		if t := Normalize(k); t != "" {
			terms = append(terms, t)
		}
	}
	c.mu.Lock()
	id := c.next
	c.next++
	c.hooks[id] = listener{hook: h, terms: terms}
	c.mu.Unlock()
	c.publish()

	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			delete(c.hooks, id)
			c.mu.Unlock()
			c.publish()
		})
	}
}

func (c *Commands) Hear(text string) {
	said := Normalize(text)
	if said == "" {
		return
	}
	padded := " " + said + " "

	c.mu.Lock()
	ids := make([]int, 0, len(c.hooks))
	for id := range c.hooks {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var due []Hook
	for _, id := range ids {
		l := c.hooks[id]
		if c.allow(l.hook.Name) && slices.ContainsFunc(
			l.terms,
			func(t string) bool { return strings.Contains(padded, " "+t+" ") },
		) {
			due = append(due, l.hook)
		}
	}
	c.mu.Unlock()

	for _, h := range due {
		heard := Heard{Text: said}
		if h.Match != nil {
			m := h.Match.FindStringSubmatch(said)
			if m == nil {
				continue
			}
			heard.Groups = groups(h.Match, m)
		}
		h.Heard(heard)
	}
}

func (c *Commands) Keywords() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var words []string
	for _, l := range c.hooks {
		if !c.allow(l.hook.Name) {
			continue
		}
		for _, k := range l.hook.Keywords {
			if k = strings.TrimSpace(k); k != "" {
				words = append(words, k)
			}
		}
	}
	slices.Sort(words)
	return slices.Compact(words)
}

func (c *Commands) publish() {
	c.push.Lock()
	defer c.push.Unlock()
	words := c.Keywords()
	c.mu.Lock()
	for _, group := range c.phrases {
		for _, p := range group {
			if p = strings.TrimSpace(p); p != "" {
				words = append(words, p)
			}
		}
	}
	c.mu.Unlock()
	slices.Sort(words)
	words = slices.Compact(words)
	if slices.Equal(words, c.sent) {
		return
	}
	if err := c.engine.Keywords(words); err != nil {
		slog.Warn("voice keywords not applied", "err", err)
		return
	}
	c.sent = words
}

func groups(re *regexp.Regexp, m []string) map[string]string {
	var g map[string]string
	for i, name := range re.SubexpNames() {
		if name == "" || i >= len(m) {
			continue
		}
		if g == nil {
			g = map[string]string{}
		}
		g[name] = m[i]
	}
	return g
}
