// Package googlephotos reads photos from public Google Photos shared albums.
//
// Google has no supported API for albums shared by link, so this speaks the
// same undocumented endpoints the web viewer uses: the share page is fetched
// once to learn the album and auth keys, then every page of items comes from
// the batchexecute endpoint with the snAcKc RPC. Nothing here needs a Google
// account, and nothing is written anywhere.
//
// The positional layout of an album item is verified field by field and
// malformed entries are skipped, because Google reshuffles it from time to
// time. Only still photos are returned; videos and live photos are dropped.
package googlephotos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// agent is a desktop browser user agent; Google serves a script-only
	// shim to clients that look like bots, with no album keys in it.
	agent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) " +
		"Chrome/124.0.0.0 Safari/537.36"

	// xssi is the prefix Google puts in front of batchexecute responses.
	xssi = ")]}'"

	maxPicture = 32 << 20
	maxPage    = 16 << 20
	maxItems   = 20000
	maxPages   = 100

	// photoSide caps the longest side of a requested picture. The display is
	// far smaller than this; it keeps a cache of them on a small SD card.
	photoSide = 1920

	// videoKey marks an item that carries a duration: a video, or a live
	// photo, which we skip because only stills can sit behind the clock.
	videoKey = "76647426"
)

// ErrLink means a shared album could not be read: the link is wrong, private,
// or Google changed the page enough that the album keys are gone.
var ErrLink = errors.New("googlephotos: that shared album is not public")

var errNoPage = errors.New("googlephotos: unexpected album response")

// Client reads public albums. The zero value works; set HTTP to share a
// connection pool or a timeout.
type Client struct {
	HTTP *http.Client
}

// New returns a client with a generous timeout for large albums.
func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// Album is one shared album, in the order Google lists it.
type Album struct {
	Title  string
	Photos []Photo
}

// Photo is one still photo of a shared album.
type Photo struct {
	ID     string
	URL    string
	Width  int
	Height int
	Taken  time.Time
}

// SplitLinks splits a setting value into album links. Links may be separated
// by commas, spaces, or new lines.
func SplitLinks(list string) []string {
	fields := strings.FieldsFunc(list, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
	})
	links := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			links = append(links, f)
		}
	}
	return links
}

// Album reads every still photo of one shared album link. Links of the form
// https://photos.app.goo.gl/... and https://photos.google.com/share/... are
// both accepted; any redirect is followed.
func (c *Client) Album(ctx context.Context, link string) (Album, error) {
	link = strings.TrimSpace(link)
	if !shared(link) {
		return Album{}, ErrLink
	}
	page, final, err := c.fetch(ctx, link, "text/html,application/xhtml+xml", maxPage)
	if err != nil {
		return Album{}, err
	}
	html := string(page)
	keys, ok := parseKeys(html)
	if !ok {
		return Album{}, ErrLink
	}
	album := Album{Title: parseTitle(html)}
	origin := final.Scheme + "://" + final.Host
	referer := origin + "/share/" + keys.album + "?key=" + keys.auth

	seen := map[string]bool{}
	next := ""
	for range maxPages {
		photos, more, err := c.page(ctx, keys, origin, referer, next)
		if err != nil {
			if seen == nil || len(album.Photos) == 0 {
				return Album{}, err
			}
			// A later page failing still leaves a usable album.
			break
		}
		added := 0
		for _, p := range photos {
			if seen[p.ID] {
				continue
			}
			seen[p.ID] = true
			album.Photos = append(album.Photos, p)
			added++
			if len(album.Photos) >= maxItems {
				return album, nil
			}
		}
		if more == "" || added == 0 {
			break
		}
		next = more
	}
	return album, nil
}

// Picture downloads one photo returned by Album.
func (c *Client) Picture(ctx context.Context, link string) ([]byte, error) {
	if !shared(link) {
		return nil, fmt.Errorf("googlephotos: refusing to fetch %q", link)
	}
	data, _, err := c.fetch(ctx, link, "image/*", maxPicture)
	return data, err
}

