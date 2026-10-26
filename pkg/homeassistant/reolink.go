package homeassistant

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
)

type ReolinkDevice struct {
	Device  string            `json:"device"`
	Name    string            `json:"name"`
	Model   string            `json:"model"`
	UID     string            `json:"uid"`
	Cameras []Named           `json:"cameras"`
	Sensors map[string]string `json:"sensors"`
	Binary  []Named           `json:"binary"`
}

type Named struct {
	Entity string `json:"entity"`
	Name   string `json:"name"`
}

const reolinkDevicesTemplate = `{%- set keys = ['visitor', 'person', 'package', 'vehicle', 'pet'] -%}
{%- set ns = namespace(seen=[], out=[]) -%}
{%- for e in integration_entities('reolink') -%}
{%- set d = device_id(e) -%}
{%- if d and d not in ns.seen -%}
{%- set ns.seen = ns.seen + [d] -%}
{%- set dev = namespace(cameras=[], binary=[], sensors={}, uid='') -%}
{%- for i in device_attr(d, 'identifiers') or [] -%}
{%- if i[0] == 'reolink' -%}{%- set dev.uid = i[1] -%}{%- endif -%}
{%- endfor -%}
{%- for x in device_entities(d) -%}
{%- set label = state_attr(x, 'friendly_name') -%}
{%- if label and x.startswith('camera.') -%}
{%- set dev.cameras = dev.cameras + [{'entity': x, 'name': label}] -%}
{%- elif label and x.startswith('binary_sensor.') -%}
{%- set dev.binary = dev.binary + [{'entity': x, 'name': label}] -%}
{%- set tail = x.split('_') | last -%}
{%- if tail in keys -%}{%- set dev.sensors = dict(dev.sensors, **{tail: x}) -%}{%- endif -%}
{%- endif -%}
{%- endfor -%}
{%- if dev.cameras -%}
{%- set ns.out = ns.out + [{'device': d, 'name': device_attr(d, 'name_by_user') or device_attr(d, 'name') or '', 'model': device_attr(d, 'model') or '', 'uid': dev.uid, 'cameras': dev.cameras, 'sensors': dev.sensors, 'binary': dev.binary}] -%}
{%- endif -%}
{%- endif -%}
{%- endfor -%}
{{ ns.out | tojson }}`

func (h *Controller) ReolinkDevices(ctx context.Context) ([]ReolinkDevice, error) {
	raw, err := h.Template(ctx, reolinkDevicesTemplate)
	if err != nil {
		return nil, err
	}
	return ParseReolinkDevices(raw)
}

func ParseReolinkDevices(raw json.RawMessage) ([]ReolinkDevice, error) {
	out := []ReolinkDevice{}
	if err := json.Unmarshal(unwrap(raw), &out); err != nil {
		return nil, fmt.Errorf("homeassistant: the Reolink devices: %w", err)
	}
	slices.SortFunc(out, func(a, b ReolinkDevice) int { return cmp.Compare(a.Name, b.Name) })
	return out, nil
}
