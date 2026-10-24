package cast

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func from(sender, namespace, body string) Message {
	return Message{
		Source:      sender,
		Destination: ReceiverID,
		Namespace:   namespace,
		Payload:     body,
	}
}

func toApp(sender, transport, namespace, body string) Message {
	return Message{
		Source:      sender,
		Destination: transport,
		Namespace:   namespace,
		Payload:     body,
	}
}

func answered(t *testing.T, r *Receiver, m Message) Message {
	t.Helper()

	out, err := r.Receive(m)
	if err != nil {
		t.Fatalf("%s: %v", m.Namespace, err)
	}
	var direct []Message
	for _, a := range out {
		if a.Destination != Broadcast {
			direct = append(direct, a)
		}
	}
	if len(direct) != 1 {
		t.Fatalf("%s gave %d direct messages, want one", m.Namespace, len(direct))
	}
	return direct[0]
}

func replyTo(t *testing.T, r *Receiver, m Message) Message {
	t.Helper()

	out, err := r.Receive(m)
	if err != nil {
		t.Fatalf("%s: %v", m.Namespace, err)
	}
	for _, a := range out {
		if a.Destination == m.Source {
			return a
		}
	}

	t.Fatalf("%s: nothing came back to %s", m.Namespace, m.Source)
	return Message{}
}

func status(t *testing.T, payload string) ReceiverStatus {
	t.Helper()

	var got struct {
		Status ReceiverStatus `json:"status"`
	}
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("the status did not parse: %v", err)
	}
	return got.Status
}

func mediaStatus(t *testing.T, payload string) []MediaStatus {
	t.Helper()

	var got struct {
		Status []MediaStatus `json:"status"`
	}
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("the media status did not parse: %v", err)
	}
	return got.Status
}

type player struct {
	loaded  *Media
	at      time.Duration
	started bool
	playing bool
	stopped int
	sought  time.Duration
	fail    error
}

func (p *player) Load(m Media, at time.Duration, play bool) error {
	if p.fail != nil {
		return p.fail
	}
	p.loaded, p.at, p.started, p.playing = &m, at, play, play
	return nil
}

func (p *player) Play() error  { p.playing = true; return nil }
func (p *player) Pause() error { p.playing = false; return nil }

func (p *player) Stop() error {
	p.playing = false
	p.stopped++
	return nil
}

func (p *player) Seek(to time.Duration) error { p.sought = to; return nil }
func (p *player) Elapsed() time.Duration      { return p.sought }

