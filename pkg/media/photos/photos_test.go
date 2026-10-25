package photos

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type server struct {
	version string
	key     string
	assets  int
	memory  string

	mu       sync.Mutex
	searches []map[string]any
	previews int
}

func (s *server) start(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("x-api-key") != s.key {
				http.Error(w, "{}", http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /api/server/about", auth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"version":%q}`, s.version)
	}))
	mux.HandleFunc("GET /api/albums", auth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"album-1","albumName":"Kitchen","assetCount":3}]`))
	}))
	mux.HandleFunc("GET /api/memories", auth(func(w http.ResponseWriter, _ *http.Request) {
		if s.memory == "" {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = fmt.Fprintf(w, `[{"id":"m","type":"on_this_day","data":{"year":2019},`+
			`"assets":[{"id":%q,"type":"IMAGE"}]}]`, s.memory)
	}))
	mux.HandleFunc("POST /api/search/random", auth(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		s.searches = append(s.searches, body)
		s.mu.Unlock()
		var out []map[string]any
		for i := range s.assets {
			out = append(out, map[string]any{
				"id":            fmt.Sprintf("a%d", i),
				"type":          "IMAGE",
				"localDateTime": "2020-05-01T10:00:00.000Z",
				"exifInfo":      map[string]any{"city": "Lisbon", "country": "Portugal"},
				"people": []map[string]any{
					{"id": "p", "name": "Ana"},
					{"id": "q", "name": ""},
				},
			})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	mux.HandleFunc(
		"GET /api/assets/{id}/thumbnail",
		auth(func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			s.previews++
			s.mu.Unlock()
			_, _ = w.Write([]byte("jpeg " + r.PathValue("id")))
		}),
	)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

type settings struct{ schema.Photos }

func (s *settings) Read() schema.Photos { return s.Photos }

func (s *settings) Update(change func(*schema.Photos)) error { change(&s.Photos); return nil }

func library(t *testing.T, cfg schema.Photos, dir string) (*Library, *settings) {
	t.Helper()
	s := &settings{cfg}
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	return New(Options{Settings: s, Dir: dir, Keep: 4, Now: func() time.Time { return now }}), s
}

func TestNothingHappensWithoutAServer(t *testing.T) {
	l, _ := library(t, schema.Photos{}, t.TempDir())
	l.step(context.Background())
	if got := l.State().Status; got != Unset {
		t.Errorf("status %v, want Unset", got)
	}
	if _, ok := l.Next(); ok {
		t.Error("a picture without a server")
	}
}

func TestItKeepsPicturesAheadWithMemoriesFirst(t *testing.T) {
	srv := &server{version: "v3.3.1", key: "k", assets: 10, memory: "mem"}
	l, _ := library(t, schema.Photos{Server: srv.start(t), Key: "k", Memories: true}, t.TempDir())

	l.step(context.Background())
	if st := l.State(); st.Status != Ready || st.Version != "v3.3.1" {
		t.Fatalf("state %+v", st)
	}
	if srv.previews != 4 {
		t.Errorf("downloaded %d previews, want 4 ahead", srv.previews)
	}
	first, ok := l.Next()
	if !ok || first.ID != "mem" || first.Year != 2019 || string(first.Data) != "jpeg mem" {
		t.Fatalf("first picture %+v", first)
	}
	second, _ := l.Next()
	if second.Place != "Lisbon, Portugal" || len(second.People) != 1 ||
		second.Taken.Year() != 2020 {
		t.Errorf("second picture %+v", second)
	}

	l.step(context.Background())
	if srv.previews != 6 {
		t.Errorf("after showing two, downloaded %d previews in all, want 6", srv.previews)
	}
	if len(srv.searches) != 1 {
		t.Errorf("searched %d times with a full deck, want 1", len(srv.searches))
	}
	filter, _ := srv.searches[0]["filter"].(map[string]any)
	if filter["type"] == nil || filter["albumIds"] != nil {
		t.Errorf("filter %v", filter)
	}
}

func TestNoPictureRepeatsUntilTheDeckRunsOut(t *testing.T) {
	srv := &server{version: "v3.3.1", key: "k", assets: 3}
	l, _ := library(t, schema.Photos{Server: srv.start(t), Key: "k"}, t.TempDir())
	l.step(context.Background())
	l.step(context.Background())
	seen := map[string]bool{}
	for range 3 {
		p, ok := l.Next()
		if !ok || seen[p.ID] {
			t.Fatalf("repeated or missing %+v", p)
		}
		seen[p.ID] = true
	}
	if p, ok := l.Next(); !ok || !seen[p.ID] {
		t.Errorf("after the deck ran out, got %+v; want a cached one again", p)
	}
}

func TestNamesResolveAndUnknownNamesSaySo(t *testing.T) {
	srv := &server{version: "v3.3.1", key: "k", assets: 1}
	url := srv.start(t)
	l, s := library(t, schema.Photos{Server: url, Key: "k", Albums: "kitchen"}, t.TempDir())
	l.step(context.Background())
	filter, _ := srv.searches[0]["filter"].(map[string]any)
	albums, _ := filter["albumIds"].(map[string]any)
	if fmt.Sprint(albums["any"]) != "[album-1]" {
		t.Errorf("albums %v", albums)
	}

	s.Albums = "Trips"
	l.step(context.Background())
	if st := l.State(); st.Status != Unknown || st.Missing != "Trips" {
		t.Errorf("state %+v", st)
	}
}

func TestProblemsShowAsStatus(t *testing.T) {
	for _, c := range []struct {
		name    string
		version string
		key     string
		want    Status
	}{
		{"old server", "v3.1.0", "k", TooOld},
		{"wrong key", "v3.3.1", "nope", BadKey},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := &server{version: c.version, key: "k", assets: 1}
			l, _ := library(t, schema.Photos{Server: srv.start(t), Key: c.key}, t.TempDir())
			l.step(context.Background())
			if got := l.State().Status; got != c.want {
				t.Errorf("status %v, want %v", got, c.want)
			}
		})
	}
	l, _ := library(t, schema.Photos{Server: "http://127.0.0.1:1", Key: "k"}, t.TempDir())
	l.step(context.Background())
	if got := l.State().Status; got != Unreachable {
		t.Errorf("status %v, want Unreachable", got)
	}
}

func TestCachedPicturesSurviveARestartWithoutTheServer(t *testing.T) {
	dir := t.TempDir()
	srv := &server{version: "v3.3.1", key: "k", assets: 5}
	l, _ := library(t, schema.Photos{Server: srv.start(t), Key: "k"}, dir)
	l.step(context.Background())

	again, _ := library(t, schema.Photos{Server: "http://127.0.0.1:1", Key: "k"}, dir)
	p, ok := again.Next()
	if !ok || string(p.Data) != "jpeg "+p.ID || p.Place != "Lisbon, Portugal" {
		t.Errorf("after a restart got %+v %v", p, ok)
	}
}

func TestChangingTheSelectionStartsOver(t *testing.T) {
	srv := &server{version: "v3.3.1", key: "k", assets: 5}
	l, s := library(t, schema.Photos{Server: srv.start(t), Key: "k"}, t.TempDir())
	l.step(context.Background())
	s.Favorites = true
	l.step(context.Background())
	if len(srv.searches) != 2 {
		t.Fatalf("searched %d times, want 2", len(srv.searches))
	}
	filter, _ := srv.searches[1]["filter"].(map[string]any)
	if filter["isFavorite"] == nil {
		t.Errorf("second search %v", filter)
	}
}

// googleAlbum fakes a public shared album with still photos named by id.
func googleAlbum(t *testing.T, ids ...string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var downloaded atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /share/{album}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>Trips - Google Photos</title></head>` +
			`<body><script>{"snAcKc":{request:["album",null,null,"auth"]}}</script></body></html>`))
	})
	mux.HandleFunc(
		"POST /u/0/_/PhotosUi/data/batchexecute",
		func(w http.ResponseWriter, r *http.Request) {
			items := make([]any, 0, len(ids))
			for _, id := range ids {
				items = append(items, []any{
					id,
					[]any{"http://" + r.Host + "/p/" + id + "=w4000-h2000", 4000.0, 2000.0},
					1600000000000.0,
					nil,
					nil,
					1600000001000.0,
					map[string]any{},
				})
			}
			inner, _ := json.Marshal([]any{nil, items, ""})
			line, _ := json.Marshal([]any{[]any{"wrb.fr", "snAcKc", string(inner)}})
			_, _ = fmt.Fprintf(w, ")]}'\n\n%s\n", line)
		},
	)
	mux.HandleFunc("GET /p/{id}", func(w http.ResponseWriter, r *http.Request) {
		downloaded.Add(1)
		id, _, _ := strings.Cut(r.PathValue("id"), "=")
		_, _ = w.Write([]byte("jpeg " + id))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &downloaded
}

func TestGoogleAlbumsFillTheDeckAndSaySo(t *testing.T) {
	srv, downloaded := googleAlbum(t, "one", "two", "three")
	l, _ := library(
		t,
		schema.Photos{Google: srv.URL + "/share/album"},
		t.TempDir(),
	)
	l.step(context.Background())

	st := l.State()
	if st.Status != Ready || st.Source != "google" {
		t.Fatalf("state %+v", st)
	}
	if downloaded.Load() != 3 {
		t.Errorf("downloaded %d pictures, want 3", downloaded.Load())
	}
	want := map[string]bool{"one": true, "two": true, "three": true}
	for range 3 {
		p, ok := l.Next()
		if !ok || !want[p.ID] || string(p.Data) != "jpeg "+p.ID {
			t.Fatalf("picture %+v %v", p, ok)
		}
		delete(want, p.ID)
		if p.Taken.Year() != 2020 {
			t.Errorf("taken %v", p.Taken)
		}
	}
}

func TestABrokenGoogleLinkSaysSo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<title>Sign in</title>"))
	}))
	t.Cleanup(srv.Close)
	l, _ := library(t, schema.Photos{Google: srv.URL + "/share/x"}, t.TempDir())
	l.step(context.Background())
	if st := l.State(); st.Status != Link || st.Source != "google" {
		t.Fatalf("state %+v", st)
	}
}

func TestGoogleAlbumsWinOverImmich(t *testing.T) {
	srv, _ := googleAlbum(t, "one")
	l, _ := library(
		t,
		schema.Photos{Server: "http://127.0.0.1:1", Key: "k", Google: srv.URL + "/share/album"},
		t.TempDir(),
	)
	l.step(context.Background())
	if st := l.State(); st.Status != Ready || st.Source != "google" {
		t.Fatalf("state %+v", st)
	}
}
