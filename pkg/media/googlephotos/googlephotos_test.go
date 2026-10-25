package googlephotos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testAlbum = "AF1QipAlbumKey"
	testAuth  = "auth-key"
)

// google fakes the two endpoints a shared album is read from: the share page
// that carries the keys, and the batchexecute pages of items.
type google struct {
	title     string
	pages     [][]any
	failAfter int
	calls     int
	mu        sync.Mutex
}

func (g *google) start(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /share/{album}", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != testAuth {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!doctype html><html><head><title>%s - Google Photos`+
			`</title></head><body><script>AF_dataServiceRequests = {'snAcKc': `+
			`{id:'snAcKc',request:["%s",null,null,"%s"]}};</script></body></html>`,
			g.title, testAlbum, testAuth)
	})
	mux.HandleFunc(
		"POST /u/0/_/PhotosUi/data/batchexecute",
		func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var envelope [][][]any
			if err := json.Unmarshal([]byte(r.PostForm.Get("f.req")), &envelope); err != nil {
				http.Error(w, "bad envelope", http.StatusBadRequest)
				return
			}
			inner := envelope[0][0][1].(string)
			var request []any
			if err := json.Unmarshal([]byte(inner), &request); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			g.mu.Lock()
			call := g.calls
			g.calls++
			g.mu.Unlock()
			if request[0] != testAlbum || request[3] != testAuth {
				http.Error(w, "bad keys", http.StatusBadRequest)
				return
			}
			if g.failAfter > 0 && call >= g.failAfter {
				http.Error(w, "page is broken", http.StatusInternalServerError)
				return
			}
			if call >= len(g.pages) {
				http.Error(w, "no pages left", http.StatusInternalServerError)
				return
			}
			var next string
			if call+1 < len(g.pages) {
				next = fmt.Sprintf("page-%d", call+1)
			}
			writePage(t, w, g.pages[call], next)
		})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func (g *google) link(srv *httptest.Server) string {
	return srv.URL + "/share/" + testAlbum + "?key=" + testAuth
}

func writePage(t *testing.T, w http.ResponseWriter, items []any, next string) {
	t.Helper()
	inner, err := json.Marshal([]any{nil, items, next})
	if err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal([]any{[]any{"wrb.fr", "snAcKc", string(inner), nil, nil, "generic"}})
	if err != nil {
		t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, ")]}'\n\n%d\n%s\n", len(line), line)
}

func photo(id string, taken int64) []any {
	return []any{
		id,
		[]any{
			"https://lh3.googleusercontent.com/p/" + id + "=w4000-h2000-k-no",
			float64(4000),
			float64(2000),
			nil,
			[]any{float64(123456)},
		},
		float64(taken),
		"dedup",
		nil,
		float64(taken + 1000),
		map[string]any{},
	}
}

func video(id string) []any {
	return []any{
		id,
		[]any{
			"https://lh3.googleusercontent.com/p/" + id + "=w4000-h2000-k-no",
			float64(4000),
			float64(2000),
		},
		float64(1600000000000),
		nil,
		nil,
		float64(1600000001000),
		map[string]any{videoKey: float64(2500)},
	}
}

func TestAlbumReadsEveryPageAndSkipsVideos(t *testing.T) {
	g := &google{
		title: "Summer",
		pages: [][]any{
			{photo("one", 1600000000000), video("clip"), photo("two", 1500000000000)},
			{photo("three", 0)},
		},
	}
	srv := g.start(t)
	c := New()
	album, err := c.Album(context.Background(), g.link(srv))
	if err != nil {
		t.Fatal(err)
	}
	if album.Title != "Summer" {
		t.Errorf("title %q, want Summer", album.Title)
	}
	var ids []string
	for _, p := range album.Photos {
		ids = append(ids, p.ID)
	}
	if got := strings.Join(ids, ","); got != "one,two,three" {
		t.Fatalf("ids %q, want one,two,three", got)
	}
	if g.calls != 2 {
		t.Errorf("read %d pages, want 2", g.calls)
	}
	first := album.Photos[0]
	if first.URL != "https://lh3.googleusercontent.com/p/one=w1920-h960" {
		t.Errorf("url %q", first.URL)
	}
	if first.Width != 4000 || first.Height != 2000 {
		t.Errorf("size %dx%d", first.Width, first.Height)
	}
	if !first.Taken.Equal(time.UnixMilli(1600000000000)) {
		t.Errorf("taken %v", first.Taken)
	}
	if !album.Photos[2].Taken.IsZero() {
		t.Errorf("a nonsense timestamp became %v", album.Photos[2].Taken)
	}
}

