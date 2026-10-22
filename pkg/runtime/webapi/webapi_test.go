package webapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	esphome "github.com/ygelfand/go-esphome-device"

	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type memory struct{ v schema.Frontend }

func (m *memory) Read() schema.Frontend { return m.v }

func (m *memory) Update(f func(*schema.Frontend)) error {
	f(&m.v)
	return nil
}

type fakeStates struct {
	mu      sync.Mutex
	fns     []func(esphome.State)
	have    []esphome.State
	pressed int
}

func (f *fakeStates) Listen(fn func(esphome.State)) func() {
	f.mu.Lock()
	f.fns = append(f.fns, fn)
	f.mu.Unlock()
	return func() {}
}

func (f *fakeStates) States() []esphome.State { return f.have }

func (f *fakeStates) Descriptions() []esphome.Description { return nil }

func (f *fakeStates) Press(object string) error {
	if object != "reboot" {
		return errors.New("no such button")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pressed++
	return nil
}

func (f *fakeStates) Actions() []esphome.Action {
	return []esphome.Action{{
		Name: "call",
		Args: []esphome.Arg{
			{Name: "who", Type: esphome.ArgString},
			{Name: "video", Type: esphome.ArgBool},
		},
		Answers: true,
	}}
}

func (f *fakeStates) RunAction(name string, c esphome.Call) (any, error) {
	if name != "call" {
		return nil, errors.New("no such action")
	}
	return fmt.Sprintf("calling %s video=%v", c.String("who"), c.Bool("video")), nil
}

func (f *fakeStates) emit(s esphome.State) {
	f.mu.Lock()
	fns := f.fns
	f.mu.Unlock()
	for _, fn := range fns {
		fn(s)
	}
}

func (f *fakeStates) listening() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.fns) > 0
}

func serve(t *testing.T, a *API) *httptest.Server {
	t.Helper()
	r := chi.NewRouter()
	for pattern, h := range a.Routes() {
		method, path, _ := strings.Cut(pattern, " ")
		r.Method(method, path, h)
	}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func fixture(t *testing.T) (*API, *memory, *fakeStates, *httptest.Server) {
	t.Helper()
	settings := &memory{}
	states := &fakeStates{
		have: []esphome.State{{Object: "call_state", Value: json.RawMessage(`{"state":"idle"}`)}},
	}
	reg := &setting.Registry{}
	a := New(Options{
		Settings: settings,
		Registry: reg,
		States:   states,
		Device:   func() Device { return Device{Name: "kitchen"} },
		Pages: map[string]http.Handler{
			"echo": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(r.Method + " " + r.URL.Path))
			}),
		},
	})
	reg.Add(a.Controls())
	a.Restore(settings.v)
	return a, settings, states, serve(t, a)
}

func do(t *testing.T, method, url, body string, edit func(*http.Request)) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestAPasswordIsMadeAndGuardsEverything(t *testing.T) {
	_, settings, _, srv := fixture(t)
	if len(settings.v.Password) != passwordSize {
		t.Fatalf("made password %q, want %d characters", settings.v.Password, passwordSize)
	}
	if got := do(
		t,
		http.MethodGet,
		srv.URL+Prefix+"/device",
		"",
		nil,
	); got.StatusCode != http.StatusUnauthorized {
		t.Errorf("device without signing in answered %d, want 401", got.StatusCode)
	}
	basic := func(r *http.Request) { r.SetBasicAuth("admin", settings.v.Password) }
	if got := do(
		t,
		http.MethodGet,
		srv.URL+Prefix+"/device",
		"",
		basic,
	); got.StatusCode != http.StatusOK {
		t.Errorf("device with the password answered %d, want 200", got.StatusCode)
	}
}

