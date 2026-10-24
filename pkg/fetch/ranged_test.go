package fetch

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func served(t *testing.T, body []byte) (string, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	}))
	t.Cleanup(s.Close)
	return s.URL, &requests
}

func content(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}

func TestAStreamReadsBackWhole(t *testing.T) {
	for _, n := range []int{0, 1, 99, 100, 101, 1000} {
		body := content(n)
		url, _ := served(t, body)
		r := NewRanged(t.Context(), http.DefaultClient, url, 0, Chunks{Size: 100, Ahead: 3})

		got, err := io.ReadAll(r)
		if err != nil {
			t.Errorf("%d bytes: %v", n, err)
		}
		if !bytes.Equal(got, body) {
			t.Errorf("%d bytes read back as %d", n, len(got))
		}
		if n > 0 && r.Size() != int64(n) {
			t.Errorf("%d bytes: size reads %d", n, r.Size())
		}
		_ = r.Close()
	}
}

func TestNoMoreThanTheReadAheadIsFetched(t *testing.T) {
	url, requests := served(t, content(10_000))
	r := NewRanged(t.Context(), http.DefaultClient, url, 0, Chunks{Size: 100, Ahead: 3})
	defer r.Close()

	time.Sleep(200 * time.Millisecond)
	if got := requests.Load(); got > 4 {
		t.Errorf(
			"%d chunks fetched with nothing read, want at most the read-ahead and one in hand",
			got,
		)
	}
}

func TestSeekStartsAgainFromTheOffset(t *testing.T) {
	body := content(1000)
	url, _ := served(t, body)
	r := NewRanged(context.Background(), http.DefaultClient, url, 0, Chunks{Size: 100, Ahead: 2})
	defer r.Close()

	head := make([]byte, 50)
	if _, err := io.ReadFull(r, head); err != nil {
		t.Fatal(err)
	}

	r.From(730)
	rest, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rest, body[730:]) {
		t.Errorf("after seeking to 730, read %d bytes that do not match", len(rest))
	}
}

func TestSeekReadsWhereverItLands(t *testing.T) {
	body := content(1000)
	url, _ := served(t, body)
	r := NewRanged(context.Background(), http.DefaultClient, url, 0, Chunks{Size: 100, Ahead: 2})
	defer r.Close()

	for _, c := range []struct {
		offset int64
		whence int
		want   int64
	}{
		{120, io.SeekStart, 120},
		{30, io.SeekCurrent, 160},
		{900, io.SeekStart, 900},
		{-50, io.SeekEnd, 950},
		{10, io.SeekStart, 10},
	} {
		at, err := r.Seek(c.offset, c.whence)
		if err != nil || at != c.want {
			t.Fatalf("seek %d from %d: at %d, %v; want %d", c.offset, c.whence, at, err, c.want)
		}
		got := make([]byte, 10)
		if _, err := io.ReadFull(r, got); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, body[c.want:c.want+10]) {
			t.Errorf("after seeking to %d, read bytes that do not match", c.want)
		}
	}
}

func TestARefusedChunkIsTriedAgainAtAFreshURL(t *testing.T) {
	body := content(300)
	var refused atomic.Bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fresh") == "" &&
			strings.HasPrefix(r.Header.Get("Range"), "bytes=100-") &&
			refused.CompareAndSwap(false, true) {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	}))
	defer s.Close()

	var renewed atomic.Int32
	r := NewRanged(t.Context(), http.DefaultClient, s.URL, 0, Chunks{
		Size: 100, Ahead: 1,
		Renew: func(context.Context) (string, error) {
			renewed.Add(1)
			return s.URL + "?fresh=1", nil
		},
	})
	defer r.Close()

	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("read back %d bytes that do not match", len(got))
	}
	if renewed.Load() != 1 {
		t.Errorf("renewed %d times, want once", renewed.Load())
	}
}

func TestContentRangeTotal(t *testing.T) {
	for h, want := range map[string]int64{"bytes 0-99/1000": 1000, "bytes 0-99/*": 0, "": 0} {
		if got := total(h); got != want {
			t.Errorf("%q: %d, want %d", h, got, want)
		}
	}
}

func TestAPacedStreamStaysWithinItsLead(t *testing.T) {
	body := content(1000)
	start := time.Now()
	var mu sync.Mutex
	asked := map[string]time.Duration{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked[r.Header.Get("Range")] = time.Since(start)
		mu.Unlock()
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	}))
	t.Cleanup(s.Close)

	r := NewRanged(
		t.Context(),
		http.DefaultClient,
		s.URL,
		0,
		Chunks{Size: 100, Ahead: 10, Rate: 1000, Lead: 200 * time.Millisecond},
	)
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("read %d bytes, %v", len(got), err)
	}
	mu.Lock()
	defer mu.Unlock()
	if at := asked["bytes=200-299"]; at > 100*time.Millisecond {
		t.Errorf("a chunk inside the lead waited %v", at)
	}
	if at := asked["bytes=600-699"]; at < 350*time.Millisecond {
		t.Errorf("a chunk 0.6s in was asked for at %v, before 0.4s", at)
	}
}

func TestARefusedChunkPartWayIsTriedAgain(t *testing.T) {
	body := content(500)
	var refusals atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "bytes=200-299" && refusals.Add(1) <= 2 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	}))
	t.Cleanup(s.Close)

	r := NewRanged(
		t.Context(),
		http.DefaultClient,
		s.URL,
		0,
		Chunks{Size: 100, Ahead: 2, Rate: 1 << 30, Retry: 10 * time.Millisecond},
	)
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("read %d bytes, %v", len(got), err)
	}
	if n := refusals.Load(); n != 3 {
		t.Errorf("the refused chunk was asked for %d times, want 3", n)
	}
}