func TestASenderCastsSomethingAndControlsIt(t *testing.T) {
	p := &player{}
	r := NewReceiver("Kitchen")
	r.Player = p

	var launched, stopped string
	r.Launched = func(app App) { launched = app.ID }
	r.Stopped = func(app string) { stopped = app }

	if out, err := r.Receive(from(SenderID, NSConnection, Connect())); err != nil || len(out) != 0 {
		t.Fatalf("connect gave %d messages, %v", len(out), err)
	}

	pong := answered(t, r, from(SenderID, NSHeartbeat, Ping()))
	if h, _ := Kind(pong.Payload); h.Type != TypePong {
		t.Fatalf("a ping was answered with %q", h.Type)
	}

	first := answered(t, r, from(SenderID, NSReceiver, `{"type":"GET_STATUS","requestId":1}`))
	if got := status(t, first.Payload); len(got.Applications) != 0 {
		t.Errorf("a fresh receiver says %d applications are running", len(got.Applications))
	}

	out := answered(t, r, from(SenderID, NSReceiver,
		`{"type":"LAUNCH","requestId":2,"appId":"`+DefaultMediaReceiver+`"}`))

	h, err := Kind(out.Payload)
	if err != nil {
		t.Fatalf("Kind: %v", err)
	}
	if h.Type != TypeStatus {
		t.Fatalf("a launch was answered with %q, want a receiver status", h.Type)
	}
	if h.RequestID != 2 {
		t.Errorf("answered request %d, want 2", h.RequestID)
	}
	if launched != DefaultMediaReceiver {
		t.Errorf("launched %q", launched)
	}

	app := status(t, out.Payload).Applications
	if len(app) != 1 {
		t.Fatalf("%d applications after a launch", len(app))
	}
	transport := app[0].TransportID
	if transport == "" {
		t.Fatal("the application has no transport id, so nothing can be addressed to it")
	}

	var speaks bool
	for _, n := range app[0].Namespaces {
		if n.Name == NSMedia {
			speaks = true
		}
	}
	if !speaks {
		t.Error("the application does not claim the media namespace")
	}

	loaded := answered(t, r, toApp(SenderID, transport, NSMedia, `{
		"type":"LOAD","requestId":3,
		"media":{"contentId":"http://example/track.mp3","streamType":"BUFFERED","contentType":"audio/mpeg",
		         "metadata":{"metadataType":3,"title":"Sun Ra","artist":"Arkestra"},"duration":215}
	}`))

	if h, _ := Kind(loaded.Payload); h.Type != TypeMediaStatus {
		t.Fatalf("a load was answered with %q", h.Type)
	}
	if p.loaded == nil || p.loaded.ContentID != "http://example/track.mp3" {
		t.Fatalf("the player was given %+v", p.loaded)
	}
	if !p.started {
		t.Error("the load did not start playing, although autoplay was not set to false")
	}

	list := mediaStatus(t, loaded.Payload)
	if len(list) != 1 {
		t.Fatalf("%d statuses", len(list))
	}
	if list[0].PlayerState != StatePlaying {
		t.Errorf("state %q", list[0].PlayerState)
	}
	if list[0].Media == nil || list[0].Media.Metadata.Title != "Sun Ra" {
		t.Error("the status after a load does not carry the media")
	}
	session := list[0].MediaSessionID
	if session == 0 {
		t.Fatal("the load produced no session id")
	}

	paused := answered(t, r, toApp(SenderID, transport, NSMedia,
		`{"type":"PAUSE","requestId":4,"mediaSessionId":`+itoa(session)+`}`))

	if p.playing {
		t.Error("the player is still playing after a pause")
	}
	if got := mediaStatus(t, paused.Payload); got[0].PlayerState != StatePaused {
		t.Errorf("state %q after a pause", got[0].PlayerState)
	}

	stops, err := r.Receive(
		from(
			SenderID,
			NSReceiver,
			`{"type":"STOP","requestId":5,"sessionId":"`+app[0].SessionID+`"}`,
		),
	)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	var idled bool
	for _, m := range stops {
		if m.Namespace == NSMedia {
			got := mediaStatus(t, m.Payload)
			idled = len(got) == 1 && got[0].PlayerState == StateIdle &&
				got[0].IdleReason == IdleCancelled
		}
	}
	if !idled {
		t.Error("stopping the application did not tell the sender the media went idle")
	}
	if stopped != DefaultMediaReceiver {
		t.Errorf("stopped %q", stopped)
	}
	if r.Running() != "" {
		t.Errorf("%q is still running", r.Running())
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}

	var out []byte
	for v > 0 {
		out = append([]byte{byte('0' + v%10)}, out...)
		v /= 10
	}
	return string(out)
}

func TestAnyApplicationIsAccepted(t *testing.T) {
	for _, app := range []string{DefaultMediaReceiver, "C35B0678", "DD107DDB", "DEADBEEF"} {
		r := NewReceiver("Kitchen")

		out := answered(t, r, from(SenderID, NSReceiver,
			`{"type":"LAUNCH","requestId":1,"appId":"`+app+`"}`))

		h, _ := Kind(out.Payload)
		if h.Type != TypeStatus {
			t.Errorf("%s came back %q, want a receiver status", app, h.Type)
			continue
		}
		if got := r.Running(); got != app {
			t.Errorf("%s is running %q, want the id that was asked for", app, got)
		}

		apps := status(t, out.Payload).Applications
		if len(apps) != 1 || apps[0].AppID != app {
			t.Errorf("%s: the status does not name it as running: %+v", app, apps)
		}
	}
}

