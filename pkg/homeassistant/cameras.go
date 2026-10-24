package homeassistant

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type Camera struct {
	Entity
	Picture  string `json:"picture"`
	Features int    `json:"features"`
}

const cameraStream = 2

func (c Camera) Streams() bool { return c.Features&cameraStream != 0 }

const camerasTemplate = `{%- set ns = namespace(out=[]) -%}
{%- for s in states.camera -%}
{%- set ns.out = ns.out + [{'id': s.entity_id, 'name': s.name, 'state': s.state, 'area_id': area_id(s.entity_id) or '', 'area': area_name(s.entity_id) or '', 'labels': labels(s.entity_id), 'picture': s.attributes.get('entity_picture') or '', 'features': s.attributes.get('supported_features') or 0}] -%}
{%- endfor -%}
{{ ns.out | tojson }}`

func (h *Controller) Cameras(ctx context.Context) ([]Camera, error) {
	raw, err := h.Template(ctx, camerasTemplate)
	if err != nil {
		return nil, err
	}
	return ParseCameras(raw)
}

func ParseCameras(raw json.RawMessage) ([]Camera, error) {
	out := []Camera{}
	if err := json.Unmarshal(unwrap(raw), &out); err != nil {
		return nil, fmt.Errorf("homeassistant: the camera list: %w", err)
	}
	slices.SortStableFunc(out, func(a, b Camera) int {
		if (a.Area == "") != (b.Area == "") {
			if a.Area == "" {
				return 1
			}
			return -1
		}
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.Area), strings.ToLower(b.Area)),
			cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			cmp.Compare(a.ID, b.ID),
		)
	})
	return out, nil
}
