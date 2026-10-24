package webreceiver

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/ygelfand/libcountertop/pkg/media/cast"
)

//go:embed hooks.js
var hooksJS string

func hooks(env cast.Env, port int) string {
	screen := env.Video != nil
	values, _ := json.Marshal(map[string]any{
		"port-for-web-server": port,
		"device-capabilities": map[string]any{
			"display_supported":     screen,
			"touch_input_supported": screen,
			"is_hdr_supported":      false,
			"is_dv_supported":       false,
			"is_device_registered":  false,
			"bluetooth_supported":   false,
			"is_group":              false,
			"focus_state_supported": false,
		},
		"device-info": map[string]any{
			"os-name":     "Linux",
			"os-version":  "1.56",
			"model-name":  env.Model,
			"device-name": env.Name,
		},
		"device-environment":      "home",
		"receiver-device-id":      env.Device.ID,
		"receiver-flags":          map[string]any{},
		"log-level-cast-receiver": 0,
	})
	return strings.NewReplacer(
		"__VALUES__", string(values),
		"__PORT__", strconv.Itoa(port),
	).Replace(hooksJS)
}
