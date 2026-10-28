package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/ygelfand/libcountertop/pkg/display/surface"
	"github.com/ygelfand/libcountertop/pkg/input/touch"
)

func TestDesktopRestrictsRequests(t *testing.T) {
	for _, test := range []struct {
		name, remote, origin string
		want                 int
	}{
		{"public address", "8.8.8.8:1234", "", http.StatusForbidden},
		{"foreign origin", "192.168.1.2:1234", "https://other.example", http.StatusForbidden},
		{"insecure origin", "192.168.1.2:1234", "http://panel", http.StatusForbidden},
		{"malformed offer", "192.168.1.2:1234", "https://panel", http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "https://panel/", bytes.NewBufferString("{}"))
			r.RemoteAddr = test.remote
			r.Header.Set("Origin", test.origin)
			w := httptest.NewRecorder()
			(&Server{}).ServeHTTP(w, r)
			if w.Code != test.want {
				t.Fatalf("status = %d, want %d", w.Code, test.want)
			}
		})
	}
}

func TestHeartbeatDoesNotExtendIdleControl(t *testing.T) {
	s := &Server{size: func() (int, int) { return 800, 480 }, deliver: func(touch.Contact) {}}
	sess, err := s.create(800, 480)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sess.cancel)
	sess.input.last = time.Now().Add(-idleTimeout)
	s.current = sess
	r := httptest.NewRequest(http.MethodPost, "https://panel/"+sess.id, nil)
	r.RemoteAddr = "192.168.1.2:1234"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("heartbeat = %d", w.Code)
	}
	go sess.watch()
	select {
	case <-sess.ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("heartbeat kept idle controller alive")
	}
}

type blockedCapture struct {
	closed chan struct{}
	once   sync.Once
}

func (c *blockedCapture) Read() (surface.Encoded, error) {
	<-c.closed
	return surface.Encoded{}, io.EOF
}
func (*blockedCapture) Keyframe() error { return nil }
func (c *blockedCapture) Close() error  { c.once.Do(func() { close(c.closed) }); return nil }

func TestDesktopReleasesTouchAndCaptureOnDisconnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	contacts := make(chan touch.Contact, 4)
	opened := make(chan struct{})
	stream := &blockedCapture{closed: make(chan struct{})}
	s := &Server{
		size:    func() (int, int) { return 800, 480 },
		open:    func() (capture, error) { close(opened); return stream, nil },
		deliver: func(c touch.Contact) { contacts <- c },
	}
	settings := webrtc.SettingEngine{}
	settings.SetIncludeLoopbackCandidate(true)
	pc, err := webrtc.NewAPI(webrtc.WithSettingEngine(settings)).
		NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	if _, err := pc.AddTransceiverFromKind(
		webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	); err != nil {
		t.Fatal(err)
	}
	dc, err := pc.CreateDataChannel("input", nil)
	if err != nil {
		t.Fatal(err)
	}
	dc.OnOpen(func() {
		if err := dc.SendText(`{"phase":"down","x":0.25,"y":0.5}`); err != nil {
			t.Error(err)
		}
	})
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gathered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	body, err := json.Marshal(pc.LocalDescription())
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "https://panel/", bytes.NewReader(body))
	r.RemoteAddr = "192.168.1.2:1234"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("offer = %d: %s", w.Code, w.Body.String())
	}
	var answer struct {
		ID     string                    `json:"id"`
		Answer webrtc.SessionDescription `json:"answer"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.mu.Lock()
		if s.current != nil {
			s.current.cancel()
		}
		s.mu.Unlock()
	})
	if err := pc.SetRemoteDescription(answer.Answer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-opened:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case c := <-contacts:
		if c.Phase != touch.Down || c.X != 200 || c.Y != 240 {
			t.Fatalf("down = %v", c)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	r = httptest.NewRequest(http.MethodDelete, "https://panel/"+answer.ID, nil)
	r.RemoteAddr = "192.168.1.2:1234"
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("disconnect = %d", w.Code)
	}
	select {
	case c := <-contacts:
		if c.Phase != touch.Up || c.X != 200 || c.Y != 240 {
			t.Fatalf("release = %v", c)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-stream.closed:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestDesktopNegotiatesOneControllerWithoutOpeningCapture(t *testing.T) {
	s := &Server{
		size:    func() (int, int) { return 800, 480 },
		open:    func() (capture, error) { t.Error("capture opened before ICE connected"); return nil, context.Canceled },
		deliver: func(touch.Contact) {},
	}
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	if _, err := pc.AddTransceiverFromKind(
		webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := pc.CreateDataChannel("input", nil); err != nil {
		t.Fatal(err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(offer)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "https://panel"+path, bytes.NewReader(body))
		r.RemoteAddr = "192.168.1.2:1234"
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w := request(http.MethodPost, "/")
	if w.Code != http.StatusOK {
		t.Fatalf("offer = %d: %s", w.Code, w.Body.String())
	}
	var answer struct {
		ID     string                    `json:"id"`
		Answer webrtc.SessionDescription `json:"answer"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.ID == "" || answer.Answer.Type != webrtc.SDPTypeAnswer {
		t.Fatalf("answer = %+v", answer)
	}
	t.Cleanup(func() {
		s.mu.Lock()
		if s.current != nil {
			s.current.cancel()
		}
		s.mu.Unlock()
	})
	if w := request(http.MethodPost, "/"); w.Code != http.StatusConflict {
		t.Fatalf("second controller = %d", w.Code)
	}
	if w := request(http.MethodDelete, "/wrong"); w.Code != http.StatusNotFound {
		t.Fatalf("wrong session = %d", w.Code)
	}
	if w := request(http.MethodPost, "/"+answer.ID); w.Code != http.StatusNoContent {
		t.Fatalf("heartbeat = %d", w.Code)
	}
	if w := request(http.MethodDelete, "/"+answer.ID); w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", w.Code)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		current := s.current
		s.mu.Unlock()
		if current == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("session not released")
}
