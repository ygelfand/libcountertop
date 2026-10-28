package webreceiver

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ygelfand/libcountertop/pkg/media/cast"
	"github.com/ygelfand/libcountertop/pkg/media/playback"
	"github.com/ygelfand/libcountertop/pkg/media/urlplayer"
)

type sent struct {
	mu   sync.Mutex
	msgs []cast.Message
	ads  [][]string
}

func (s *sent) send(m cast.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, m)
}

func (s *sent) advertise(_, _ string, ns []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ads = append(s.ads, ns)
}

func (s *sent) wait(t *testing.T, n int) []cast.Message {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		if len(s.msgs) >= n {
			out := append([]cast.Message(nil), s.msgs...)
			s.mu.Unlock()
			return out
		}
		s.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waited for %d messages to senders", n)
	return nil
}

func page(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v2/ipc"
	c, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func read(t *testing.T, c *websocket.Conn) (frame, map[string]any) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	var f frame
	if err := c.ReadJSON(&f); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(f.Data), &body)
	return f, body
}

func TestThePageAndTheSendersTalkThroughTheBus(t *testing.T) {
	s := &sent{}
	r := New(
		cast.Env{Send: s.send, Advertise: s.advertise, Device: cast.Device{ID: "abc"}},
		Options{},
	)
	srv := httptest.NewServer(r.handler())
	defer srv.Close()

	app := &cast.Application{
		AppID:       "9AC194DC",
		SessionID:   "sess",
		TransportID: "transport-1",
		DisplayName: "Plex",
	}
	if _, err := r.Receive(app, cast.Message{
		Source: "sender-1", Destination: app.TransportID, Namespace: cast.NSConnection,
		Payload: `{"type":"CONNECT"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Receive(app, cast.Message{
		Source:      "sender-1",
		Destination: app.TransportID,
		Namespace:   "urn:x-cast:com.plexapp.plexcast",
		Payload:     `{"type":"hello"}`,
	}); err != nil {
		t.Fatal(err)
	}

	c := page(t, srv)
	ready, _ := json.Marshal(
		map[string]any{
			"type":             "ready",
			"activeNamespaces": []string{cast.NSMedia, "urn:x-cast:com.plexapp.plexcast"},
		},
	)
	if err := c.WriteJSON(
		frame{Namespace: systemNS, SenderID: "SystemSender", Data: string(ready)},
	); err != nil {
		t.Fatal(err)
	}

	f, body := read(t, c)
	if f.Namespace != systemNS || body["type"] != "ready" || body["sessionId"] != "sess" ||
		body["applicationId"] != "9AC194DC" {
		t.Fatalf("the platform answered the page's ready with %s %s", f.Namespace, f.Data)
	}
	f, body = read(t, c)
	if body["type"] != "senderconnected" || body["senderId"] != "sender-1" {
		t.Fatalf("the sender that connected before the page was told as %s", f.Data)
	}
	if f, _ = read(
		t,
		c,
	); f.Namespace != "urn:x-cast:com.plexapp.plexcast" ||
		f.SenderID != "sender-1" {
		t.Fatalf("the sender's first message reached the page as %s from %s", f.Data, f.SenderID)
	}
	s.mu.Lock()
	ads := s.ads
	s.mu.Unlock()
	if len(ads) != 1 || len(ads[0]) != 2 {
		t.Fatalf("the page's namespaces were advertised as %v", ads)
	}

	status := `{"type":"MEDIA_STATUS","requestId":0,"status":[{"mediaSessionId":7,"playerState":"PLAYING"}]}`
	if err := c.WriteJSON(
		frame{Namespace: cast.NSMedia, SenderID: "*:*", Data: status},
	); err != nil {
		t.Fatal(err)
	}
	out := s.wait(t, 1)
	if m := out[0]; m.Source != "transport-1" || m.Destination != cast.Broadcast ||
		m.Namespace != cast.NSMedia {
		t.Fatalf("the page's status went out as %+v", m)
	}

	if err := (remote{r}).Pause(); err != nil {
		t.Fatal(err)
	}
	f, body = read(t, c)
	if f.SenderID != injected || body["type"] != "PAUSE" || body["mediaSessionId"] != float64(7) {
		t.Fatalf("the panel's pause reached the page as %s from %s", f.Data, f.SenderID)
	}

	greeting, err := r.Receive(app, cast.Message{
		Source: "sender-2", Destination: app.TransportID, Namespace: cast.NSConnection,
		Payload: `{"type":"CONNECT"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(greeting) != 1 || greeting[0].Destination != "sender-2" ||
		greeting[0].Payload != `{"type":"MEDIA_STATUS","requestId":0,"status":[{"mediaSessionId":7,"playerState":"PLAYING"}]}` {
		t.Fatalf("a sender joining mid-session was greeted with %+v", greeting)
	}
}

func TestThePageAsksWhatThePlayerPlays(t *testing.T) {
	r := New(cast.Env{
		Output: nopOutput{},
		Video:  func() playback.Target { return playback.Target{Tallest: 720, Fastest: 30} },
	}, Options{})
	srv := httptest.NewServer(r.handler())
	defer srv.Close()
	for _, c := range []struct{ how, typ, want string }{
		{"", `video/mp4; codecs="avc1.64001f"; width=1280; height=720; framerate=30`, "true"},
		{"", `video/mp4; codecs="avc1.64001f"; width=1920; height=1080`, "false"},
		{"", `video/webm; codecs="vp9"`, "false"},
		{"", `audio/mp4; codecs="mp4a.40.2"`, "true"},
		{"", `video/mp4; codecs="avc1.4d401e"; width=99999`, "false"},
		{"", `video/mp4; codecs="avc1.4d401e"; framerate=9999`, "false"},
		{"", `video/mp4; codecs="hvc1.1.6.L93.B0"; width=1280; height=720`, "true"},
		{"", `video/mp4; codecs="hvc1.2.4.L93.B0"; width=1280; height=720`, "false"},
		{"&feed=1", `video/mp4; codecs="hvc1.1.6.L93.B0"; width=1280; height=720`, "true"},
		{"&sealed=1", `video/mp4; codecs="hvc1.1.6.L93.B0"; width=1280; height=720`, "false"},
		{"&sealed=1", `video/mp4; codecs="avc1.64001f"; width=1280; height=720`, "true"},
	} {
		typ, want := c.typ, c.want
		resp, err := http.Get(srv.URL + "/plays?type=" + url.QueryEscape(typ) + c.how)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(got) != want {
			t.Errorf("plays %q = %s, want %s", typ, got, want)
		}
		if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("plays %q does not let the page read the answer", typ)
		}
	}
}

func TestTheHooksCarryThisDevice(t *testing.T) {
	js := hooks(cast.Env{Name: "Kitchen", Device: cast.Device{ID: "abc"}}, 8012)
	if strings.Contains(js, "__VALUES__") || strings.Contains(js, "__PORT__") {
		t.Fatal("the hooks still hold a placeholder")
	}
	if !strings.Contains(js, `"receiver-device-id":"abc"`) ||
		!strings.Contains(js, "const port = 8012;") {
		t.Fatal("the hooks do not carry the device's identity and bus port")
	}
}

func TestThePageFeedsSegmentsItAppends(t *testing.T) {
	video, err := os.ReadFile("../../../urlplayer/testdata/feed-video.mp4")
	if err != nil {
		t.Fatal(err)
	}
	r := New(
		cast.Env{Output: quietOutput{}, Video: func() playback.Target { return playback.Target{} }},
		Options{},
	)
	srv := httptest.NewServer(r.handler())
	defer srv.Close()
	c, _, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(srv.URL, "http")+"/media",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, o := range []string{`{"op":"source","start":0}`, `{"op":"add","lane":1,"type":"video/mp4"}`} {
		if err := c.WriteMessage(websocket.TextMessage, []byte(o)); err != nil {
			t.Fatal(err)
		}
	}
	head := []byte(`{"op":"append","lane":1,"seq":7,"offset":0}`)
	msg := binary.BigEndian.AppendUint32(nil, uint32(len(head)))
	msg = append(append(msg, head...), video...)
	if err := c.WriteMessage(websocket.BinaryMessage, msg); err != nil {
		t.Fatal(err)
	}
	var done, meta report
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := c.ReadJSON(&done); err != nil {
		t.Fatal(err)
	}
	if done.Event != "done" || done.Lane != 1 || done.Seq != 7 || done.Message != "" ||
		len(done.Buffered) != 1 || done.Buffered[0][1] < 1.9 {
		t.Fatalf("the append was answered with %+v", done)
	}
	if err := c.ReadJSON(&meta); err != nil {
		t.Fatal(err)
	}
	if meta.Event != "meta" || meta.W != 64 || meta.H != 64 {
		t.Fatalf("the opened source was reported as %+v", meta)
	}
}

func TestTheScreenFollowsThePagesMediaStatus(t *testing.T) {
	r := New(
		cast.Env{Output: quietOutput{}, Video: func() playback.Target { return playback.Target{} }},
		Options{},
	)
	r.watch(`{"type":"MEDIA_STATUS","status":[{"mediaSessionId":7,"playerState":"BUFFERING",` +
		`"currentTime":2180400,"media":{"streamType":"LIVE","duration":2184000}}]}`)
	r.watch(`{"type":"MEDIA_STATUS","status":[{"mediaSessionId":7,"playerState":"PLAYING",` +
		`"currentTime":2180410}]}`)
	want := urlplayer.Status{
		At:      2180410 * time.Second,
		Playing: true,
		Length:  2184000 * time.Second,
		Live:    true,
	}
	if !reflect.DeepEqual(r.shown, want) {
		t.Fatalf("the screen follows %+v, want %+v", r.shown, want)
	}
	if err := (remote{r}).Seek(2180420 * time.Second); err != nil {
		t.Fatal(err)
	}
	if len(r.pending) != 1 {
		t.Fatalf("%d messages queued for the page, want one seek", len(r.pending))
	}
	var body struct {
		Type        string  `json:"type"`
		CurrentTime float64 `json:"currentTime"`
	}
	if err := json.Unmarshal([]byte(r.pending[0].Data), &body); err != nil {
		t.Fatal(err)
	}
	if body.Type != cast.TypeSeek || body.CurrentTime != 2180420 {
		t.Fatalf("the page was sent %+v, want a seek to 2180420 on its own clock", body)
	}
}

type countedOutput struct {
	quietOutput
	plays atomic.Int32
}

func (o *countedOutput) Play(playback.Source) { o.plays.Add(1) }

func TestTheFirstSourceSettlesBeforeItPlays(t *testing.T) {
	video, err := os.ReadFile("../../../urlplayer/testdata/feed-video.mp4")
	if err != nil {
		t.Fatal(err)
	}
	sound, err := os.ReadFile("../../../urlplayer/testdata/feed-audio.mp4")
	if err != nil {
		t.Fatal(err)
	}
	out := &countedOutput{}
	r := New(
		cast.Env{Output: out, Video: func() playback.Target { return playback.Target{} }},
		Options{},
	)
	srv := httptest.NewServer(r.handler())
	defer srv.Close()
	c, _, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(srv.URL, "http")+"/media",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, o := range []string{
		`{"op":"source","start":0}`,
		`{"op":"add","lane":1,"type":"video/mp4"}`,
		`{"op":"add","lane":2,"type":"audio/mp4"}`,
	} {
		if err := c.WriteMessage(websocket.TextMessage, []byte(o)); err != nil {
			t.Fatal(err)
		}
	}
	for lane, data := range map[int][]byte{1: video, 2: sound} {
		head := fmt.Appendf(nil, `{"op":"append","lane":%d,"seq":%d,"offset":0}`, lane, lane)
		msg := binary.BigEndian.AppendUint32(nil, uint32(len(head)))
		if err := c.WriteMessage(
			websocket.BinaryMessage,
			append(append(msg, head...), data...),
		); err != nil {
			t.Fatal(err)
		}
	}
	for _, o := range []string{`{"op":"seek","t":0.5}`, `{"op":"play"}`} {
		if err := c.WriteMessage(websocket.TextMessage, []byte(o)); err != nil {
			t.Fatal(err)
		}
	}
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	for {
		var r report
		if err := c.ReadJSON(&r); err != nil {
			t.Fatal(err)
		}
		if r.Event == "seeked" {
			if r.T == nil || *r.T != 0.5 {
				t.Fatalf("the held seek was answered with %+v", r)
			}
			break
		}
	}
	if n := out.plays.Load(); n != 0 {
		t.Fatalf("the output played %d times while the first source settled", n)
	}
	deadline := time.Now().Add(settle + 2*time.Second)
	for out.plays.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if out.plays.Load() != 1 {
		t.Fatal("the first source never played once it settled")
	}
	if at := r.player.Begins(); at != 500*time.Millisecond {
		t.Fatalf("the settled source began at %v, want where the page seeked", at)
	}
}

func TestASeekThePlayerRefusesStillEndsTheSeek(t *testing.T) {
	r := New(
		cast.Env{Output: quietOutput{}, Video: func() playback.Target { return playback.Target{} }},
		Options{},
	)
	srv := httptest.NewServer(r.handler())
	defer srv.Close()
	c, _, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(srv.URL, "http")+"/media",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.WriteMessage(websocket.TextMessage, []byte(`{"op":"seek","t":600}`)); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	for {
		var got report
		if err := c.ReadJSON(&got); err != nil {
			t.Fatalf("the page never heard its refused seek end: %v", err)
		}
		if got.Event == "seeked" {
			if got.T == nil || *got.T != 0 {
				t.Fatalf("the refused seek ended at %+v, want where playback already was", got)
			}
			return
		}
	}
}

func TestThePageIsToldWhichSegmentsFeed(t *testing.T) {
	r := New(
		cast.Env{Output: quietOutput{}, Video: func() playback.Target { return playback.Target{} }},
		Options{},
	)
	srv := httptest.NewServer(r.handler())
	defer srv.Close()
	for typ, want := range map[string]string{
		`video/mp4; codecs="avc1.4d401e"`: "true",
		`audio/mp4; codecs="mp4a.40.2"`:   "true",
		`audio/webm; codecs="opus"`:       "true",
		`audio/mpeg`:                      "false",
	} {
		resp, err := http.Get(srv.URL + "/plays?feed=1&type=" + url.QueryEscape(typ))
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(got) != want {
			t.Errorf("feeds %q = %s, want %s", typ, got, want)
		}
	}
}

type nopOutput struct{ playback.Output }

type quietOutput struct{}

func (quietOutput) Attend(*playback.Session)                       {}
func (quietOutput) Failed(error)                                   {}
func (quietOutput) Play(playback.Source)                           {}
func (quietOutput) Stop(playback.Source)                           {}
func (quietOutput) Handoff(playback.Source)                        {}
func (quietOutput) Pause(playback.Source)                          {}
func (quietOutput) Resume(playback.Source)                         {}
func (quietOutput) Position(playback.Source) (time.Duration, bool) { return 0, false }
func (quietOutput) Behind(playback.Source) time.Duration           { return 0 }
func (quietOutput) Changed(playback.Source)                        {}