func TestAlbumStopsAtABrokenLaterPage(t *testing.T) {
	g := &google{
		title:     "Summer",
		pages:     [][]any{{photo("one", 0)}, {photo("two", 0)}},
		failAfter: 1,
	}
	srv := g.start(t)
	album, err := New().Album(context.Background(), g.link(srv))
	if err != nil {
		t.Fatal(err)
	}
	if len(album.Photos) != 1 || album.Photos[0].ID != "one" {
		t.Errorf("got %v, want the first page only", album.Photos)
	}
}

func TestAPrivateAlbumSaysSo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><title>Sign in</title></html>"))
	}))
	t.Cleanup(srv.Close)
	if _, err := New().Album(context.Background(), srv.URL+"/share/x"); !errors.Is(err, ErrLink) {
		t.Errorf("got %v, want ErrLink", err)
	}
	if _, err := New().Album(context.Background(), "not a link"); !errors.Is(err, ErrLink) {
		t.Errorf("got %v, want ErrLink", err)
	}
}

func TestPictureDownloadsThePhoto(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != agent {
			t.Errorf("user agent %q", got)
		}
		_, _ = w.Write([]byte("jpeg"))
	}))
	t.Cleanup(srv.Close)
	data, err := New().Picture(context.Background(), srv.URL+"/p/x=w100-h100")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "jpeg" {
		t.Errorf("data %q", data)
	}
	if _, err := New().Picture(context.Background(), "file:///etc/passwd"); err == nil {
		t.Error("a file URL was fetched")
	}
}

func TestSplitLinks(t *testing.T) {
	got := SplitLinks(" https://a.example/x ,\nhttps://b.example/y\r\n\n")
	want := []string{"https://a.example/x", "https://b.example/y"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := SplitLinks("  ,, "); len(got) != 0 {
		t.Errorf("blank setting gave %q", got)
	}
}

func TestResize(t *testing.T) {
	for _, c := range []struct {
		in   string
		w, h int
		want string
	}{
		{
			"https://x/p/a=w4000-h2000-k-no", 4000, 2000,
			"https://x/p/a=w1920-h960",
		},
		{"https://x/p/a=w100-h50", 100, 50, "https://x/p/a=w100-h50"},
		{"https://x/p/a", 0, 0, "https://x/p/a=w1920-h1080"},
		{"https://x/p/a=s0", 800, 600, "https://x/p/a=w800-h600"},
	} {
		if got := resize(c.in, c.w, c.h); got != c.want {
			t.Errorf("resize(%q, %d, %d) = %q, want %q", c.in, c.w, c.h, got, c.want)
		}
	}
}

func TestParsePageWithoutALengthLine(t *testing.T) {
	inner, _ := json.Marshal([]any{nil, []any{photo("one", 0)}, ""})
	line, _ := json.Marshal([]any{[]any{"wrb.fr", "snAcKc", string(inner)}})
	photos, next, err := parsePage(")]}'\n\n" + string(line))
	if err != nil {
		t.Fatal(err)
	}
	if len(photos) != 1 || photos[0].ID != "one" || next != "" {
		t.Errorf("got %v %q", photos, next)
	}
	if _, _, err := parsePage(")]}'\n\n123\n"); !errors.Is(err, errNoPage) {
		t.Errorf("got %v", err)
	}
}

func TestParseKeysNeedsTheRequestBlob(t *testing.T) {
	html := `{"snAcKc":{request:["album",null,null,"auth"]}}`
	keys, ok := parseKeys(html)
	if !ok || keys.album != "album" || keys.auth != "auth" {
		t.Errorf("keys %+v %v", keys, ok)
	}
	if _, ok := parseKeys(`{"snAcKc":{}}`); ok {
		t.Error("keys from nothing")
	}
}
