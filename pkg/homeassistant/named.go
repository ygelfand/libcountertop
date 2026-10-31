package homeassistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const namedPage = 100

const namedTemplate = `{%- set ns = namespace(out=[]) -%}
{%- for s in (states[DOMAIN] | list)[START:END] -%}
{%- set ns.out = ns.out + [[s.entity_id, s.name]] -%}
{%- endfor -%}
{{ ns.out | tojson }}`

func (h *Controller) Named(ctx context.Context, domain string) ([]Named, error) {
	var out []Named
	for start := 0; ; start += namedPage {
		raw, err := h.Template(ctx, namedPageTemplate(domain, start))
		if err != nil {
			return nil, err
		}
		page, err := ParseNamed(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(page) < namedPage {
			return out, nil
		}
	}
}

func namedPageTemplate(domain string, start int) string {
	return strings.NewReplacer(
		"DOMAIN", strconv.Quote(domain),
		"START", strconv.Itoa(start),
		"END", strconv.Itoa(start+namedPage),
	).Replace(namedTemplate)
}

func ParseNamed(raw json.RawMessage) ([]Named, error) {
	var pairs [][2]string
	if err := json.Unmarshal(unwrap(raw), &pairs); err != nil {
		return nil, fmt.Errorf("homeassistant: the entity names: %w", err)
	}
	out := make([]Named, len(pairs))
	for i, p := range pairs {
		out[i] = Named{Entity: p[0], Name: p[1]}
	}
	return out, nil
}
