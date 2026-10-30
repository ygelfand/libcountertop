package settings

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/assistant/turn"
	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	ha "github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/hook"
	"github.com/ygelfand/libcountertop/pkg/inference/wake"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type WakeChooser interface {
	Allowed() bool
	Choose(ctx context.Context, slot int, phrase string) error
}

type PipelineChooser interface {
	Allowed() bool
	Read(ctx context.Context, slot int) (chosen string, options []string, err error)
	Choose(ctx context.Context, slot int, option string) error
}

type Switch interface {
	On() bool
	Set(on bool) error
}

type GatedSwitch interface {
	Switch
	Available() bool
}

type AssistantDependencies struct {
	Slots       int
	Pryon       Switch
	PryonStop   GatedSwitch
	Gemini      Switch
	Preferences Preferences
	Models      func() []wake.Model
	Phrases     func() []string
	Chooser     WakeChooser
	Pipelines   PipelineChooser
	Requested   *hook.Hook[int]
	Words       turn.Words
	Look        *LookPages
	Shell       *shell.Shell
}

type pipeline struct {
	chosen  string
	options []string
}

type AssistantPages struct {
	options AssistantDependencies

	mu        sync.Mutex
	pipelines map[int]pipeline
}

func NewAssistantPages(
	o AssistantDependencies,
) *AssistantPages {
	return &AssistantPages{options: o}
}
func (p *AssistantPages) open(makePage func() shell.View) func(int) {
	return func(int) { p.options.Shell.Push(makePage()) }
}

// Below 0.5 a model fires on anything; at 1 nothing reaches it.
const (
	LeastSure = 50
	MostSure  = 99
)

const chooseFor = 10 * time.Second

func (p *AssistantPages) Page() *shell.Page {
	return &shell.Page{
		Title: say.T("assistants.title"),
		Build: func() ([]menu.Row, []func(int)) {
			var (
				rows []menu.Row
				acts []func(int)
			)
			for slot := range p.options.Slots {
				rows = append(rows, menu.Row{
					Label: say.F("assistants.slot", map[string]any{"N": slot + 1}),
					Open:  func() shell.View { return p.Slot(slot) },
					Kind:  menu.Chevron,
					Value: p.Listening(slot),

					Hint:   p.Hearing(slot),
					Chosen: false,
				})
				acts = append(acts, nil)
			}
			if pryon := p.options.Pryon; pryon != nil {
				on := pryon.On()
				rows = append(rows, menu.Row{
					Label: say.T("assistants.setting.pryon"),
					Kind:  menu.Toggle,
					On:    on,
				})
				acts = append(acts, func(int) {
					if err := pryon.Set(!on); err != nil {
						slog.Error("switching pryon failed", "err", err)
					}
				})
			}
			if stop := p.options.PryonStop; stop != nil {
				on, available := stop.On(), stop.Available()
				rows = append(rows, menu.Row{
					Label: say.T("assistants.setting.pryon_stop"),
					Kind:  menu.Toggle,
					On:    on,
					Dim:   !available,
				})
				acts = append(acts, func(int) {
					if !available {
						return
					}
					if err := stop.Set(!on); err != nil {
						slog.Error("switching the pryon stop word failed", "err", err)
					}
				})
			}
			if gemini := p.options.Gemini; gemini != nil {
				on := gemini.On()
				rows = append(rows, menu.Row{
					Label: say.T("assistants.setting.gemini"),
					Kind:  menu.Toggle,
					On:    on,
				})
				acts = append(acts, func(int) {
					if err := gemini.Set(!on); err != nil {
						slog.Error("switching gemini failed", "err", err)
					}
				})
			}
			return rows, acts
		},
	}
}

func (p *AssistantPages) Listening(slot int) string {
	if id := p.options.Preferences.Wake.Read().Slot(slot).ID; id != "" {
		return wake.Pick(p.options.Models(), id).Phrase
	}
	return say.T("assistants.off")
}

func (p *AssistantPages) Hearing(slot int) string {
	id := p.options.Preferences.Wake.Read().Slot(slot).ID
	if id == "" {
		if p.picks() {
			return ""
		}
		return say.T("assistants.unset")
	}
	if _, ok := wake.Find(p.options.Models(), id); !ok {
		return say.T("assistants.waiting")
	}
	return ""
}

