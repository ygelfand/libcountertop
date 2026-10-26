package photos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	neturl "net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	googleRefresh   = 6 * time.Hour
	googleWait      = 30 * time.Second
	googlePageMost  = 16 << 20
	googleFetchMost = 2048
	googlePagesMost = 20
	googlePageRPC   = "snAcKc"
	googleBatch     = "https://photos.google.com/_/PhotosUi/data/batchexecute"
	googleAgent     = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
)

var googleBlock = regexp.MustCompile(`AF_initDataCallback\(\{`)

type googleSource struct {
	client *http.Client
	most   func() int
}

func Google(most func() int) Provider {
	return googleSource{client: &http.Client{Timeout: googleWait}, most: most}
}

func (googleSource) Key() string { return "google" }

func (googleSource) Name() string { return say.T("photos.google") }

func (googleSource) Status(cfg schema.Photos) Status {
	switch {
	case !cfg.Google.Enabled:
		return Off
	case len(googleWanted(cfg.Google)) == 0:
		return Unset
	}
	return Connecting
}

func (googleSource) Same(a, b schema.Photos) bool { return a.Google == b.Google }

func (g googleSource) Open(ctx context.Context, cfg schema.Photos) (Session, string, error) {
	s := &googleSession{src: g, cfg: cfg.Google, urls: map[string]string{}}
	if err := s.refresh(ctx, time.Now()); err != nil {
		return nil, "", err
	}
	return s, "", nil
}

type googleAlbum struct {
	weight int
	photos []Found
}

type googleSession struct {
	src     googleSource
	cfg     schema.Google
	albums  []googleAlbum
	urls    map[string]string
	fetched time.Time
}

func (s *googleSession) Draw(
	ctx context.Context,
	now time.Time,
	low bool,
) ([]Found, []Found, error) {
	if now.Sub(s.fetched) >= googleRefresh {
		if err := s.refresh(ctx, now); err != nil {
			slog.Warn("photos: refreshing google albums", "err", err)
		}
	}
	if !low {
		return nil, nil, nil
	}
	return nil, s.pick(deckSize), nil
}

func (s *googleSession) Fetch(ctx context.Context, id string) ([]byte, error) {
	base, ok := s.urls[id]
	if !ok {
		return nil, &MissingError{Name: id}
	}
	side := googleFetchMost
	if s.src.most != nil {
		if n := s.src.most(); n > 0 {
			side = n
		}
	}
	body, _, err := s.src.get(ctx, fmt.Sprintf("%s=w%d-h%d", base, side, side))
	return body, err
}

func (s *googleSession) refresh(ctx context.Context, now time.Time) error {
	var (
		albums []googleAlbum
		urls   = map[string]string{}
		first  error
	)
	for _, a := range googleWanted(s.cfg) {
		photos, err := s.src.album(ctx, a.URL)
		if err == nil && len(photos) == 0 {
			err = &MissingError{Name: a.URL}
		}
		if err == nil {
			album := googleAlbum{weight: a.Weight}
			for _, p := range photos {
				urls[p.ID] = p.URL
				album.photos = append(album.photos, Found{ID: p.ID, Taken: p.Taken})
			}
			albums = append(albums, album)
			continue
		}
		slog.Warn("photos: reading a google album", "album", a.URL, "err", err)
		if first == nil {
			first = err
		}
	}
	if len(albums) == 0 {
		return first
	}
	s.albums, s.urls, s.fetched = albums, urls, now
	return nil
}

func (s *googleSession) pick(n int) []Found {
	total := 0
	for _, a := range s.albums {
		total += a.weight
	}
	if total == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []Found
	for range n * 2 {
		if len(out) == n {
			break
		}
		r := rand.IntN(total)
		var a googleAlbum
		for _, a = range s.albums {
			if r < a.weight {
				break
			}
			r -= a.weight
		}
		if len(a.photos) == 0 {
			continue
		}
		f := a.photos[rand.IntN(len(a.photos))]
		if !seen[f.ID] {
			seen[f.ID] = true
			out = append(out, f)
		}
	}
	return out
}

func (g googleSource) album(ctx context.Context, url string) ([]googlePhoto, error) {
	page, final, err := g.get(ctx, url)
	if err != nil {
		return nil, err
	}
	data, err := googleData(page)
	if err != nil {
		return nil, err
	}
	photos, token, id := parseGoogleData(data)
	key := final.Query().Get("key")
	for pages := 1; token != "" && id != "" && pages < googlePagesMost; pages++ {
		more, err := g.page(ctx, id, token, key)
		if err != nil {
			slog.Warn("photos: reading more of a google album", "album", url, "err", err)
			break
		}
		var got []googlePhoto
		got, token, _ = parseGoogleData(more)
		photos = append(photos, got...)
	}
	return photos, nil
}

func (g googleSource) get(ctx context.Context, url string) ([]byte, *neturl.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	body, final, err := g.do(req)
	return body, final, err
}

