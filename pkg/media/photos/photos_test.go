package photos

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type server struct {
	version string
	key     string
	assets  int
	memory  string
	perms   []string
	deny    string

	mu       sync.Mutex
	down     bool
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
	mux.HandleFunc("GET /api/api-keys/me", auth(func(w http.ResponseWriter, r *http.Request) {
		if s.perms == nil {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"permissions": s.perms})
	}))
	mux.HandleFunc("GET /api/server/about", auth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"version":%q}`, s.version)
	}))
	mux.HandleFunc("GET /api/albums", auth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"album-1","albumName":"Kitchen","assetCount":3}]`))
	}))
	mux.HandleFunc("GET /api/memories", auth(func(w http.ResponseWriter, _ *http.Request) {
		if s.deny != "" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprintf(
				w,
				`{"message":"Missing required permission: %s","statusCode":403}`,
				s.deny,
			)
			return
		}
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		down := s.down
		s.mu.Unlock()
		if down {
			panic(http.ErrAbortHandler)
		}
		mux.ServeHTTP(w, r)
	}))
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

func on(i schema.Immich) schema.Photos {
	i.Enabled = true
	return schema.Photos{Immich: i}
}

func TestNothingHappensUntilImmichIsEnabledWithAServer(t *testing.T) {
	for _, c := range []struct {
		name string
		cfg  schema.Photos
		want Status
	}{
		{"off", schema.Photos{}, Off},
		{"no server", on(schema.Immich{}), Unset},
	} {
		t.Run(c.name, func(t *testing.T) {
			l, _ := library(t, c.cfg, t.TempDir())
			l.step(context.Background())
			if got := l.State("immich").Status; got != c.want {
				t.Errorf("status %v, want %v", got, c.want)
			}
			if _, ok := l.Next(); ok {
				t.Error("a picture without a source")
			}
		})
	}
}

func TestTurningImmichOffDropsItsPictures(t *testing.T) {
	srv := &server{version: "v3.3.1", key: "k", assets: 5}
	l, s := library(t, on(schema.Immich{Server: srv.start(t), Key: "k"}), t.TempDir())
	l.step(context.Background())
	if _, ok := l.Next(); !ok {
		t.Fatal("no picture while on")
	}
	s.Immich.Enabled = false
	l.step(context.Background())
	if p, ok := l.Next(); ok {
		t.Errorf("still showing %+v after turning Immich off", p)
	}
	if got := l.State("immich").Status; got != Off {
		t.Errorf("status %v, want Off", got)
	}
}

func TestItKeepsPicturesAheadWithMemoriesFirst(t *testing.T) {
	srv := &server{version: "v3.3.1", key: "k", assets: 10, memory: "mem"}
	l, _ := library(
		t,
		on(schema.Immich{Server: srv.start(t), Key: "k", Memories: true}),
		t.TempDir(),
	)

	l.step(context.Background())
	if st := l.State("immich"); st.Status != Ready || st.Version != "v3.3.1" {
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
	l, _ := library(t, on(schema.Immich{Server: srv.start(t), Key: "k"}), t.TempDir())
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
	l, s := library(t, on(schema.Immich{Server: url, Key: "k", Albums: "kitchen"}), t.TempDir())
	l.step(context.Background())
	filter, _ := srv.searches[0]["filter"].(map[string]any)
	albums, _ := filter["albumIds"].(map[string]any)
	if fmt.Sprint(albums["any"]) != "[album-1]" {
		t.Errorf("albums %v", albums)
	}

	s.Immich.Albums = "Trips"
	l.step(context.Background())
	if st := l.State("immich"); st.Status != Unknown || st.Missing != "Trips" {
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
			l, _ := library(t, on(schema.Immich{Server: srv.start(t), Key: c.key}), t.TempDir())
			l.step(context.Background())
			if got := l.State("immich").Status; got != c.want {
				t.Errorf("status %v, want %v", got, c.want)
			}
		})
	}
	l, _ := library(t, on(schema.Immich{Server: "http://127.0.0.1:1", Key: "k"}), t.TempDir())
	l.step(context.Background())
	if got := l.State("immich").Status; got != Unreachable {
		t.Errorf("status %v, want Unreachable", got)
	}
}