func TestAcceptingAnAppDoesNotPromiseItsNamespaces(t *testing.T) {
	r := NewReceiver("Kitchen")

	out := answered(t, r, from(SenderID, NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"C35B0678"}`))

	apps := status(t, out.Payload).Applications
	if len(apps) != 1 {
		t.Fatalf("want one application, got %+v", apps)
	}

	for _, n := range apps[0].Namespaces {
		switch n.Name {
		case NSConnection, NSHeartbeat, NSMedia:
		default:
			t.Errorf("offers %q, which this device does not speak", n.Name)
		}
	}
}

func TestASecondSenderJoinsRatherThanRestarts(t *testing.T) {
	r := NewReceiver("Kitchen")

	first := answered(t, r, from(SenderID, NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`))
	was := status(t, first.Payload).Applications[0].SessionID

	if _, err := r.Receive(from("sender-1", NSConnection, Connect())); err != nil {
		t.Fatal(err)
	}

	out, err := r.Receive(from("sender-1", NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`))
	if err != nil {
		t.Fatalf("the second launch: %v", err)
	}

	now := status(t, out[0].Payload).Applications[0].SessionID
	if now != was {
		t.Errorf("the application restarted: session %q became %q", was, now)
	}
}

func TestTheOtherSendersAreToldAboutAChange(t *testing.T) {
	r := NewReceiver("Kitchen")

	if _, err := r.Receive(from(SenderID, NSConnection, Connect())); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Receive(from("sender-1", NSConnection, Connect())); err != nil {
		t.Fatal(err)
	}

	out, err := r.Receive(from(SenderID, NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`))
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("%d messages, want the answer and one broadcast", len(out))
	}

	var told bool
	for _, m := range out {
		if m.Destination != Broadcast {
			continue
		}
		told = true
		if !strings.Contains(m.Payload, `"requestId":0`) {
			t.Errorf("the broadcast %s does not carry request 0", m.Payload)
		}
	}
	if !told {
		t.Error("nothing was broadcast")
	}
}

func TestAMuteDoesNotMoveTheLevel(t *testing.T) {
	r := NewReceiver("Kitchen")

	var gotLevel float64
	var gotMuted bool
	r.Volume = func(level float64, muted bool) { gotLevel, gotMuted = level, muted }

	out := answered(t, r, from(SenderID, NSReceiver,
		`{"type":"SET_VOLUME","requestId":1,"volume":{"level":0.4}}`))
	if v := status(t, out.Payload).Volume; v.Level == nil || *v.Level != 0.4 {
		t.Fatalf("the level came back %v", v.Level)
	}

	out = answered(t, r, from(SenderID, NSReceiver,
		`{"type":"SET_VOLUME","requestId":2,"volume":{"muted":true}}`))

	v := status(t, out.Payload).Volume
	if v.Level == nil || *v.Level != 0.4 {
		t.Errorf("muting moved the level to %v", v.Level)
	}
	if v.Muted == nil || !*v.Muted {
		t.Error("the mute did not take")
	}
	if gotLevel != 0.4 || !gotMuted {
		t.Errorf("the host was told level %v muted %v", gotLevel, gotMuted)
	}
}

