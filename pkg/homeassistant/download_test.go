package homeassistant

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ygelfand/go-esphome-device/api"
)

type upstream struct {
	body     []byte
	status   int
	sizeless bool
	flaky    sync.Map
	inFlight atomic.Int32
	most     atomic.Int32
	calls    atomic.Int32
}

func (u *upstream) reply(req *api.HomeassistantActionRequest) *api.HomeassistantActionResponse {
	data := map[string]string{}
	for _, p := range req.GetData() {
		data[p.GetKey()] = p.GetValue()
	}
	off, _ := strconv.ParseInt(data["offset"], 10, 64)
	n, _ := strconv.ParseInt(data["length"], 10, 64)
	if _, fail := u.flaky.LoadAndDelete(off); fail {
		return &api.HomeassistantActionResponse{ErrorMessage: "upstream timed out"}
	}
	answer := map[string]any{"status": http.StatusPartialContent, "total": len(u.body)}
	if u.sizeless {
		delete(answer, "total")
	}
	switch {
	case u.status != 0:
		answer = map[string]any{"status": u.status}
	case off >= int64(len(u.body)):
		answer["status"] = http.StatusRequestedRangeNotSatisfiable
	default:
		answer["data"] = base64.StdEncoding.EncodeToString(
			u.body[off:min(off+n, int64(len(u.body)))],
		)
	}
	inner, _ := json.Marshal(answer)
	outer, _ := json.Marshal(map[string]string{"response": string(inner)})
	return &api.HomeassistantActionResponse{Success: true, ResponseData: outer}
}

func serving(t *testing.T, u *upstream) *http.Client {
	t.Helper()
	h, f := connected(nil)
	f.answer = func(req *api.HomeassistantActionRequest) *api.HomeassistantActionResponse {
		if req.GetService() != fetchAction {
			t.Errorf("called %s", req.GetService())
			return nil
		}
		u.calls.Add(1)
		now := u.inFlight.Add(1)
		for {
			most := u.most.Load()
			if now <= most || u.most.CompareAndSwap(most, now) {
				break
			}
		}
		resp := u.reply(req)
		resp.CallId = req.GetCallId()
		go func() {
			time.Sleep(5 * time.Millisecond)
			u.inFlight.Add(-1)
			h.answer(f, resp)
		}()
		return nil
	}
	h.links[f] = &link{access: Allowed, integration: IntegrationReady, protocol: FetchProtocol}
	return &http.Client{Transport: h.Downloads()}
}

func blob(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}

func TestDownloadsAWholeFileInPipelinedPieces(t *testing.T) {
	u := &upstream{body: blob(5*chunk + 123)}
	resp, err := serving(t, u).Get("https://github.com/o/r/releases/download/v1/a.tar.xz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || resp.ContentLength != int64(len(u.body)) {
		t.Errorf(
			"status %d length %d, want 200 and %d",
			resp.StatusCode,
			resp.ContentLength,
			len(u.body),
		)
	}
	if !bytes.Equal(got, u.body) {
		t.Fatalf("got %d bytes that differ from the %d sent", len(got), len(u.body))
	}
	if c := u.calls.Load(); c != 6 {
		t.Errorf("made %d calls, want 6", c)
	}
	if m := u.most.Load(); m < 2 {
		t.Errorf("at most %d calls were in flight, want them pipelined", m)
	}
}

func TestDownloadsAFileOfUnknownSizeToItsEnd(t *testing.T) {
	for _, size := range []int{3 * chunk, 3*chunk + 17} {
		u := &upstream{body: blob(size), sizeless: true}
		resp, err := serving(t, u).Get("https://api.github.com/repos/o/r/releases")
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("%d bytes: %v", size, err)
		}
		if !bytes.Equal(got, u.body) {
			t.Errorf("got %d bytes, want the %d sent", len(got), size)
		}
	}
}

func TestDownloadsRetryAFailedPiece(t *testing.T) {
	pause = time.Millisecond
	t.Cleanup(func() { pause = time.Second })
	u := &upstream{body: blob(4 * chunk)}
	u.flaky.Store(int64(2*chunk), true)
	resp, err := serving(t, u).Get("https://github.com/o/r/releases/download/v1/a.tar.xz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, u.body) {
		t.Errorf("got %d bytes, want the %d sent", len(got), len(u.body))
	}
	if c := u.calls.Load(); c != 5 {
		t.Errorf("made %d calls, want 5 with one retry", c)
	}
}

func TestDownloadsARange(t *testing.T) {
	u := &upstream{body: blob(3 * chunk)}
	req, _ := http.NewRequest(http.MethodGet, "https://d.example/f", nil)
	req.Header.Set("Range", "bytes=100-"+strconv.Itoa(chunk+99))
	resp, err := serving(t, u).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	want := "bytes 100-" + strconv.Itoa(chunk+99) + "/" + strconv.Itoa(3*chunk)
	if resp.StatusCode != http.StatusPartialContent || resp.Header.Get("Content-Range") != want {
		t.Errorf(
			"status %d range %q, want 206 %q",
			resp.StatusCode,
			resp.Header.Get("Content-Range"),
			want,
		)
	}
	if !bytes.Equal(got, u.body[100:chunk+100]) {
		t.Errorf("got %d bytes, want the %d asked for", len(got), chunk)
	}
}

func TestDownloadsPassAFailureThrough(t *testing.T) {
	u := &upstream{status: http.StatusNotFound}
	resp, err := serving(t, u).Get("https://github.com/o/r/releases/download/v1/missing")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", resp.StatusCode)
	}
}

func TestDownloadsNeedAnIntegrationThatFetches(t *testing.T) {
	h, f := connected(nil)
	h.links[f] = &link{access: Allowed, integration: IntegrationReady, protocol: DashboardProtocol}
	_, err := (&http.Client{Transport: h.Downloads()}).Get("https://github.com/o/r")
	if err == nil {
		t.Fatal("downloaded through an integration without fetch")
	}
}