func (p *AssistantPages) Slot(slot int) *shell.Page {
	return &shell.Page{
		OnOpen: func() { p.readPipeline(slot) },
		Title:  say.F("assistants.slot", map[string]any{"N": slot + 1}),
		Build: func() ([]menu.Row, []func(int)) {
			saved := p.options.Preferences.Wake.Read().Slot(slot)

			word, choose := p.wordRow(slot)
			assistant, pick := p.pipelineRow(slot)
			return []menu.Row{
				{Label: say.T("assistants.talk"), Hint: say.T("assistants.talk.hint")},

				word,
				assistant,

				{
					Label: say.T("assistants.threshold"),
					Kind:  menu.Slider,
					Level: Sensitivity(saved.Threshold),
					Hint:  say.T("assistants.threshold.hint"),
				},

				{
					Label: say.T("assistants.tone"),
					Kind:  menu.Chevron,
					Value: saved.Tone.Label(),
				},
				{
					Label: say.T("assistants.reply"),
					Kind:  menu.Chevron,
					Value: saved.Delivery.Label(),
				},
				{
					Label: say.T("look.title"),
					Open:  func() shell.View { return p.options.Look.Page(slot) },
					Kind:  menu.Chevron,
					Value: saved.Look.Place.Label(),
				},
			}, []func(int){
				func(int) { p.options.Requested.Emit(slot) },
				choose,
				pick,
				p.Threshold(slot),
				p.open(func() shell.View { return p.Tone(slot) }),
				p.open(func() shell.View { return p.Delivery(slot) }),
				nil,
			}
		},
	}
}

func (p *AssistantPages) wordRow(slot int) (menu.Row, func(int)) {
	row := menu.Row{Label: say.T("assistants.word"), Kind: menu.Chevron, Value: p.Listening(slot)}
	if !p.picks() {
		row.Dim, row.Hint = true, say.T("assistants.word.ha")
		return row, func(int) {}
	}
	return row, p.open(func() shell.View { return p.Word(slot) })
}

func (p *AssistantPages) pipelineRow(slot int) (menu.Row, func(int)) {
	row := menu.Row{Label: say.T("assistants.pipeline"), Kind: menu.Chevron}
	if p.options.Pipelines == nil || !p.options.Pipelines.Allowed() {
		row.Dim, row.Hint = true, say.T("assistants.word.ha")
		return row, func(int) {}
	}
	now, ok := p.pipeline(slot)
	if !ok {
		row.Dim = true
		return row, func(int) {}
	}
	row.Value = pipelineName(now.chosen)
	return row, p.open(func() shell.View { return p.Pipeline(slot) })
}

func (p *AssistantPages) Pipeline(slot int) *shell.Page {
	return &shell.Page{
		Title: say.T("assistants.pipeline"),
		Build: func() ([]menu.Row, []func(int)) {
			now, _ := p.pipeline(slot)
			rows := make([]menu.Row, 0, len(now.options))
			acts := make([]func(int), 0, len(now.options))
			for _, option := range now.options {
				rows = append(
					rows,
					menu.Row{Label: pipelineName(option), Chosen: option == now.chosen},
				)
				acts = append(acts, func(int) { p.choosePipeline(slot, option) })
			}
			return rows, acts
		},
	}
}

func pipelineName(option string) string {
	if option == ha.PreferredPipeline {
		return say.T("assistants.pipeline.preferred")
	}
	return option
}

func (p *AssistantPages) pipeline(slot int) (pipeline, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now, ok := p.pipelines[slot]
	return now, ok
}

func (p *AssistantPages) readPipeline(slot int) {
	if p.options.Pipelines == nil || !p.options.Pipelines.Allowed() {
		return
	}
	safe.Go("reading the assistant pipeline", func() {
		ctx, cancel := context.WithTimeout(context.Background(), chooseFor)
		defer cancel()
		chosen, options, err := p.options.Pipelines.Read(ctx, slot)
		if err != nil {
			slog.Warn("reading the assistant pipeline failed", "slot", slot+1, "err", err)
			return
		}
		p.mu.Lock()
		if p.pipelines == nil {
			p.pipelines = map[int]pipeline{}
		}
		p.pipelines[slot] = pipeline{chosen: chosen, options: options}
		p.mu.Unlock()
		p.options.Shell.Redraw()
	})
}

func (p *AssistantPages) choosePipeline(slot int, option string) {
	safe.Go("choosing the assistant pipeline", func() {
		ctx, cancel := context.WithTimeout(context.Background(), chooseFor)
		defer cancel()
		if err := p.options.Pipelines.Choose(ctx, slot, option); err != nil {
			slog.Error("choosing the assistant pipeline failed", "slot", slot+1, "err", err)
			return
		}
		p.mu.Lock()
		now := p.pipelines[slot]
		now.chosen = option
		p.pipelines[slot] = now
		p.mu.Unlock()
		p.options.Shell.Redraw()
	})
}

