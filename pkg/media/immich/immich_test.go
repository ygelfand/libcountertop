package immich

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func fake(t *testing.T, seen *map[string]any) *Client {
	t.Helper()
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("x-api-key") != "k3y" {
				http.Error(w, `{"message":"Invalid API key"}`, http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /api/server/about", auth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"v3.3.1","licensed":false,"versionUrl":"x"}`))
	}))
	mux.HandleFunc("GET /api/albums", auth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"a1","albumName":"Kitchen","assetCount":3}]`))
	}))
	mux.HandleFunc("GET /api/tags", auth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"t1","name":"display","value":"home/display"}]`))
	}))
	mux.HandleFunc("GET /api/people", auth(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		_, _ = fmt.Fprintf(
			w,
			`{"people":[{"id":"p%s","name":"P%s"}],"total":2,"hidden":0,"hasNextPage":%t}`,
			page,
			page,
			page == "1",
		)
	}))
	mux.HandleFunc("GET /api/memories", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("for") != "2026-10-14" || r.URL.Query().Get("type") != "on_this_day" {
			http.Error(w, "query", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`[{"id":"m1","type":"on_this_day","data":{"year":2019},` +
			`"assets":[{"id":"x1","type":"IMAGE"}]}]`))
	}))
	mux.HandleFunc("POST /api/search/random", auth(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(seen)
		_, _ = w.Write([]byte(`[{"id":"i1","type":"IMAGE","width":4000,"height":3000,` +
			`"localDateTime":"2019-10-14T09:00:00.000Z","isFavorite":true,` +
			`"exifInfo":{"dateTimeOriginal":"2019-10-14T13:00:00.000Z","city":"Lisbon","country":"Portugal"},` +
			`"people":[{"id":"p1","name":"Ana"}]}]`))
	}))
	mux.HandleFunc(
		"GET /api/assets/{id}/thumbnail",
		auth(func(w http.ResponseWriter, r *http.Request) {
			size := r.URL.Query().Get("size")
			if size != Preview && size != Fullsize {
				http.Error(w, "size", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(size + " of " + r.PathValue("id")))
		}),
	)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return New(srv.URL+"/", "k3y")
}

func TestRandomAsksWithTheV3Filter(t *testing.T) {
	var seen map[string]any
	c := fake(t, &seen)
	got, err := c.Random(context.Background(), Selection{
		Albums:    []string{"a1", "a2"},
		People:    []string{"p1"},
		Favorites: true,
	}, 5000)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"filter": map[string]any{
			"type":       map[string]any{"eq": "IMAGE"},
			"visibility": map[string]any{"eq": "timeline"},
			"albumIds":   map[string]any{"any": []any{"a1", "a2"}},
			"personIds":  map[string]any{"any": []any{"p1"}},
			"isFavorite": map[string]any{"eq": true},
		},
		"size":       float64(1000),
		"withExif":   true,
		"withPeople": true,
	}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("search body\n got %v\nwant %v", seen, want)
	}
	if len(got) != 1 || got[0].Exif == nil || got[0].Exif.City != "Lisbon" ||
		got[0].Width != 4000 || len(got[0].People) != 1 || !got[0].Favorite {
		t.Errorf("asset %+v", got)
	}
}

func TestAnEmptySelectionAsksForEveryImage(t *testing.T) {
	var seen map[string]any
	c := fake(t, &seen)
	if _, err := c.Random(context.Background(), Selection{}, 0); err != nil {
		t.Fatal(err)
	}
	filter, _ := seen["filter"].(map[string]any)
	for _, key := range []string{"albumIds", "tagIds", "personIds", "isFavorite"} {
		if _, ok := filter[key]; ok {
			t.Errorf("%s sent: %v", key, filter)
		}
	}
	if seen["size"] != float64(1) {
		t.Errorf("size %v", seen["size"])
	}
}

func TestListsAndPages(t *testing.T) {
	var seen map[string]any
	c := fake(t, &seen)
	ctx := context.Background()

	people, err := c.People(ctx)
	if err != nil || len(people) != 2 || people[1].ID != "p2" {
		t.Errorf("people %+v %v", people, err)
	}
	albums, err := c.Albums(ctx)
	if err != nil || len(albums) != 1 || albums[0].Assets != 3 {
		t.Errorf("albums %+v %v", albums, err)
	}
	tags, err := c.Tags(ctx)
	if err != nil || len(tags) != 1 || tags[0].Value != "home/display" {
		t.Errorf("tags %+v %v", tags, err)
	}
	memories, err := c.Memories(ctx, time.Date(2026, 10, 14, 23, 0, 0, 0, time.UTC))
	if err != nil || len(memories) != 1 || memories[0].Data.Year != 2019 ||
		len(memories[0].Assets) != 1 {
		t.Errorf("memories %+v %v", memories, err)
	}
	about, err := c.About(ctx)
	if err != nil || !Supported(about.Version) {
		t.Errorf("about %+v %v", about, err)
	}
	for _, size := range []string{Preview, Fullsize} {
		b, err := c.Picture(ctx, "i1", size)
		if err != nil || string(b) != size+" of i1" {
			t.Errorf("%s %q %v", size, b, err)
		}
	}
}

func TestSupportedVersions(t *testing.T) {
	for v, want := range map[string]bool{
		"v3.3.1": true, "v3.2.0": true, "3.2.0": true, "v4.0.0": true,
		"v3.1.9": false, "v1.140.0": false, "": false, "dev": false,
	} {
		if got := Supported(v); got != want {
			t.Errorf("Supported(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestFailuresSayWhat(t *testing.T) {
	var seen map[string]any
	c := fake(t, &seen)
	ctx := context.Background()
	c.Key = "wrong"
	if _, err := c.Albums(ctx); !errors.Is(err, ErrKey) {
		t.Errorf("a wrong key gave %v", err)
	}
	if _, err := c.Picture(ctx, "i1", Preview); !errors.Is(err, ErrKey) {
		t.Errorf("a wrong key on a preview gave %v", err)
	}
	if _, err := New("", "k").Albums(ctx); !errors.Is(err, ErrNoServer) {
		t.Errorf("no server gave %v", err)
	}
}
