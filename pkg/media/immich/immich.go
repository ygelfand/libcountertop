package immich

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	maxPicture = 32 << 20
	maxRandom  = 1000
	peoplePage = 1000
)

var (
	ErrKey      = errors.New("immich: the API key was refused")
	ErrNoServer = errors.New("immich: no server set")
)

type Client struct {
	Base string
	Key  string
	HTTP *http.Client
}

type About struct {
	Version string `json:"version"`
}

type Album struct {
	ID     string `json:"id"`
	Name   string `json:"albumName"`
	Assets int    `json:"assetCount"`
}

type Tag struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Person struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Hidden bool   `json:"isHidden"`
}

type Exif struct {
	Taken   string `json:"dateTimeOriginal"`
	City    string `json:"city"`
	State   string `json:"state"`
	Country string `json:"country"`
}

type Asset struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Width    int      `json:"width"`
	Height   int      `json:"height"`
	Local    string   `json:"localDateTime"`
	Favorite bool     `json:"isFavorite"`
	Exif     *Exif    `json:"exifInfo"`
	People   []Person `json:"people"`
}

type Memory struct {
	ID     string  `json:"id"`
	Type   string  `json:"type"`
	Assets []Asset `json:"assets"`
	Data   struct {
		Year int `json:"year"`
	} `json:"data"`
}

type Selection struct {
	Albums    []string
	Tags      []string
	People    []string
	Favorites bool
}

func New(base, key string) *Client {
	return &Client{
		Base: strings.TrimRight(base, "/"),
		Key:  key,
		HTTP: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) About(ctx context.Context) (About, error) {
	var out About
	return out, c.call(ctx, http.MethodGet, "/api/server/about", nil, &out)
}

func (c *Client) KeyPermissions(ctx context.Context) ([]string, error) {
	var out struct {
		Permissions []string `json:"permissions"`
	}
	return out.Permissions, c.call(ctx, http.MethodGet, "/api/api-keys/me", nil, &out)
}

func (c *Client) Albums(ctx context.Context) ([]Album, error) {
	var out []Album
	return out, c.call(ctx, http.MethodGet, "/api/albums", nil, &out)
}

func (c *Client) Tags(ctx context.Context) ([]Tag, error) {
	var out []Tag
	return out, c.call(ctx, http.MethodGet, "/api/tags", nil, &out)
}

func (c *Client) People(ctx context.Context) ([]Person, error) {
	var all []Person
	for page := 1; ; page++ {
		var out struct {
			People []Person `json:"people"`
			More   bool     `json:"hasNextPage"`
		}
		path := fmt.Sprintf("/api/people?page=%d&size=%d", page, peoplePage)
		if err := c.call(ctx, http.MethodGet, path, nil, &out); err != nil {
			return nil, err
		}
		all = append(all, out.People...)
		if !out.More {
			return all, nil
		}
	}
}

func (c *Client) Random(ctx context.Context, s Selection, n int) ([]Asset, error) {
	filter := map[string]any{
		"type":       map[string]any{"eq": "IMAGE"},
		"visibility": map[string]any{"eq": "timeline"},
	}
	for key, ids := range map[string][]string{
		"albumIds":  s.Albums,
		"tagIds":    s.Tags,
		"personIds": s.People,
	} {
		if len(ids) > 0 {
			filter[key] = map[string]any{"any": ids}
		}
	}
	if s.Favorites {
		filter["isFavorite"] = map[string]any{"eq": true}
	}
	body := map[string]any{
		"filter":     filter,
		"size":       min(max(n, 1), maxRandom),
		"withExif":   true,
		"withPeople": true,
	}
	var out []Asset
	return out, c.call(ctx, http.MethodPost, "/api/search/random", body, &out)
}

func (c *Client) Memories(ctx context.Context, day time.Time) ([]Memory, error) {
	var out []Memory
	path := "/api/memories?type=on_this_day&for=" + day.Format(time.DateOnly)
	return out, c.call(ctx, http.MethodGet, path, nil, &out)
}

const (
	Preview  = "preview"
	Fullsize = "fullsize"
)

func (c *Client) Picture(ctx context.Context, id, size string) ([]byte, error) {
	path := "/api/assets/" + url.PathEscape(id) + "/thumbnail?size=" + url.QueryEscape(size)
	req, err := c.request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "image/*")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("immich: %w", err)
	}
	defer resp.Body.Close()
	if err := status(resp, http.MethodGet, path); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxPicture+1))
	if err != nil {
		return nil, fmt.Errorf("immich: preview of %s: %w", id, err)
	}
	if len(b) > maxPicture {
		return nil, fmt.Errorf("immich: preview of %s is over %d bytes", id, maxPicture)
	}
	return b, nil
}

func Supported(version string) bool {
	parts := strings.SplitN(strings.TrimPrefix(version, "v"), ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return major > 3 || major == 3 && minor >= 2
}

func (c *Client) call(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := c.request(ctx, method, path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("immich: %w", err)
	}
	defer resp.Body.Close()
	if err := status(resp, method, path); err != nil {
		return err
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out); err != nil {
		return fmt.Errorf("immich: %s %s: %w", method, path, err)
	}
	return nil
}

func status(resp *http.Response, method, path string) error {
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return nil
	case http.StatusUnauthorized:
		return ErrKey
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode == http.StatusForbidden {
		var body struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(msg, &body)
		if perm, ok := strings.CutPrefix(body.Message, missingPermission); ok {
			return &PermissionError{Permission: strings.TrimSpace(perm)}
		}
		return ErrKey
	}
	return fmt.Errorf(
		"immich: %s %s: %s %s",
		method,
		path,
		resp.Status,
		strings.TrimSpace(string(msg)),
	)
}

func (c *Client) request(
	ctx context.Context,
	method, path string,
	body io.Reader,
) (*http.Request, error) {
	if c.Base == "" {
		return nil, ErrNoServer
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, body)
	if err != nil {
		return nil, fmt.Errorf("immich: %w", err)
	}
	req.Header.Set("x-api-key", c.Key)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

const missingPermission = "Missing required permission:"

type PermissionError struct{ Permission string }

func (e *PermissionError) Error() string {
	return fmt.Sprintf("immich: the API key lacks %s", e.Permission)
}