func (g googleSource) page(ctx context.Context, id, token, key string) ([]json.RawMessage, error) {
	inner, err := json.Marshal([]any{id, token, nil, key})
	if err != nil {
		return nil, err
	}
	freq, err := json.Marshal([][][]any{{{googlePageRPC, string(inner), nil, "generic"}}})
	if err != nil {
		return nil, err
	}
	form := neturl.Values{"f.req": {string(freq)}}
	target := googleBatch + "?rpcids=" + googlePageRPC + "&rt=c&source-path=" +
		neturl.QueryEscape("/share/"+id)
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		target,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	body, _, err := g.do(req)
	if err != nil {
		return nil, err
	}
	return batchData(body)
}

func (g googleSource) do(req *http.Request) ([]byte, *neturl.URL, error) {
	req.Header.Set("User-Agent", googleAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	url := req.URL.String()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil, &MissingError{Name: url}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("photos: google answered %s for %s", resp.Status, url)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, googlePageMost))
	return body, resp.Request.URL, err
}

func googleWanted(cfg schema.Google) []schema.GoogleAlbum {
	var out []schema.GoogleAlbum
	for _, a := range cfg.Albums {
		if strings.TrimSpace(a.URL) != "" && a.Weight > 0 {
			a.URL = strings.TrimSpace(a.URL)
			out = append(out, a)
		}
	}
	return out
}

type googlePhoto struct {
	ID    string
	URL   string
	Taken string
}

var errGoogleShape = errors.New("photos: the google album page has no photo list")

func googleData(page []byte) ([]json.RawMessage, error) {
	var best []json.RawMessage
	most := -1
	for _, at := range googleBlock.FindAllIndex(page, -1) {
		rest := page[at[1]:]
		i := bytes.Index(rest, []byte("data:"))
		if i < 0 {
			continue
		}
		var data []json.RawMessage
		if err := json.NewDecoder(bytes.NewReader(rest[i+len("data:"):])).
			Decode(&data); err != nil {
			continue
		}
		if len(data) < 2 {
			continue
		}
		var list []json.RawMessage
		if err := json.Unmarshal(data[1], &list); err != nil {
			continue
		}
		if len(list) > most {
			best, most = data, len(list)
		}
	}
	if best == nil {
		return nil, errGoogleShape
	}
	return best, nil
}

func batchData(body []byte) ([]json.RawMessage, error) {
	i := bytes.Index(body, []byte("[["))
	if i < 0 {
		return nil, errGoogleShape
	}
	var frames [][]json.RawMessage
	if err := json.NewDecoder(bytes.NewReader(body[i:])).Decode(&frames); err != nil {
		return nil, err
	}
	for _, f := range frames {
		var kind, rpc, payload string
		if len(f) < 3 || json.Unmarshal(f[0], &kind) != nil || json.Unmarshal(f[1], &rpc) != nil ||
			kind != "wrb.fr" || rpc != googlePageRPC || json.Unmarshal(f[2], &payload) != nil {
			continue
		}
		var data []json.RawMessage
		if err := json.Unmarshal([]byte(payload), &data); err != nil {
			return nil, err
		}
		return data, nil
	}
	return nil, errGoogleShape
}

func parseGoogleAlbum(page []byte) ([]googlePhoto, error) {
	data, err := googleData(page)
	if err != nil {
		return nil, err
	}
	photos, _, _ := parseGoogleData(data)
	return photos, nil
}

func parseGoogleData(data []json.RawMessage) ([]googlePhoto, string, string) {
	var (
		list  []json.RawMessage
		token string
		id    string
	)
	if len(data) > 1 {
		_ = json.Unmarshal(data[1], &list)
	}
	if len(data) > 2 {
		_ = json.Unmarshal(data[2], &token)
	}
	if len(data) > 3 {
		var meta []json.RawMessage
		if json.Unmarshal(data[3], &meta) == nil && len(meta) > 0 {
			_ = json.Unmarshal(meta[0], &id)
		}
	}
	var out []googlePhoto
	for _, raw := range list {
		var e []json.RawMessage
		if json.Unmarshal(raw, &e) != nil || len(e) < 3 {
			continue
		}
		var (
			pid    string
			detail []json.RawMessage
			url    string
			stamp  float64
		)
		if json.Unmarshal(e[0], &pid) != nil || json.Unmarshal(e[1], &detail) != nil ||
			len(detail) < 1 || json.Unmarshal(detail[0], &url) != nil ||
			!strings.HasPrefix(url, "https://") {
			continue
		}
		p := googlePhoto{ID: pid, URL: url}
		if json.Unmarshal(e[2], &stamp) == nil && stamp > 0 {
			p.Taken = time.UnixMilli(int64(stamp)).UTC().Format(time.RFC3339)
		}
		out = append(out, p)
	}
	return out, token, id
}
