package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
)

const (
	homeAsk     = 10 * time.Second
	labelsFresh = time.Minute
)

type Home interface {
	Labels(ctx context.Context) ([]homeassistant.Label, error)
	Template(ctx context.Context, tmpl string) (json.RawMessage, error)
}

const namesTemplate = `{%- set ns = namespace(out=[]) -%}
{%- for a in areas() -%}{%- set ns.out = ns.out + [area_name(a)] -%}{%- endfor -%}
{%- for e in (label_entities(LABEL) if LABEL else []) -%}
{%- set ns.out = ns.out + [state_attr(e, 'friendly_name') or e] -%}
{%- endfor -%}
{{ ns.out | tojson }}`

func HomeNames(ctx context.Context, h Home, label string) ([]string, error) {
	quoted, err := json.Marshal(label)
	if err != nil {
		return nil, err
	}
	raw, err := h.Template(ctx, strings.ReplaceAll(namesTemplate, "LABEL", string(quoted)))
	if err != nil {
		return nil, err
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		raw = json.RawMessage(text)
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil, fmt.Errorf("voice: the Home Assistant names: %w", err)
	}
	out := names[:0]
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out, nil
}

func LabelChoices(h Home) func() []setting.Option {
	var (
		mu   sync.Mutex
		at   time.Time
		last []setting.Option
	)
	return func() []setting.Option {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(at) < labelsFresh {
			return last
		}
		ctx, cancel := context.WithTimeout(context.Background(), homeAsk)
		defer cancel()
		labels, err := h.Labels(ctx)
		if err != nil {
			return last
		}
		last = make([]setting.Option, 0, len(labels))
		for _, l := range labels {
			last = append(last, setting.Option{Value: l.ID, Label: l.Name})
		}
		at = time.Now()
		return last
	}
}
