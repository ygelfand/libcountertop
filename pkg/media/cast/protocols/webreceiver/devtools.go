package webreceiver

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ygelfand/libcountertop/pkg/media/cast/engine"
)

func logged(e engine.Event) {
	switch e.Method {
	case "Runtime.consoleAPICalled":
		var p struct {
			Type string `json:"type"`
			Args []struct {
				Value       any    `json:"value"`
				Description string `json:"description"`
			} `json:"args"`
		}
		if json.Unmarshal(e.Params, &p) != nil {
			return
		}
		parts := make([]string, 0, len(p.Args))
		for _, a := range p.Args {
			if a.Value != nil {
				parts = append(parts, fmt.Sprint(a.Value))
			} else {
				parts = append(parts, a.Description)
			}
		}
		slog.Debug("cast page console", "level", p.Type, "text", strings.Join(parts, " "))
	case "Runtime.exceptionThrown":
		var p struct {
			Details struct {
				Text      string `json:"text"`
				Exception struct {
					Description string `json:"description"`
				} `json:"exception"`
			} `json:"exceptionDetails"`
		}
		if json.Unmarshal(e.Params, &p) == nil {
			slog.Debug("cast page exception", "text", p.Details.Text,
				"exception", p.Details.Exception.Description)
		}
	}
}
