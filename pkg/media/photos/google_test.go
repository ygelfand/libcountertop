package photos

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func albumPage(base string, prefix string, n int) string {
	var photos []string
	for i := range n {
		photos = append(photos, fmt.Sprintf(
			`["%s%d",["%s/img/%s%d",4032,3024,null,null,null,null,null,null,[]],1700000000000,"x",-18000000,1700000100000]`,
			prefix,
			i,
			base,
			prefix,
			i,
		))
	}
	return `<html><script nonce="a">AF_initDataCallback({key: 'ds:0', hash: '1', data:[null,null], sideChannel: {}});</script>` +
		`<script nonce="a">AF_initDataCallback({key: 'ds:1', hash: '2', data:[null,[` +
		strings.Join(
			photos,
			",",
		) + `],"next"], sideChannel: {}});</script></html>`
}

type googleServer struct {
	srv   *httptest.Server
	sizes []string
}

func newGoogleServer(t *testing.T) *googleServer {
	t.Helper()
	g := &googleServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /share/{album}", func(w http.ResponseWriter, r *http.Request) {
		switch album := r.PathValue("album"); album {
		case "gone":
			http.NotFound(w, r)
		default:
			_, _ = w.Write([]byte(albumPage(g.srv.URL, album, 5)))
		}
	})
	mux.HandleFunc("GET /img/{rest}", func(w http.ResponseWriter, r *http.Request) {
		rest := r.PathValue("rest")
		if i := strings.Index(rest, "="); i >= 0 {
			g.sizes = append(g.sizes, rest[i:])
		}
		_, _ = w.Write([]byte("jpeg " + rest))
	})
	g.srv = httptest.NewTLSServer(mux)
	t.Cleanup(g.srv.Close)
	return g
}

func (g *googleServer) source(most int) googleSource {
	return googleSource{client: g.srv.Client(), most: func() int { return most }}
}

func googleCfg(albums ...schema.GoogleAlbum) schema.Photos {
	var cfg schema.Photos
	cfg.Google.Enabled = true
	copy(cfg.Google.Albums[:], albums)
	return cfg
}

func TestTheAlbumPageYieldsItsPhotos(t *testing.T) {
	g := newGoogleServer(t)
	photos, err := parseGoogleAlbum([]byte(albumPage(g.srv.URL, "a", 3)))
	if err != nil || len(photos) != 3 {
		t.Fatalf("got %v %v", photos, err)
	}
	if photos[0].ID != "a0" || !strings.HasSuffix(photos[0].URL, "/img/a0") ||
		photos[0].Taken == "" {
		t.Errorf("first photo %+v", photos[0])
	}
	if _, err := parseGoogleAlbum([]byte("<html>sign in</html>")); err == nil {
		t.Error("a page without a photo list parsed")
	}
}

func TestWeightsDecideHowOftenEachAlbumIsPicked(t *testing.T) {
	s := &googleSession{albums: []googleAlbum{
		{weight: 9, photos: manyFound("a", 400)},
		{weight: 1, photos: manyFound("b", 400)},
		{weight: 0, photos: manyFound("c", 400)},
	}}
	from := map[byte]int{}
	for _, f := range s.pick(200) {
		from[f.ID[0]]++
	}
	if from['c'] != 0 || from['a'] < 4*from['b'] {
		t.Errorf("picks by album %v", from)
	}
}

func manyFound(prefix string, n int) []Found {
	out := make([]Found, n)
	for i := range out {
		out[i] = Found{ID: fmt.Sprintf("%s%d", prefix, i)}
	}
	return out
}

func TestPhotosComeSizedToTheScreen(t *testing.T) {
	g := newGoogleServer(t)
	cfg := googleCfg(schema.GoogleAlbum{URL: g.srv.URL + "/share/a", Weight: 5})
	sess, _, err := g.source(1280).Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, back, err := sess.Draw(context.Background(), time.Now(), true)
	if err != nil || len(back) == 0 {
		t.Fatalf("drew %v %v", back, err)
	}
	data, err := sess.Fetch(context.Background(), back[0].ID)
	if err != nil || !strings.HasPrefix(string(data), "jpeg ") {
		t.Fatalf("fetched %q %v", data, err)
	}
	if len(g.sizes) != 1 || g.sizes[0] != "=w1280-h1280" {
		t.Errorf("sizes asked %v", g.sizes)
	}
}

func TestABrokenAlbumDoesNotStopTheOthers(t *testing.T) {
	g := newGoogleServer(t)
	cfg := googleCfg(
		schema.GoogleAlbum{URL: g.srv.URL + "/share/gone", Weight: 5},
		schema.GoogleAlbum{URL: g.srv.URL + "/share/b", Weight: 5},
	)
	sess, _, err := g.source(800).Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, back, _ := sess.Draw(context.Background(), time.Now(), true)
	for _, f := range back {
		if !strings.HasPrefix(f.ID, "b") {
			t.Fatalf("picked %q from the missing album", f.ID)
		}
	}
	only := googleCfg(schema.GoogleAlbum{URL: g.srv.URL + "/share/gone", Weight: 5})
	if _, _, err := g.source(800).Open(context.Background(), only); err == nil {
		t.Error("an album that is gone opened")
	}
}

func TestGoogleStatusFollowsTheSettings(t *testing.T) {
	src := Google(nil)
	for _, c := range []struct {
		name string
		cfg  schema.Photos
		want Status
	}{
		{"off", schema.Photos{}, Off},
		{"no albums", googleCfg(), Unset},
		{"weight zero", googleCfg(schema.GoogleAlbum{URL: "https://x", Weight: 0}), Unset},
		{"ready", googleCfg(schema.GoogleAlbum{URL: "https://x", Weight: 3}), Connecting},
	} {
		if got := src.Status(c.cfg); got != c.want {
			t.Errorf("%s: status %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTheNextPageOfAnAlbumParses(t *testing.T) {
	inner := `[null,[["p300",["https://lh3.example/p300",10,10],1700000000000]],"",["album"]]`
	payload, _ := json.Marshal(inner)
	body := ")]}'\n\n123\n[[\"wrb.fr\",\"snAcKc\"," + string(
		payload,
	) + ",null,null,null,\"generic\"]]"
	data, err := batchData([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	photos, token, id := parseGoogleData(data)
	if len(photos) != 1 || photos[0].ID != "p300" || token != "" || id != "album" {
		t.Errorf("photos %v token %q id %q", photos, token, id)
	}
}