func (c *Client) page(
	ctx context.Context, keys albumKeys, origin, referer, next string,
) ([]Photo, string, error) {
	var token any
	if next != "" {
		token = next
	}
	inner, err := json.Marshal([]any{keys.album, token, nil, keys.auth})
	if err != nil {
		return nil, "", err
	}
	envelope, err := json.Marshal([]any{[]any{[]any{"snAcKc", string(inner), nil, "generic"}}})
	if err != nil {
		return nil, "", err
	}
	endpoint := origin + "/u/0/_/PhotosUi/data/batchexecute?rpcids=snAcKc&source-path=" +
		url.QueryEscape("/share/"+keys.album)
	body := strings.NewReader("f.req=" + url.QueryEscape(string(envelope)))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, "", fmt.Errorf("googlephotos: %w", err)
	}
	req.Header.Set("User-Agent", agent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", origin)
	req.Header.Set("Referer", referer)

	resp, err := c.client().Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("googlephotos: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("googlephotos: album page: %s", resp.Status)
	}
	data, err := read(resp.Body, maxPage)
	if err != nil {
		return nil, "", err
	}
	return parsePage(string(data))
}

func (c *Client) fetch(
	ctx context.Context, link, accept string, limit int64,
) ([]byte, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("googlephotos: %w", err)
	}
	req.Header.Set("User-Agent", agent)
	req.Header.Set("Accept", accept)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := c.client().Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("googlephotos: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("googlephotos: %s: %s", link, resp.Status)
	}
	data, err := read(resp.Body, limit)
	if err != nil {
		return nil, nil, err
	}
	return data, resp.Request.URL, nil
}

func (c *Client) client() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func read(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("googlephotos: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("googlephotos: response is over %d bytes", limit)
	}
	return data, nil
}

func shared(link string) bool {
	u, err := url.Parse(link)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

type albumKeys struct {
	album string
	auth  string
}

var (
	// The share page carries the snAcKc request as a JavaScript literal:
	//   "snAcKc",ext: ... ,request:["<albumKey>",null,null,"<authKey>"]
	keyRE = regexp.MustCompile(`(?s)snAcKc[^}]*?request:\s*\[\s*"([A-Za-z0-9_-]+)"\s*,` +
		`\s*null\s*,\s*null\s*,\s*"([A-Za-z0-9_-]+)"`)

	titleRE = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

	// Google appends a size hint to the image path, such as =w1920-h1080-k-no.
	sizeRE = regexp.MustCompile(`(?i)=[whs]\d+(?:-[a-z0-9]+)*$`)
)

func parseKeys(html string) (albumKeys, bool) {
	m := keyRE.FindStringSubmatch(html)
	if m == nil {
		return albumKeys{}, false
	}
	return albumKeys{album: m[1], auth: m[2]}, true
}

func parseTitle(html string) string {
	m := titleRE.FindStringSubmatch(html)
	if m == nil {
		return ""
	}
	title := strings.TrimSpace(m[1])
	return strings.TrimSpace(strings.TrimSuffix(title, "- Google Photos"))
}

// parsePage reads one batchexecute response. It returns the photos and the
// token for the next page, which is empty on the last page.
func parsePage(body string) ([]Photo, string, error) {
	text := strings.TrimSpace(body)
	text = strings.TrimSpace(strings.TrimPrefix(text, xssi))
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[") {
			continue
		}
		var entries any
		if err := json.Unmarshal([]byte(line), &entries); err != nil {
			continue
		}
		inner, ok := findRPC(entries)
		if !ok {
			continue
		}
		return parseInner(inner)
	}
	return nil, "", errNoPage
}

