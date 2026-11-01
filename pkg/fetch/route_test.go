package fetch

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type canned string

func (c canned) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(string(c))),
		Request:    r,
	}, nil
}

func TestARouteTakesOnlyTheRequestsItClaims(t *testing.T) {
	t.Cleanup(func() { Through(nil) })
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("direct"))
	}))
	defer direct.Close()

	Through(func(r *http.Request) http.RoundTripper {
		if r.URL.Hostname() == "releases.example" {
			return canned("relayed")
		}
		return nil
	})
	read := func(url string) string {
		t.Helper()
		resp, err := Client(0).Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	if got := read("https://releases.example/a"); got != "relayed" {
		t.Errorf("claimed host got %q, want relayed", got)
	}
	if got := read(direct.URL); got != "direct" {
		t.Errorf("other host got %q, want direct", got)
	}

	Through(nil)
	if got := read(direct.URL); got != "direct" {
		t.Errorf("after clearing the route got %q, want direct", got)
	}
}