func TestDeveloperRunsActionsPressesButtonsAndKeepsThePasswordOut(t *testing.T) {
	a, settings, states, srv := fixture(t)
	basic := func(r *http.Request) { r.SetBasicAuth("admin", settings.v.Password) }

	var listed []actionInfo
	if err := json.NewDecoder(do(t, http.MethodGet, srv.URL+Prefix+"/actions", "", basic).Body).
		Decode(&listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Name != "call" || len(listed[0].Args) != 2 ||
		listed[0].Args[1].Type != "bool" {
		t.Errorf("listed %+v, want call with a string and a bool", listed)
	}

	resp := do(
		t,
		http.MethodPost,
		srv.URL+Prefix+"/actions/call",
		`{"args":{"who":"office","video":true}}`,
		basic,
	)
	var said struct{ Answer string }
	if err := json.NewDecoder(resp.Body).Decode(&said); err != nil {
		t.Fatal(err)
	}
	if said.Answer != "calling office video=true" {
		t.Errorf("call answered %q", said.Answer)
	}
	if got := do(
		t,
		http.MethodPost,
		srv.URL+Prefix+"/actions/nope",
		`{"args":{}}`,
		basic,
	); got.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("an unknown action answered %d, want 422", got.StatusCode)
	}

	if got := do(
		t,
		http.MethodPost,
		srv.URL+Prefix+"/buttons/reboot",
		"",
		basic,
	); got.StatusCode != http.StatusNoContent ||
		states.pressed != 1 {
		t.Errorf("pressing reboot answered %d with %d presses", got.StatusCode, states.pressed)
	}

	own := a.knobs.Entities()[0].Object()
	states.have = append(
		states.have,
		esphome.State{Object: own, Value: json.RawMessage(`{"state":"secret"}`)},
	)
	resp = do(t, http.MethodGet, srv.URL+Prefix+"/diagnostics", "", basic)
	if !strings.HasPrefix(
		resp.Header.Get("Content-Disposition"),
		`attachment; filename="kitchen-diagnostics-`,
	) {
		t.Errorf("diagnostics disposition %q", resp.Header.Get("Content-Disposition"))
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "secret") || !strings.Contains(string(body), "call_state") {
		t.Errorf("diagnostics %s, want call_state and no password", body)
	}
}

func TestPagesSitBehindThePasswordAndAreListed(t *testing.T) {
	_, settings, _, srv := fixture(t)
	if got := do(
		t,
		http.MethodDelete,
		srv.URL+Prefix+"/echo/a1",
		"",
		nil,
	); got.StatusCode != http.StatusUnauthorized {
		t.Errorf("a page without signing in answered %d, want 401", got.StatusCode)
	}
	basic := func(r *http.Request) { r.SetBasicAuth("admin", settings.v.Password) }
	resp := do(t, http.MethodDelete, srv.URL+Prefix+"/echo/a1", "", basic)
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "DELETE /a1" {
		t.Errorf("the page heard %q, want DELETE /a1 with the prefix gone", body)
	}
	var d Device
	if err := json.NewDecoder(do(t, http.MethodGet, srv.URL+Prefix+"/device", "", basic).Body).
		Decode(&d); err != nil {
		t.Fatal(err)
	}
	if len(d.Pages) != 1 || d.Pages[0] != "echo" {
		t.Errorf("device lists pages %v, want [echo]", d.Pages)
	}
}

func TestLoginGivesATokenThatAPasswordChangeRevokes(t *testing.T) {
	a, settings, _, srv := fixture(t)
	resp := do(
		t,
		http.MethodPost,
		srv.URL+Prefix+"/login",
		`{"password":"`+settings.v.Password+`"}`,
		nil,
	)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login answered %d", resp.StatusCode)
	}
	var got struct{ Token string }
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil || got.Token == "" {
		t.Fatalf("login returned no token (%v)", err)
	}
	if len(resp.Cookies()) == 0 || !resp.Cookies()[0].HttpOnly || !resp.Cookies()[0].Secure {
		t.Errorf("session cookie %+v, want HttpOnly and Secure", resp.Cookies())
	}
	bearer := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+got.Token) }
	if got := do(
		t,
		http.MethodGet,
		srv.URL+Prefix+"/settings",
		"",
		bearer,
	); got.StatusCode != http.StatusOK {
		t.Fatalf("settings with the token answered %d", got.StatusCode)
	}
	if err := a.Controls().Change("password", "new-password-1"); err != nil {
		t.Fatal(err)
	}
	if got := do(
		t,
		http.MethodGet,
		srv.URL+Prefix+"/settings",
		"",
		bearer,
	); got.StatusCode != http.StatusUnauthorized {
		t.Errorf("the old token still works after a password change: %d", got.StatusCode)
	}
}