func TestASourceRecoversAfterTheServerComesBack(t *testing.T) {
	srv := &server{version: "v3.3.1", key: "k", assets: 5, down: true}
	s := &settings{on(schema.Immich{Server: srv.start(t), Key: "k"})}
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	l := New(Options{Settings: s, Dir: t.TempDir(), Keep: 4, Now: func() time.Time { return now }})
	l.step(context.Background())
	if got := l.State("immich").Status; got != Unreachable {
		t.Fatalf("while down: %v, want Unreachable", got)
	}
	srv.mu.Lock()
	srv.down = false
	srv.mu.Unlock()
	now = now.Add(retryAfter)
	l.step(context.Background())
	if got := l.State("immich").Status; got != Ready {
		t.Errorf("after the server came back: %v, want Ready", got)
	}
	if _, ok := l.Next(); !ok {
		t.Error("no photo fetched after the server came back")
	}
}

func TestCachedPicturesSurviveARestartWithoutTheServer(t *testing.T) {
	dir := t.TempDir()
	srv := &server{version: "v3.3.1", key: "k", assets: 5}
	l, _ := library(t, on(schema.Immich{Server: srv.start(t), Key: "k"}), dir)
	l.step(context.Background())

	again, _ := library(t, on(schema.Immich{Server: "http://127.0.0.1:1", Key: "k"}), dir)
	p, ok := again.Next()
	if !ok || string(p.Data) != "jpeg "+p.ID || p.Place != "Lisbon, Portugal" {
		t.Errorf("after a restart got %+v %v", p, ok)
	}
}

func TestChangingTheSelectionStartsOver(t *testing.T) {
	srv := &server{version: "v3.3.1", key: "k", assets: 5}
	l, s := library(t, on(schema.Immich{Server: srv.start(t), Key: "k"}), t.TempDir())
	l.step(context.Background())
	s.Immich.Favorites = true
	l.step(context.Background())
	if len(srv.searches) != 2 {
		t.Fatalf("searched %d times, want 2", len(srv.searches))
	}
	filter, _ := srv.searches[1]["filter"].(map[string]any)
	if filter["isFavorite"] == nil {
		t.Errorf("second search %v", filter)
	}
}

func TestStrayFilesInThePhotoFolderAreRemoved(t *testing.T) {
	dir := t.TempDir()
	stray := filepath.Join(dir, "old-name.jpg")
	if err := os.WriteFile(stray, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	library(t, schema.Photos{}, dir)
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Errorf("stray file still there: %v", err)
	}
}

func TestMissingPermissionsAreNamed(t *testing.T) {
	for _, c := range []struct {
		name  string
		perms []string
		deny  string
		cfg   schema.Immich
		want  Status
		says  string
	}{
		{
			name:  "every gap at once",
			perms: []string{"server.about", "asset.read", "asset.view"},
			cfg:   schema.Immich{Memories: true, Albums: "Kitchen"},
			want:  NoPermission,
			says:  "memory.read, album.read",
		},
		{
			name:  "all covers everything",
			perms: []string{"all"},
			cfg:   schema.Immich{Memories: true, Albums: "Kitchen"},
			want:  Ready,
		},
		{
			name: "a refused call names its permission",
			deny: "memory.read",
			cfg:  schema.Immich{Memories: true},
			want: NoPermission,
			says: "memory.read",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := &server{
				version: "v3.3.1",
				key:     "k",
				assets:  2,
				memory:  "mem",
				perms:   c.perms,
				deny:    c.deny,
			}
			c.cfg.Server, c.cfg.Key = srv.start(t), "k"
			l, _ := library(t, on(c.cfg), t.TempDir())
			l.step(context.Background())
			st := l.State("immich")
			if st.Status != c.want || st.Missing != c.says {
				t.Errorf("state %+v, want %v %q", st, c.want, c.says)
			}
		})
	}
}
