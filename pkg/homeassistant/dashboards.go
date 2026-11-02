package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	DashboardProtocol = 3

	dashboardsAction = "echolocal.dashboards"
	dashboardToken   = "echolocal.dashboard_token"
)

var ErrNoDashboards = errors.New(
	"homeassistant: no connected Home Assistant has the EchoLocal integration with dashboards",
)

type DashboardUser struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Admin bool   `json:"admin"`
}

type DashboardView struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

type Dashboard struct {
	Path  string          `json:"path"`
	Title string          `json:"title"`
	Admin bool            `json:"admin"`
	Views []DashboardView `json:"views"`
}

type DashboardTheme struct {
	Name string `json:"name"`
	Dark bool   `json:"dark"`
}

type Dashboards struct {
	Version    string           `json:"version"`
	Users      []DashboardUser  `json:"users"`
	Dashboards []Dashboard      `json:"dashboards"`
	Themes     []DashboardTheme `json:"themes"`
}

type DashboardToken struct {
	URL     string
	Access  string
	Expires time.Duration
}

func (h *Controller) Dashboards(ctx context.Context, node string) (Dashboards, error) {
	var out Dashboards
	err := h.askDashboard(ctx, dashboardsAction, map[string]string{"node": node}, &out)
	return out, err
}

func (h *Controller) DashboardToken(
	ctx context.Context,
	node, user string,
) (DashboardToken, error) {
	var out struct {
		URL     string `json:"url"`
		Access  string `json:"access_token"`
		Expires int    `json:"expires_in"`
	}
	err := h.askDashboard(ctx, dashboardToken, map[string]string{"node": node, "user": user}, &out)
	if err != nil {
		return DashboardToken{}, err
	}
	if out.URL == "" || out.Access == "" {
		return DashboardToken{}, errors.New("homeassistant: the dashboard sign-in is empty")
	}
	return DashboardToken{
		URL:     out.URL,
		Access:  out.Access,
		Expires: time.Duration(out.Expires) * time.Second,
	}, nil
}

func (h *Controller) askDashboard(
	ctx context.Context,
	action string,
	data map[string]string,
	into any,
) error {
	conn, ok := h.speaker(DashboardProtocol)
	if !ok {
		return ErrNoDashboards
	}
	raw, err := h.send(ctx, []sender{conn}, request(action, data, nil, pingReply))
	if err != nil {
		return err
	}
	var out struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("homeassistant: the %s response: %w", action, err)
	}
	if err := json.Unmarshal(unwrap(out.Response), into); err != nil {
		return fmt.Errorf("homeassistant: the %s answer: %w", action, err)
	}
	return nil
}