func TestACommandForTheWrongSessionIsRefused(t *testing.T) {
	r := NewReceiver("Kitchen")
	p := &player{}
	r.Player = p

	out := answered(t, r, from(SenderID, NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`))
	transport := status(t, out.Payload).Applications[0].TransportID

	answered(t, r, toApp(SenderID, transport, NSMedia,
		`{"type":"LOAD","requestId":2,"media":{"contentId":"http://example/a.mp3"}}`))

	refused := answered(t, r, toApp(SenderID, transport, NSMedia,
		`{"type":"PAUSE","requestId":3,"mediaSessionId":999}`))

	if h, _ := Kind(refused.Payload); h.Type != TypeInvalid {
		t.Errorf("came back %q, want a refusal", h.Type)
	}
	if !p.playing {
		t.Error("the wrong session's pause stopped the music")
	}
}

func TestAutoplayFalseLoadsWithoutStarting(t *testing.T) {
	r := NewReceiver("Kitchen")
	p := &player{}
	r.Player = p

	out := answered(t, r, from(SenderID, NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`))
	transport := status(t, out.Payload).Applications[0].TransportID

	loaded := answered(t, r, toApp(
		SenderID,
		transport,
		NSMedia,
		`{"type":"LOAD","requestId":2,"autoplay":false,"media":{"contentId":"http://example/a.mp3"}}`,
	))

	if p.started {
		t.Error("autoplay false started playing")
	}
	if got := mediaStatus(t, loaded.Payload); got[0].PlayerState != StatePaused {
		t.Errorf("state %q, want paused", got[0].PlayerState)
	}
}

func TestALoadThePlayerRefuses(t *testing.T) {
	r := NewReceiver("Kitchen")
	p := &player{fail: errBroken}
	r.Player = p

	out := answered(t, r, from(SenderID, NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`))
	transport := status(t, out.Payload).Applications[0].TransportID

	answers, err := r.Receive(toApp(SenderID, transport, NSMedia,
		`{"type":"LOAD","requestId":2,"media":{"contentId":"http://example/a.mp3"}}`))
	if err == nil {
		t.Error("a load that failed reported success")
	}
	if len(answers) != 1 {
		t.Fatalf("%d answers", len(answers))
	}
	if h, _ := Kind(answers[0].Payload); h.Type != TypeLoadFailed {
		t.Errorf("came back %q, want a load failure", h.Type)
	}
	if r.Media() != nil {
		t.Error("something was left loaded")
	}
}

var errBroken = &castError{"nothing to play it with"}

type castError struct{ s string }

func (e *castError) Error() string { return e.s }

func TestStoppingTheMediaLeavesTheApplicationUp(t *testing.T) {
	r := NewReceiver("Kitchen")
	r.Player = &player{}

	out := answered(t, r, from(SenderID, NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`))
	transport := status(t, out.Payload).Applications[0].TransportID

	answered(t, r, toApp(SenderID, transport, NSMedia,
		`{"type":"LOAD","requestId":2,"media":{"contentId":"http://example/a.mp3"}}`))

	answered(t, r, toApp(SenderID, transport, NSMedia, `{"type":"STOP","requestId":3}`))

	if r.Running() == "" {
		t.Error("stopping the media stopped the application")
	}
	if r.Media() != nil {
		t.Error("the media is still loaded")
	}
}