func (p *AssistantPages) picks() bool {
	return p.options.Chooser != nil && p.options.Phrases != nil && p.options.Chooser.Allowed()
}

func (p *AssistantPages) Word(slot int) *shell.Page {
	return &shell.Page{
		Title: say.T("assistants.word"),
		Build: func() ([]menu.Row, []func(int)) {
			now := p.options.Preferences.Wake.Read().Slot(slot).ID
			current := ""
			if now != "" {
				current = p.Listening(slot)
			}
			rows := []menu.Row{{Label: say.T("assistants.off"), Chosen: current == ""}}
			acts := []func(int){p.UseWord(slot, "")}
			for _, phrase := range p.options.Phrases() {
				rows = append(rows, menu.Row{Label: phrase, Chosen: phrase == current})
				acts = append(acts, p.UseWord(slot, phrase))
			}
			return rows, acts
		},
	}
}

func (p *AssistantPages) UseWord(slot int, phrase string) func(int) {
	return func(int) {
		safe.Go("choosing a wake word", func() {
			ctx, cancel := context.WithTimeout(context.Background(), chooseFor)
			defer cancel()
			if err := p.options.Chooser.Choose(ctx, slot, phrase); err != nil {
				slog.Error("choosing the wake word failed", "slot", slot+1, "err", err)
			}
		})
	}
}

func Sensitivity(v float64) int {
	return LeastSure + MostSure - min(max(int(v*100+0.5), LeastSure), MostSure)
}

func (p *AssistantPages) Threshold(slot int) func(int) {
	return func(level int) {
		v := float64(LeastSure+MostSure-min(max(level, LeastSure), MostSure)) / 100
		if err := p.options.Preferences.Word(
			slot,
			func(w *config.WakeWord) { w.Threshold = v },
		); err != nil {
			slog.Error("saving the wake threshold failed", "slot", slot+1, "err", err)
		}
	}
}

func (p *AssistantPages) Phrases() string {
	var names []string
	for slot := range p.options.Slots {
		if p.options.Preferences.Wake.Read().Slot(slot).ID != "" {
			names = append(names, p.Listening(slot))
		}
	}
	if len(names) == 0 {
		return say.T("assistants.off")
	}
	return strings.Join(names, ", ")
}

func (p *AssistantPages) Tone(slot int) *shell.Page {
	return &shell.Page{
		Title: say.T("tone.title"),
		Build: func() ([]menu.Row, []func(int)) {
			now := p.options.Preferences.Wake.Read().Slot(slot).Tone

			var (
				rows []menu.Row
				acts []func(int)
			)
			for _, c := range chimeChoices(config.Chimes(), p.options.Words.ToneChoices()) {
				rows = append(rows, menu.Row{Label: c.label, Chosen: c.chime == now})
				acts = append(acts, p.UseTone(slot, c.chime))
			}
			return rows, acts
		},
	}
}

func (p *AssistantPages) UseTone(slot int, c config.Chime) func(int) {
	return func(int) {
		if err := p.options.Preferences.Word(
			slot,
			func(w *config.WakeWord) { w.Tone = c },
		); err != nil {
			slog.Error("saving the wake tone failed", "slot", slot+1, "err", err)
			return
		}
		p.options.Words.Chime(slot)
	}
}

func (p *AssistantPages) Delivery(slot int) *shell.Page {
	return &shell.Page{
		Title: say.T("reply.title"),
		Build: func() ([]menu.Row, []func(int)) {
			now := p.options.Preferences.Wake.Read().Slot(slot).Delivery

			var (
				rows []menu.Row
				acts []func(int)
			)
			for _, d := range config.Deliveries() {
				rows = append(rows, menu.Row{
					Label: d.Label(), Chosen: d == now, Hint: Delivering(d),
				})
				acts = append(acts, p.UseDelivery(slot, d))
			}
			return rows, acts
		},
	}
}

func Delivering(d config.Delivery) string {
	if d == config.DeliveryStream {
		return say.T("reply.stream")
	}
	return say.T("reply.download")
}

func (p *AssistantPages) UseDelivery(slot int, d config.Delivery) func(int) {
	return func(int) {
		if err := p.options.Preferences.Word(
			slot,
			func(w *config.WakeWord) { w.Delivery = d },
		); err != nil {
			slog.Error("saving the reply delivery failed", "slot", slot+1, "err", err)
		}
	}
}
