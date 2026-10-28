package webapi

import (
	"encoding/json"
	"image"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/input/textentry"
)

func controlFixture(t *testing.T) (*API, *memory) {
	t.Helper()
	settings := &memory{}
	settings.v.Password = "test-password"
	a := New(Options{
		Settings: settings,
		Device:   func() Device { return Device{Name: "panel"} },
		Screen:   func() (image.Image, error) { return image.NewRGBA(image.Rect(0, 0, 2, 2)), nil },
		Pages: map[string]http.Handler{
			"desktop": http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
			),
			"call": http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
			),
		},
	})
	return a, settings
}

func TestKeyboardPairIsFieldBoundAndNotScreenAccess(t *testing.T) {
	a, _ := controlFixture(t)
	a.o.Address = func() string { return "192.168.1.25" }
	a.o.Input = &textentry.Controller{}
	field := textentry.View{Target: "first", Text: "hello", Revision: 1}
	a.o.Input.Attach(func(f func()) { f() }, func(r textentry.Request) (textentry.View, error) {
		if r.Target != "" && r.Target != field.Target {
			return textentry.View{}, textentry.ErrTarget
		}
		if r.Action == "replace" {
			field.Text = r.Text
			field.Revision++
		}
		return field, nil
	})
	link, _, err := a.KeyboardPair(field.Target)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(link)
	if err != nil || u.Host != "192.168.1.25" || u.Scheme != "https" {
		t.Fatalf("link = %s, %v", link, err)
	}
	fragment, _ := url.ParseQuery(u.Fragment)
	code := fragment.Get("control")
	w := redeemCode(a, code)
	if w.Code != http.StatusOK {
		t.Fatalf("redeem = %d: %s", w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	for _, path := range []string{"/screen", "/desktop", "/settings", "/device"} {
		if w := controlRequest(
			a,
			http.MethodGet,
			path,
			"",
			cookie,
			"",
		); w.Code != http.StatusUnauthorized {
			t.Fatalf("keyboard allowed %s: %d", path, w.Code)
		}
	}
	w = controlRequest(a, http.MethodGet, "/control/keyboard", "", cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("field = %d: %s", w.Code, w.Body.String())
	}
	w = controlRequest(
		a,
		http.MethodPost,
		"/control/keyboard",
		`{"target":"other","action":"replace","text":"no"}`,
		cookie,
		"",
	)
	if w.Code != http.StatusGone || field.Text != "hello" {
		t.Fatalf("other field = %d, %q", w.Code, field.Text)
	}
	w = controlRequest(
		a,
		http.MethodPost,
		"/control/keyboard",
		`{"target":"first","action":"replace","text":"new","revision":1}`,
		cookie,
		"",
	)
	if w.Code != http.StatusOK || field.Text != "new" {
		t.Fatalf("edit = %d, %q", w.Code, field.Text)
	}
	field.Target = "second"
	w = controlRequest(a, http.MethodGet, "/control/keyboard", "", cookie, "")
	if w.Code != http.StatusGone {
		t.Fatalf("changed field = %d", w.Code)
	}
}

func controlRequest(
	a *API,
	method, path, body string,
	cookie *http.Cookie,
	password string,
) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://panel"+Prefix+path, strings.NewReader(body))
	r.RemoteAddr = "192.168.1.10:1234"
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if password != "" {
		r.SetBasicAuth("", password)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}

func pairingCode(t *testing.T, a *API, password string) (string, controlPairReply) {
	t.Helper()
	w := controlRequest(
		a,
		http.MethodPost,
		"/control/pairing",
		`{"url":"https://custom.example:8443/panel/?view=overview"}`,
		nil,
		password,
	)
	if w.Code != http.StatusOK {
		t.Fatalf("pairing = %d: %s", w.Code, w.Body.String())
	}
	var reply controlPairReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(reply.URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host != "custom.example:8443" || u.Path != "/panel/" ||
		u.RawQuery != "view=overview" {
		t.Fatalf("pairing URL = %s", reply.URL)
	}
	fragment, err := url.ParseQuery(u.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	if fragment.Get("control") == "" || !strings.HasPrefix(reply.QR, "data:image/png;base64,") {
		t.Fatalf("invalid pairing reply: %+v", reply)
	}
	return fragment.Get("control"), reply
}

func redeemCode(a *API, code string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(struct {
		Code string `json:"code"`
	}{code})
	return controlRequest(a, http.MethodPost, "/control/pairing/redeem", string(body), nil, "")
}

func TestControlPairingGrantsOnlyScreenAccess(t *testing.T) {
	a, settings := controlFixture(t)
	if w := controlRequest(
		a,
		http.MethodPost,
		"/control/pairing",
		`{}`,
		nil,
		"",
	); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous pairing = %d", w.Code)
	}
	code, _ := pairingCode(t, a, settings.v.Password)
	w := redeemCode(a, code)
	if w.Code != http.StatusOK {
		t.Fatalf("redeem = %d: %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %v", cookies)
	}
	cookie := cookies[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode ||
		cookie.Name == cookieName {
		t.Fatalf("unsafe cookie: %+v", cookie)
	}
	for _, test := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/control/session", http.StatusOK},
		{http.MethodGet, "/screen", http.StatusOK},
		{http.MethodPost, "/desktop", http.StatusNoContent},
		{http.MethodPost, "/desktop/session-id", http.StatusNoContent},
		{http.MethodGet, "/settings", http.StatusUnauthorized},
		{http.MethodGet, "/logs", http.StatusUnauthorized},
		{http.MethodGet, "/device", http.StatusUnauthorized},
		{http.MethodPost, "/buttons/reboot", http.StatusUnauthorized},
		{http.MethodPost, "/call", http.StatusUnauthorized},
		{http.MethodPost, "/control/pairing", http.StatusUnauthorized},
	} {
		if got := controlRequest(
			a,
			test.method,
			test.path,
			`{}`,
			cookie,
			"",
		); got.Code != test.want {
			t.Errorf("%s %s = %d, want %d", test.method, test.path, got.Code, test.want)
		}
	}
	if w := redeemCode(a, code); w.Code != http.StatusOK {
		t.Fatalf("reused code = %d", w.Code)
	}
}

func TestControlPairingRefreshAndExpiry(t *testing.T) {
	a, settings := controlFixture(t)
	now := time.Now()
	a.o.Now = func() time.Time { return now }
	first, _ := pairingCode(t, a, settings.v.Password)
	second, _ := pairingCode(t, a, settings.v.Password)
	if w := redeemCode(a, first); w.Code != http.StatusUnauthorized {
		t.Fatalf("old QR = %d", w.Code)
	}
	now = now.Add(2 * time.Minute)
	if w := redeemCode(a, second); w.Code != http.StatusUnauthorized {
		t.Fatalf("expired QR = %d", w.Code)
	}
	third, _ := pairingCode(t, a, settings.v.Password)
	w := redeemCode(a, third)
	if w.Code != http.StatusOK {
		t.Fatalf("redeem = %d", w.Code)
	}
	cookie := w.Result().Cookies()[0]
	now = now.Add(15 * time.Minute)
	if w := controlRequest(
		a,
		http.MethodPost,
		"/desktop",
		`{}`,
		cookie,
		"",
	); w.Code != http.StatusUnauthorized {
		t.Fatalf("expired control session = %d", w.Code)
	}
}

func TestControlPairingSupportsConcurrentReuse(t *testing.T) {
	a, settings := controlFixture(t)
	code, _ := pairingCode(t, a, settings.v.Password)
	var accepted atomic.Int32
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			if redeemCode(a, code).Code == http.StatusOK {
				accepted.Add(1)
			}
		})
	}
	workers.Wait()
	if accepted.Load() != 8 {
		t.Fatalf("successful redemptions = %d", accepted.Load())
	}
}