func TestFinishingTellsEverySender(t *testing.T) {
	r := NewReceiver("Kitchen")
	r.Player = &player{}

	if _, err := r.Receive(from(SenderID, NSConnection, Connect())); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Receive(from("sender-1", NSConnection, Connect())); err != nil {
		t.Fatal(err)
	}

	out := replyTo(t, r, from(SenderID, NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`))
	transport := status(t, out.Payload).Applications[0].TransportID

	if _, err := r.Receive(toApp(SenderID, transport, NSMedia,
		`{"type":"LOAD","requestId":2,"media":{"contentId":"http://example/a.mp3"}}`)); err != nil {
		t.Fatal(err)
	}

	told := r.Finished()
	if len(told) != 1 || told[0].Destination != Broadcast {
		t.Fatalf("finishing sent %+v, want one broadcast", told)
	}
	if strings.Contains(told[0].Payload, "requestId") {
		t.Errorf("a media broadcast carries a request id: %s", told[0].Payload)
	}

	for _, m := range told {
		got := mediaStatus(t, m.Payload)
		if len(got) != 1 {
			t.Fatalf("%d statuses", len(got))
		}
		if got[0].PlayerState != StateIdle {
			t.Errorf("state %q, want idle", got[0].PlayerState)
		}
		if got[0].IdleReason != IdleFinished {
			t.Errorf("idle reason %q, want finished", got[0].IdleReason)
		}
	}
}

func TestACommandWithNothingRunning(t *testing.T) {
	r := NewReceiver("Kitchen")

	out := answered(t, r, toApp(SenderID, "1", NSMedia, `{"type":"PAUSE","requestId":1}`))
	if h, _ := Kind(out.Payload); h.Type != TypeInvalid {
		t.Errorf("came back %q, want a refusal", h.Type)
	}
}

func TestAskingWhatIsPlayingWhenNothingIs(t *testing.T) {
	r := NewReceiver("Kitchen")

	out := answered(t, r, toApp(SenderID, "1", NSMedia, `{"type":"GET_STATUS","requestId":1}`))

	h, _ := Kind(out.Payload)
	if h.Type != TypeMediaStatus {
		t.Fatalf("came back %q", h.Type)
	}
	if got := mediaStatus(t, out.Payload); len(got) != 0 {
		t.Errorf("%d statuses, want none", len(got))
	}
}

func TestRubbishIsAnErrorAndNotAnAnswer(t *testing.T) {
	r := NewReceiver("Kitchen")

	out, err := r.Receive(from(SenderID, NSReceiver, `not json at all`))
	if err == nil {
		t.Error("rubbish was accepted")
	}
	if len(out) != 0 {
		t.Errorf("%d messages came back", len(out))
	}
}

func TestANamespaceThisDoesNotSpeak(t *testing.T) {
	r := NewReceiver("Kitchen")

	out, err := r.Receive(from(SenderID, "urn:x-cast:com.example.whatever", `{"type":"HELLO"}`))
	if err != nil {
		t.Errorf("gave %v", err)
	}
	if len(out) != 0 {
		t.Errorf("%d messages came back", len(out))
	}
}

func TestAClosedSenderIsNotToldAnyMore(t *testing.T) {
	r := NewReceiver("Kitchen")

	if _, err := r.Receive(from(SenderID, NSConnection, Connect())); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Receive(from("sender-1", NSConnection, Connect())); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Receive(from("sender-1", NSConnection, Close())); err != nil {
		t.Fatal(err)
	}

	out, err := r.Receive(from(SenderID, NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`))
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	for _, m := range out {
		if m.Destination == "sender-1" {
			t.Errorf("the closed sender was addressed: %s", m.Payload)
		}
	}
}

func TestSeeking(t *testing.T) {
	r := NewReceiver("Kitchen")
	p := &player{}
	r.Player = p

	out := answered(t, r, from(SenderID, NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`))
	transport := status(t, out.Payload).Applications[0].TransportID

	answered(t, r, toApp(SenderID, transport, NSMedia,
		`{"type":"LOAD","requestId":2,"media":{"contentId":"http://example/a.mp3"}}`))

	sought := answered(t, r, toApp(SenderID, transport, NSMedia,
		`{"type":"SEEK","requestId":3,"currentTime":42.5,"resumeState":"PLAYBACK_PAUSE"}`))

	if p.sought != 42500*time.Millisecond {
		t.Errorf("sought to %v, want 42.5s", p.sought)
	}
	if got := mediaStatus(t, sought.Payload); got[0].PlayerState != StatePaused {
		t.Errorf("state %q, want paused", got[0].PlayerState)
	}
}

func TestTheStatusSaysWhatCanBeDoneToIt(t *testing.T) {
	r := NewReceiver("Kitchen")
	r.Player = &player{}

	out := answered(t, r, from(SenderID, NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`))
	transport := status(t, out.Payload).Applications[0].TransportID

	loaded := answered(t, r, toApp(SenderID, transport, NSMedia,
		`{"type":"LOAD","requestId":2,"media":{"contentId":"http://example/a.mp3"}}`))

	got := mediaStatus(t, loaded.Payload)[0]
	for _, want := range []struct {
		name string
		bit  int
	}{
		{"pause", CommandPause},
		{"seek", CommandSeek},
		{"volume", CommandStreamVolume},
		{"mute", CommandStreamMute},
	} {
		if got.SupportedMediaCommands&want.bit == 0 {
			t.Errorf("the status does not offer %s", want.name)
		}
	}

	if got.SupportedMediaCommands&CommandQueueNext != 0 {
		t.Error("the status claims a queue, which would get a next button that does nothing")
	}
}