func findRPC(v any) (string, bool) {
	list, ok := v.([]any)
	if !ok {
		return "", false
	}
	if len(list) >= 3 && list[0] == "wrb.fr" && list[1] == "snAcKc" {
		s, ok := list[2].(string)
		return s, ok
	}
	for _, item := range list {
		if s, ok := findRPC(item); ok {
			return s, true
		}
	}
	return "", false
}

func parseInner(raw string) ([]Photo, string, error) {
	var inner []any
	if err := json.Unmarshal([]byte(raw), &inner); err != nil {
		return nil, "", fmt.Errorf("googlephotos: album page: %w", err)
	}
	var items []any
	if len(inner) > 1 {
		items, _ = inner[1].([]any)
	}
	next := ""
	if len(inner) > 2 {
		next, _ = inner[2].(string)
	}
	photos := make([]Photo, 0, len(items))
	for _, raw := range items {
		if isVideo(raw) {
			continue
		}
		if p, ok := parseItem(raw); ok {
			photos = append(photos, p)
		}
	}
	return photos, next, nil
}

// parseItem reads the positional item array:
//
//	[mediaKey, [url, width, height, ..., [byteSize]], capturedMS, ..., uploadedMS, ...]
func parseItem(raw any) (Photo, bool) {
	list, ok := raw.([]any)
	if !ok || len(list) < 2 {
		return Photo{}, false
	}
	id, _ := list[0].(string)
	visual, ok := list[1].([]any)
	if id == "" || !ok || len(visual) < 3 {
		return Photo{}, false
	}
	link, ok := visual[0].(string)
	if !ok || !strings.HasPrefix(link, "http") {
		return Photo{}, false
	}
	width, _ := whole(visual[1])
	height, _ := whole(visual[2])
	var taken time.Time
	if len(list) > 2 {
		if ms, ok := epoch(list[2]); ok && plausible(ms) {
			taken = time.UnixMilli(ms)
		}
	}
	return Photo{
		ID:     id,
		URL:    resize(link, width, height),
		Width:  width,
		Height: height,
		Taken:  taken,
	}, true
}

// isVideo reports whether an item carries a duration. The metadata dictionary
// holding it moves between responses, so every nested container is checked.
func isVideo(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		if _, ok := t[videoKey]; ok {
			return true
		}
		for _, child := range t {
			if isVideo(child) {
				return true
			}
		}
	case []any:
		for _, child := range t {
			if isVideo(child) {
				return true
			}
		}
	}
	return false
}

// resize strips any size hint from the URL and asks for one that fits the
// display, keeping the photo's aspect ratio.
func resize(link string, width, height int) string {
	base := sizeRE.ReplaceAllString(link, "")
	if width <= 0 || height <= 0 {
		return base + "=w" + strconv.Itoa(photoSide) + "-h" + strconv.Itoa(photoSide*9/16)
	}
	longest := max(width, height)
	if longest > photoSide {
		scale := float64(photoSide) / float64(longest)
		width = max(1, int(math.Round(float64(width)*scale)))
		height = max(1, int(math.Round(float64(height)*scale)))
	}
	return base + "=w" + strconv.Itoa(width) + "-h" + strconv.Itoa(height)
}

// whole returns an integral JSON number as an int.
func whole(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n != math.Trunc(n) || n < math.MinInt32 || n > math.MaxInt32 {
			return 0, false
		}
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		if err != nil || i < math.MinInt32 || i > math.MaxInt32 {
			return 0, false
		}
		return int(i), true
	}
	return 0, false
}

// epoch returns an integral JSON number as epoch milliseconds.
func epoch(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		if n != math.Trunc(n) || n < math.MinInt64/2 || n > math.MaxInt64/2 {
			return 0, false
		}
		return int64(n), true
	case int64:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}

// plausible reports whether ms looks like a real epoch timestamp.
func plausible(ms int64) bool {
	const (
		minMS = -5364662400000 // 1800-01-01
		maxMS = 4102444800000  // 2100-01-01
	)
	return ms != 0 && ms >= minMS && ms <= maxMS
}
