package wake

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/hook"
)

type Provider interface {
	Models() []Model
	Ensure(ctx context.Context, id string, progress func(float32)) error
}

type Preparation struct {
	Phrase string
	At     float32
}

func (p Preparation) Active() bool { return p.Phrase != "" }

type Library struct {
	dir        string
	providers  []Provider
	operations sync.Mutex

	muOurs sync.Mutex
	ours   []Model

	muOffers sync.Mutex
	offers   map[string]esphome.ExternalWakeWord

	muPreparing sync.Mutex
	preparing   Preparation

	Preparing hook.Hook[Preparation]
}

const (
	adoptTimeout   = 20 * time.Second
	provideTimeout = 5 * time.Minute
)

func NewLibrary(dir string, providers ...Provider) *Library {
	l := &Library{dir: dir, providers: providers, offers: map[string]esphome.ExternalWakeWord{}}
	l.Reload()
	return l
}

func (l *Library) Dir() string { return l.dir }

func (l *Library) Ours() []Model {
	l.muOurs.Lock()
	defer l.muOurs.Unlock()
	return cloneModels(l.ours)
}

func (l *Library) Reload() {
	models, err := Installed(l.dir)
	if err != nil {
		slog.Error("listing wake words failed", "dir", l.dir, "err", err)
		return
	}
	for _, p := range l.providers {
		models = append(models, p.Models()...)
	}

	l.muOurs.Lock()
	l.ours = models
	l.muOurs.Unlock()
}

func (l *Library) Offered(offers []esphome.ExternalWakeWord) {
	next := make(map[string]esphome.ExternalWakeWord, len(offers))
	for _, o := range offers {
		o.TrainedLanguages = slices.Clone(o.TrainedLanguages)
		next[o.ID] = o
	}

	l.muOffers.Lock()
	defer l.muOffers.Unlock()
	l.offers = next
}

// Home Assistant keys its wake word selects by phrase and selects by id.
func (l *Library) Advertise() ([]esphome.WakeWord, int) {
	ours := l.Ours()

	words := make([]esphome.WakeWord, 0, len(ours))
	taken := make(map[string]bool, len(ours))
	for _, m := range ours {
		words = append(
			words,
			esphome.WakeWord{ID: m.ID, Phrase: m.Phrase, TrainedLanguages: m.Languages},
		)
		taken[m.Phrase] = true
	}

	var shadowed int
	for _, o := range l.sorted() {
		if taken[o.Phrase] {
			shadowed++
			continue
		}
		taken[o.Phrase] = true
		words = append(
			words,
			esphome.WakeWord{ID: o.ID, Phrase: o.Phrase, TrainedLanguages: o.TrainedLanguages},
		)
	}
	return words, shadowed
}

func (l *Library) Phrases() []string {
	words, _ := l.Advertise()
	out := make([]string, 0, len(words))
	for _, w := range words {
		out = append(out, w.Phrase)
	}
	slices.Sort(out)
	return out
}

// Home Assistant asks on connect and after every selection change, with the models in its custom_wake_words directory.
func (l *Library) Answer(offered []esphome.ExternalWakeWord) []esphome.WakeWord {
	l.Offered(offered)
	words, shadowed := l.Advertise()

	slog.Debug("wake words offered", "count", len(offered),
		"ours", len(l.Ours()), "advertised", len(words), "shadowed", shadowed)
	return words
}

func (l *Library) sorted() []esphome.ExternalWakeWord {
	l.muOffers.Lock()
	defer l.muOffers.Unlock()

	out := make([]esphome.ExternalWakeWord, 0, len(l.offers))
	for _, o := range l.offers {
		o.TrainedLanguages = slices.Clone(o.TrainedLanguages)
		out = append(out, o)
	}
	slices.SortFunc(
		out,
		func(a, b esphome.ExternalWakeWord) int { return strings.Compare(a.ID, b.ID) },
	)
	return out
}

func (l *Library) Purge(inUse []string) (int, int64) {
	l.operations.Lock()
	defer l.operations.Unlock()
	gone, freed := Purge(l.dir, inUse)
	if gone > 0 {
		l.Reload()
	}
	return gone, freed
}

func (l *Library) Ensure(ids []string) []Model {
	l.operations.Lock()
	defer l.operations.Unlock()
	var fetched bool
	for _, id := range ids {
		if id == "" {
			continue
		}
		if p := l.provider(id); p != nil {
			l.provide(p, id)
			fetched = true
			continue
		}
		o := l.offer(id)
		if o.URL == "" || Have(l.dir, o) {
			continue
		}
		l.fetch(o)
		fetched = true
	}

	if fetched {
		l.Reload()
	}
	return l.Ours()
}

func (l *Library) Preparation() Preparation {
	l.muPreparing.Lock()
	defer l.muPreparing.Unlock()
	return l.preparing
}

func (l *Library) provide(p Provider, id string) {
	m, _ := Find(p.Models(), id)
	ctx, cancel := context.WithTimeout(context.Background(), provideTimeout)
	defer cancel()
	defer l.prepare(Preparation{})
	l.prepare(Preparation{Phrase: m.Phrase})
	shown := 0
	if err := p.Ensure(ctx, id, func(at float32) {
		if percent := int(at * 100); percent != shown {
			shown = percent
			l.prepare(Preparation{Phrase: m.Phrase, At: at})
		}
	}); err != nil {
		slog.Error("preparing a wake word failed", "id", id, "err", err)
	}
}

func (l *Library) prepare(p Preparation) {
	l.muPreparing.Lock()
	l.preparing = p
	l.muPreparing.Unlock()
	l.Preparing.Emit(p)
}

func (l *Library) provider(id string) Provider {
	for _, p := range l.providers {
		if _, ok := Find(p.Models(), id); ok {
			return p
		}
	}
	return nil
}

func (l *Library) offer(id string) esphome.ExternalWakeWord {
	l.muOffers.Lock()
	defer l.muOffers.Unlock()
	return l.offers[id]
}

func (l *Library) fetch(o esphome.ExternalWakeWord) {
	ctx, cancel := context.WithTimeout(context.Background(), adoptTimeout)
	defer cancel()

	m, err := Adopt(ctx, l.dir, o)
	if err != nil {
		slog.Error("adopting a wake word failed", "id", o.ID, "err", err)
		return
	}
	slog.Info("wake word adopted", "id", m.ID, "phrase", m.Phrase, "engine", m.Kind)
}

func cloneModels(models []Model) []Model {
	out := slices.Clone(models)
	for i := range out {
		out[i].Languages = slices.Clone(out[i].Languages)
	}
	return out
}