func TestTheBroadcastCarriesTheMedia(t *testing.T) {
	r := NewReceiver("Kitchen")
	r.Player = &player{}

	if _, err := r.Receive(from(SenderID, NSConnection, Connect())); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Receive(from("sender-1", NSConnection, Connect())); err != nil {
		t.Fatal(err)
	}

	out := replyTo(t, r, from(SenderID, NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`))
	transport := status(t, out.Payload).Applications[0].TransportID

	answers, err := r.Receive(toApp(
		SenderID,
		transport,
		NSMedia,
		`{"type":"LOAD","requestId":2,"media":{"contentId":"http://example/a.mp3","metadata":{"title":"x"}}}`,
	))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(answers) != 2 {
		t.Fatalf("%d messages, want the answer and one broadcast", len(answers))
	}

	for _, m := range answers {
		got := mediaStatus(t, m.Payload)[0]
		if m.Destination == SenderID && got.Media == nil {
			t.Error("the answer does not carry the media")
		}
		if m.Destination == "sender-1" && got.Media == nil {
			t.Error("the broadcast does not carry the media")
		}
	}
}

func launched(t *testing.T) (*Receiver, string) {
	t.Helper()
	r := NewReceiver("dev")
	r.Player = &player{}
	if _, err := r.Receive(from(SenderID, NSConnection, Connect())); err != nil {
		t.Fatal(err)
	}
	out := replyTo(
		t,
		r,
		from(
			SenderID,
			NSReceiver,
			`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`,
		),
	)
	return r, status(t, out.Payload).Applications[0].TransportID
}

func TestStreamVolumeIsAccepted(t *testing.T) {
	r, transport := launched(t)
	var level float64
	r.Volume = func(l float64, _ bool) { level = l }
	replyTo(
		t,
		r,
		toApp(
			SenderID,
			transport,
			NSMedia,
			`{"type":"LOAD","requestId":2,"media":{"contentId":"http://example/a.mp3"}}`,
		),
	)
	got := replyTo(
		t,
		r,
		toApp(
			SenderID,
			transport,
			NSMedia,
			`{"type":"SET_VOLUME","requestId":3,"volume":{"level":0.25}}`,
		),
	)
	if h, _ := Kind(got.Payload); h.Type != TypeMediaStatus {
		t.Fatalf("answer %s", got.Payload)
	}
	if level != 0.25 {
		t.Errorf("level %v", level)
	}
}

func TestLiveMediaCannotSeek(t *testing.T) {
	r, transport := launched(t)
	loaded := replyTo(
		t,
		r,
		toApp(
			SenderID,
			transport,
			NSMedia,
			`{"type":"LOAD","requestId":2,"media":{"contentUrl":"http://example/radio","streamType":"LIVE","contentType":"audio/mpeg"}}`,
		),
	)
	s := mediaStatus(t, loaded.Payload)[0]
	if s.SupportedMediaCommands&CommandSeek != 0 {
		t.Error("live media offers seek")
	}
	if s.MediaCategory != "AUDIO" {
		t.Errorf("category %q", s.MediaCategory)
	}
	sought := replyTo(
		t,
		r,
		toApp(SenderID, transport, NSMedia, `{"type":"SEEK","requestId":3,"currentTime":30}`),
	)
	if h, _ := Kind(sought.Payload); h.Type == TypeMediaStatus {
		t.Error("a seek on live media was accepted")
	}
}

func TestForgettingAConnection(t *testing.T) {
	r := NewReceiver("Kitchen")
	p := &player{}
	r.Player = p

	if _, err := r.Receive(from(SenderID, NSConnection, Connect())); err != nil {
		t.Fatal(err)
	}
	out := answered(t, r, from(SenderID, NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`))
	transport := status(t, out.Payload).Applications[0].TransportID

	answered(t, r, toApp(SenderID, transport, NSMedia,
		`{"type":"LOAD","requestId":2,"media":{"contentId":"http://example/a.mp3"}}`))

	r.Forget()

	if r.Running() != "" || r.Media() != nil {
		t.Errorf("state survived: running %q media %v", r.Running(), r.Media())
	}
	if p.stopped == 0 {
		t.Error("the player was not stopped")
	}

	again, err := r.Receive(from("sender-2", NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`))
	if err != nil {
		t.Fatalf("relaunch: %v", err)
	}
	for _, m := range again {
		if m.Destination != "sender-2" && m.Destination != Broadcast {
			t.Errorf("a forgotten sender %q was addressed", m.Destination)
		}
	}
}

func TestEndingFromThisEndTellsEverySender(t *testing.T) {
	r := NewReceiver("Kitchen")
	if _, err := r.Receive(from(SenderID, NSConnection, Connect())); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Receive(from("sender-1", NSConnection, Connect())); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Receive(from(SenderID, NSReceiver,
		`{"type":"LAUNCH","requestId":1,"appId":"`+DefaultMediaReceiver+`"}`)); err != nil {
		t.Fatal(err)
	}

	out := r.End()
	if r.Running() != "" {
		t.Fatalf("still running %q after End", r.Running())
	}
	if len(out) != 1 || out[0].Destination != Broadcast {
		t.Fatalf("ending sent %+v, want one broadcast", out)
	}
	for _, m := range out {
		if apps := status(t, m.Payload).Applications; len(apps) != 0 {
			t.Errorf("%s was told %d applications are running", m.Destination, len(apps))
		}
	}
	if again := r.End(); again != nil {
		t.Errorf("ending with nothing running said %d things", len(again))
	}
}

func TestAMediaChangeIsBroadcastWithoutARequestID(t *testing.T) {
	r, transport := launched(t)
	out, err := r.Receive(
		toApp(
			SenderID,
			transport,
			NSMedia,
			`{"type":"LOAD","requestId":2,"media":{"contentId":"http://example/a.mp3"}}`,
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	var pushed bool
	for _, m := range out {
		if m.Destination == Broadcast && m.Namespace == NSMedia {
			pushed = true
			if strings.Contains(m.Payload, "requestId") || m.Source != transport {
				t.Errorf("broadcast %s from %s", m.Payload, m.Source)
			}
		}
	}
	if !pushed {
		t.Error("the load was not broadcast")
	}
}

func TestConnectingToTheAppGetsTheMediaStatus(t *testing.T) {
	r, transport := launched(t)
	if _, err := r.Receive(
		toApp(
			SenderID,
			transport,
			NSMedia,
			`{"type":"LOAD","requestId":2,"media":{"contentId":"http://example/a.mp3"}}`,
		),
	); err != nil {
		t.Fatal(err)
	}
	out, err := r.Receive(toApp("sender-2", transport, NSConnection, Connect()))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Destination != "sender-2" || out[0].Namespace != NSMedia ||
		!strings.Contains(out[0].Payload, `"requestId":0`) {
		t.Fatalf("connecting gave %+v", out)
	}
	if got := mediaStatus(t, out[0].Payload); len(got) != 1 || got[0].Media == nil {
		t.Errorf("status %+v", got)
	}
}

func TestAnAppPublishesWhatItPlays(t *testing.T) {
	r, transport := launched(t)
	app := &player{playing: true, sought: 42 * time.Second}
	pub := &Published{
		Media: Media{
			ContentID:   "abc",
			ContentType: "x-youtube/video",
			StreamType:  StreamBuffered,
			Duration:    300,
		},
		State:    StatePlaying,
		Commands: 311299,
		Custom:   json.RawMessage(`{"playerState":1}`),
	}
	out := r.Publish(app, pub)
	if len(out) != 1 || out[0].Destination != Broadcast || out[0].Source != transport ||
		strings.Contains(out[0].Payload, "requestId") {
		t.Fatalf("published %+v", out)
	}
	s := mediaStatus(t, out[0].Payload)[0]
	if s.Media == nil || s.Media.ContentID != "abc" || s.SupportedMediaCommands != 311299 ||
		s.CurrentTime != 42 ||
		string(s.CustomData) != `{"playerState":1}` {
		t.Fatalf("status %s", out[0].Payload)
	}
	session := s.MediaSessionID
	if r.Publish(app, pub) != nil {
		t.Error("an unchanged status was told again")
	}
	pub.State = StateBuffering
	if again := mediaStatus(t, r.Publish(app, pub)[0].Payload)[0]; again.MediaSessionID != session {
		t.Error("the same video got a new session")
	}

	paused := replyTo(
		t,
		r,
		toApp(
			SenderID,
			transport,
			NSMedia,
			`{"type":"PAUSE","requestId":5,"mediaSessionId":`+itoa(session)+`}`,
		),
	)
	if app.playing || mediaStatus(t, paused.Payload)[0].PlayerState != StatePaused {
		t.Errorf("pause gave %s", paused.Payload)
	}

	gone := r.Publish(app, nil)
	if len(gone) == 0 || !strings.Contains(gone[0].Payload, IdleCancelled) {
		t.Fatalf("stopping gave %+v", gone)
	}
	if r.Publish(app, nil) != nil {
		t.Error("stopping twice told the senders twice")
	}
}

type stepper struct {
	player
	by int
}

func (s *stepper) Step(by int) error { s.by += by; return nil }

func TestAnAppIsAskedForStatusAndSteps(t *testing.T) {
	r, transport := launched(t)
	app := &stepper{}
	out := r.Publish(
		app,
		&Published{
			Media:    Media{ContentID: "abc", StreamType: StreamBuffered},
			State:    StatePlaying,
			Commands: CommandQueueNext,
		},
	)
	session := mediaStatus(t, out[0].Payload)[0].MediaSessionID

	asked := answered(
		t,
		r,
		toApp(SenderID, transport, NSMedia, `{"type":"GET_STATUS","requestId":7}`),
	)
	if s := mediaStatus(
		t,
		asked.Payload,
	); len(s) != 1 || s[0].Media == nil ||
		s[0].Media.ContentID != "abc" {
		t.Fatalf("status %s", asked.Payload)
	}

	replyTo(
		t,
		r,
		toApp(
			SenderID,
			transport,
			NSMedia,
			`{"type":"QUEUE_NEXT","requestId":8,"mediaSessionId":`+itoa(session)+`}`,
		),
	)
	replyTo(
		t,
		r,
		toApp(
			SenderID,
			transport,
			NSMedia,
			`{"type":"QUEUE_UPDATE","requestId":9,"jump":-1,"mediaSessionId":`+itoa(session)+`}`,
		),
	)
	if app.by != 0 {
		t.Errorf("stepped %d", app.by)
	}
	replyTo(
		t,
		r,
		toApp(
			SenderID,
			transport,
			NSMedia,
			`{"type":"QUEUE_UPDATE","requestId":10,"jump":1,"mediaSessionId":`+itoa(session)+`}`,
		),
	)
	if app.by != 1 {
		t.Errorf("stepped %d", app.by)
	}
}
