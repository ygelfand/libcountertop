package webapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"time"

	"rsc.io/qr"
)

const (
	controlCookie = "screen_session"
	pairLife      = 2 * time.Minute
	controlLife   = 15 * time.Minute
	controlMost   = 64
)

type controlPermit struct {
	digest     [32]byte
	credential [32]byte
	until      time.Time
	target     string
}

type controlAccess struct {
	mu           sync.Mutex
	pair         *controlPermit
	keyboardPair *controlPermit
	sessions     map[[32]byte]controlPermit
}

type inputPermitKey struct{}

type controlPairReply struct {
	URL       string    `json:"url"`
	QR        string    `json:"qr"`
	ExpiresAt time.Time `json:"expires_at"`
}

type controlView struct {
	Name      string    `json:"name"`
	ExpiresAt time.Time `json:"expires_at"`
	Target    string    `json:"target,omitempty"`
}

func (a *API) newControlPermit(life time.Duration) (string, controlPermit, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", controlPermit{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(nonce[:])
	return token, controlPermit{
		digest:     sha256.Sum256([]byte(token)),
		credential: sha256.Sum256(a.signer()),
		until:      a.o.Now().Add(life),
	}, nil
}

func (a *API) matchesControl(p *controlPermit, token string) bool {
	if p == nil || len(token) != 43 || !a.o.Now().Before(p.until) || a.Password() == "" {
		return false
	}
	digest := sha256.Sum256([]byte(token))
	credential := sha256.Sum256(a.signer())
	return hmac.Equal(p.digest[:], digest[:]) && hmac.Equal(p.credential[:], credential[:])
}

func (a *API) controlSession(r *http.Request) (controlPermit, bool) {
	cookie, err := r.Cookie(controlCookie)
	if err != nil {
		return controlPermit{}, false
	}
	digest := sha256.Sum256([]byte(cookie.Value))
	a.control.mu.Lock()
	defer a.control.mu.Unlock()
	p, ok := a.control.sessions[digest]
	return p, ok && a.matchesControl(&p, cookie.Value)
}

func (a *API) requireControl(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if permit, ok := a.controlSession(r); (!ok || permit.target != "") && !a.authorized(w, r) {
			http.Error(w, "sign in", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func (a *API) requireInput(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if permit, ok := a.controlSession(r); ok {
			ctx, cancel := context.WithDeadline(r.Context(), permit.until)
			defer cancel()
			r = r.WithContext(context.WithValue(ctx, inputPermitKey{}, permit))
		} else if !a.authorized(w, r) {
			http.Error(w, "sign in", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func (a *API) pairControl(w http.ResponseWriter, r *http.Request) {
	if a.o.Screen == nil || a.o.Pages["desktop"] == nil {
		http.NotFound(w, r)
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, loginMost)).Decode(&body) != nil ||
		len(body.URL) > 2048 {
		http.Error(w, "send the browser URL", http.StatusBadRequest)
		return
	}
	u, err := url.Parse(body.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		http.Error(w, "use an HTTPS URL", http.StatusBadRequest)
		return
	}
	token, permit, err := a.newControlPermit(pairLife)
	if err != nil {
		http.Error(w, "try again", http.StatusInternalServerError)
		return
	}
	u.Fragment = url.Values{"control": {token}}.Encode()
	code, err := qr.Encode(u.String(), qr.M)
	if err != nil {
		http.Error(w, "use a shorter URL", http.StatusBadRequest)
		return
	}
	a.control.mu.Lock()
	a.control.pair = &permit
	a.control.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	reply(w, controlPairReply{
		URL:       u.String(),
		QR:        "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()),
		ExpiresAt: permit.until,
	})
}

func (a *API) redeemControl(w http.ResponseWriter, r *http.Request) {
	from, now := remote(r), a.o.Now()
	if a.throttled(from, now) {
		http.Error(w, "try again later", http.StatusTooManyRequests)
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, loginMost)).Decode(&body) != nil {
		http.Error(w, "send a pairing code", http.StatusBadRequest)
		return
	}
	a.control.mu.Lock()
	paired := a.control.pair
	if !a.matchesControl(paired, body.Code) {
		paired = a.control.keyboardPair
	}
	if !a.matchesControl(paired, body.Code) {
		a.control.mu.Unlock()
		a.failedFrom(from, now)
		http.Error(w, "scan a fresh code", http.StatusUnauthorized)
		return
	}
	for key, permit := range a.control.sessions {
		if !now.Before(permit.until) || permit.credential != sha256.Sum256(a.signer()) {
			delete(a.control.sessions, key)
		}
	}
	if len(a.control.sessions) >= controlMost {
		a.control.mu.Unlock()
		http.Error(w, "try again later", http.StatusTooManyRequests)
		return
	}
	token, permit, err := a.newControlPermit(controlLife)
	if err != nil {
		a.control.mu.Unlock()
		http.Error(w, "try again", http.StatusInternalServerError)
		return
	}
	if a.control.sessions == nil {
		a.control.sessions = make(map[[32]byte]controlPermit)
	}
	permit.target = paired.target
	a.control.sessions[permit.digest] = permit
	a.control.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: controlCookie, Value: token, Path: Prefix,
		Expires: permit.until, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	w.Header().Set("Cache-Control", "no-store")
	reply(w, controlView{Name: a.o.Device().Name, ExpiresAt: permit.until, Target: permit.target})
}

func (a *API) controlInfo(w http.ResponseWriter, r *http.Request) {
	permit, _ := r.Context().Value(inputPermitKey{}).(controlPermit)
	w.Header().Set("Cache-Control", "no-store")
	reply(w, controlView{Name: a.o.Device().Name, ExpiresAt: permit.until, Target: permit.target})
}

func (a *API) endControl(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(controlCookie); err == nil {
		a.control.mu.Lock()
		delete(a.control.sessions, sha256.Sum256([]byte(cookie.Value)))
		a.control.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name: controlCookie, Path: Prefix, MaxAge: -1,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
}