func TestASessionOutlivesARestartAndSigningOutEndsIt(t *testing.T) {
	a, settings, states, _ := fixture(t)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	token, err := a.open(now)
	if err != nil {
		t.Fatal(err)
	}
	again := New(Options{
		Settings: settings,
		Registry: &setting.Registry{},
		States:   states,
		Device:   func() Device { return Device{Name: "kitchen"} },
		Now:      func() time.Time { return now },
	})
	if !again.valid(token, now) {
		t.Fatal("a session from before the restart was refused")
	}
	if again.valid(token, now.Add(sessionLife+time.Minute)) {
		t.Error("a session was accepted after it expired")
	}
	if again.valid(token[:len(token)-2]+"AA", now) {
		t.Error("a tampered session was accepted")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	if !again.authorized(w, r) {
		t.Fatal("the cookie was refused")
	}
	if len(w.Result().Cookies()) != 0 {
		t.Error("a fresh session was renewed")
	}
	late := now.Add(sessionLife - sessionRenew + time.Hour)
	again.o.Now = func() time.Time { return late }
	w = httptest.NewRecorder()
	if !again.authorized(w, r) || len(w.Result().Cookies()) != 1 {
		t.Errorf("an aging session was not renewed: %v", w.Result().Cookies())
	}

	again.close(token, now)
	if again.valid(token, now) {
		t.Error("a signed-out session still works")
	}
}

func TestWrongPasswordsAreThrottled(t *testing.T) {
	_, settings, _, srv := fixture(t)
	for range failsAllowed {
		if got := do(
			t,
			http.MethodPost,
			srv.URL+Prefix+"/login",
			`{"password":"nope"}`,
			nil,
		); got.StatusCode != http.StatusUnauthorized {
			t.Fatalf("wrong password answered %d, want 401", got.StatusCode)
		}
	}
	right := `{"password":"` + settings.v.Password + `"}`
	if got := do(
		t,
		http.MethodPost,
		srv.URL+Prefix+"/login",
		right,
		nil,
	); got.StatusCode != http.StatusTooManyRequests {
		t.Errorf(
			"after %d failures the right password answered %d, want 429",
			failsAllowed,
			got.StatusCode,
		)
	}
}

func TestSettingsReadAndWriteThroughTheRegistry(t *testing.T) {
	_, settings, _, srv := fixture(t)
	basic := func(r *http.Request) { r.SetBasicAuth("admin", settings.v.Password) }

	resp := do(t, http.MethodGet, srv.URL+Prefix+"/settings/access/password", "", basic)
	var f setting.Field
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		t.Fatal(err)
	}
	if f.Value != settings.v.Password || f.Kind != "credential" {
		t.Errorf("the password reads %+v, want the saved password as a credential", f)
	}

	resp = do(
		t,
		http.MethodPut,
		srv.URL+Prefix+"/settings/access/password",
		`{"value":"swapped-pass"}`,
		basic,
	)
	if resp.StatusCode != http.StatusOK || settings.v.Password != "swapped-pass" {
		t.Errorf("writing answered %d and saved %q", resp.StatusCode, settings.v.Password)
	}
	if got := do(
		t,
		http.MethodGet,
		srv.URL+Prefix+"/settings/nope/nothing",
		"",
		func(r *http.Request) {
			r.SetBasicAuth("admin", "swapped-pass")
		},
	); got.StatusCode != http.StatusNotFound {
		t.Errorf("an unknown setting answered %d, want 404", got.StatusCode)
	}
}

func TestEventsStartWithEveryStateThenFollow(t *testing.T) {
	_, settings, states, srv := fixture(t)
	header := http.Header{}
	req := &http.Request{Header: header}
	req.SetBasicAuth("admin", settings.v.Password)
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + Prefix + "/events"
	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	var first event
	if err := conn.ReadJSON(&first); err != nil {
		t.Fatal(err)
	}
	if first.Type != "states" || len(first.States) != 1 || first.States[0].Object != "call_state" {
		t.Fatalf("first message %+v, want the snapshot", first)
	}
	for !states.listening() {
		time.Sleep(time.Millisecond)
	}
	states.emit(esphome.State{Object: "call_state", Value: json.RawMessage(`{"state":"ringing"}`)})
	var next event
	if err := conn.ReadJSON(&next); err != nil {
		t.Fatal(err)
	}
	if next.Type != "state" || next.State == nil ||
		string(next.State.Value) != `{"state":"ringing"}` {
		t.Errorf("second message %+v, want the ringing state", next)
	}
}
