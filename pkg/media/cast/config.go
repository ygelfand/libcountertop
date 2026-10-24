package cast

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
)

const appConfigURL = "https://clients3.google.com/cast/chromecast/device/app?a="

type AppConfig struct {
	URL  string
	Name string
	Icon string
}

var configs = struct {
	sync.Mutex
	known map[string]AppConfig
}{known: map[string]AppConfig{}}

func Describe(ctx context.Context, client *http.Client, app string) (AppConfig, error) {
	configs.Lock()
	c, ok := configs.known[app]
	configs.Unlock()
	if ok {
		return c, nil
	}
	c, err := fetchConfig(ctx, client, app)
	if err != nil {
		return AppConfig{}, err
	}
	configs.Lock()
	configs.known[app] = c
	configs.Unlock()
	return c, nil
}

func fetchConfig(ctx context.Context, client *http.Client, app string) (AppConfig, error) {
	if client == nil {
		return AppConfig{}, errors.New("cast: no client to fetch app config")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, appConfigURL+app, nil)
	if err != nil {
		return AppConfig{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return AppConfig{}, fmt.Errorf("cast: app config: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return AppConfig{}, fmt.Errorf("cast: app config for %s: %s", app, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return AppConfig{}, fmt.Errorf("cast: app config: %w", err)
	}
	var cfg struct {
		URL         string `json:"url"`
		DisplayName string `json:"display_name"`
		Promo       []struct {
			Icon   string `json:"icon_url"`
			Locale string `json:"locale"`
		} `json:"promo_assets"`
	}
	if err := json.Unmarshal(
		bytes.TrimSpace(bytes.TrimPrefix(body, []byte(")]}'"))),
		&cfg,
	); err != nil {
		return AppConfig{}, fmt.Errorf("cast: app config: %w", err)
	}
	c := AppConfig{URL: cfg.URL, Name: cfg.DisplayName}
	for _, a := range cfg.Promo {
		if a.Icon != "" && (c.Icon == "" || a.Locale == "") {
			c.Icon = a.Icon
		}
	}
	return c, nil
}
