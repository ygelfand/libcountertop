package homeassistant

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/ygelfand/go-esphome-device/api"
)

func TestDashboardsReadsTheIntegrationsReply(t *testing.T) {
	reply := map[string]any{
		"version": "2026.10.2",
		"users":   []map[string]any{{"id": "u1", "name": "Wall", "admin": false}},
		"dashboards": []map[string]any{{
			"path":  "lovelace",
			"title": "Overview",
			"views": []map[string]string{{"path": "kitchen", "title": "Kitchen"}},
		}},
		"themes": []map[string]any{
			{"name": "Kitchen", "dark": true},
			{"name": "plain", "dark": false},
		},
	}
	inner, _ := json.Marshal(reply)
	outer, _ := json.Marshal(map[string]string{"response": string(inner)})
	var asked string
	h, f := connected(func(req *api.HomeassistantActionRequest) *api.HomeassistantActionResponse {
		asked = req.GetService()
		return &api.HomeassistantActionResponse{Success: true, ResponseData: outer}
	})
	h.links[f] = &link{access: Allowed, integration: IntegrationReady, protocol: DashboardProtocol}

	got, err := h.Dashboards(context.Background(), "athena")
	if err != nil {
		t.Fatal(err)
	}
	if asked != dashboardsAction {
		t.Errorf("called %s", asked)
	}
	want := []DashboardTheme{{Name: "Kitchen", Dark: true}, {Name: "plain"}}
	if got.Version != "2026.10.2" || len(got.Users) != 1 || len(got.Dashboards) != 1 ||
		len(got.Dashboards[0].Views) != 1 || !slices.Equal(got.Themes, want) {
		t.Errorf("read %+v", got)
	}
}

func TestDashboardsNeedAnIntegrationThatServesThem(t *testing.T) {
	h, f := connected(nil)
	h.links[f] = &link{access: Allowed, integration: IntegrationReady, protocol: WebRTCProtocol}
	if _, err := h.Dashboards(context.Background(), "athena"); err != ErrNoDashboards {
		t.Errorf("got %v, want %v", err, ErrNoDashboards)
	}
}