func TestPasswordChangeRevokesPairingAndControlSession(t *testing.T) {
	a, settings := controlFixture(t)
	code, _ := pairingCode(t, a, settings.v.Password)
	settings.v.Password = "new-password"
	if w := redeemCode(a, code); w.Code != http.StatusUnauthorized {
		t.Fatalf("old credential QR = %d", w.Code)
	}
	code, _ = pairingCode(t, a, settings.v.Password)
	w := redeemCode(a, code)
	if w.Code != http.StatusOK {
		t.Fatalf("redeem = %d", w.Code)
	}
	cookie := w.Result().Cookies()[0]
	settings.v.Password = "another-password"
	if w := controlRequest(
		a,
		http.MethodPost,
		"/desktop",
		`{}`,
		cookie,
		"",
	); w.Code != http.StatusUnauthorized {
		t.Fatalf("old credential control session = %d", w.Code)
	}
}

func TestRefreshingPairingKeepsRedeemedSessionsValid(t *testing.T) {
	a, settings := controlFixture(t)
	code, _ := pairingCode(t, a, settings.v.Password)
	w := redeemCode(a, code)
	if w.Code != http.StatusOK {
		t.Fatalf("redeem = %d", w.Code)
	}
	first := w.Result().Cookies()[0]
	w = redeemCode(a, code)
	if w.Code != http.StatusOK {
		t.Fatalf("reuse = %d", w.Code)
	}
	second := w.Result().Cookies()[0]
	if first.Value == second.Value {
		t.Fatal("paired devices share a session secret")
	}
	pairingCode(t, a, settings.v.Password)
	for _, cookie := range []*http.Cookie{first, second} {
		if w := controlRequest(
			a,
			http.MethodPost,
			"/desktop",
			`{}`,
			cookie,
			"",
		); w.Code != http.StatusNoContent {
			t.Fatalf("existing session = %d", w.Code)
		}
	}
	if w := controlRequest(
		a,
		http.MethodDelete,
		"/control/session",
		``,
		first,
		"",
	); w.Code != http.StatusNoContent {
		t.Fatalf("end access = %d", w.Code)
	}
	if w := controlRequest(
		a,
		http.MethodPost,
		"/desktop",
		`{}`,
		first,
		"",
	); w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session = %d", w.Code)
	}
	if w := controlRequest(
		a,
		http.MethodPost,
		"/desktop",
		`{}`,
		second,
		"",
	); w.Code != http.StatusNoContent {
		t.Fatalf("other session = %d", w.Code)
	}
}

func TestPairingRejectsInvalidBrowserURLs(t *testing.T) {
	for _, value := range []string{"http://panel/", "https:///", "https://user:password@panel/", ""} {
		t.Run(value, func(t *testing.T) {
			a, settings := controlFixture(t)
			body, _ := json.Marshal(struct {
				URL string `json:"url"`
			}{value})
			if w := controlRequest(
				a,
				http.MethodPost,
				"/control/pairing",
				string(body),
				nil,
				settings.v.Password,
			); w.Code != http.StatusBadRequest {
				t.Fatalf("URL accepted: %d", w.Code)
			}
		})
	}
}
