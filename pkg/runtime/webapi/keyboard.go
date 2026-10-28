package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/ygelfand/libcountertop/pkg/input/textentry"
)

func (a *API) KeyboardPair(target string) (string, time.Time, error) {
	if a.o.Input == nil || a.o.Address == nil || target == "" {
		return "", time.Time{}, textentry.ErrUnavailable
	}
	ip := net.ParseIP(a.o.Address())
	if ip == nil {
		return "", time.Time{}, errors.New("connect the device to the network")
	}
	token, permit, err := a.newControlPermit(pairLife)
	if err != nil {
		return "", time.Time{}, err
	}
	permit.target = target
	a.control.mu.Lock()
	a.control.keyboardPair = &permit
	a.control.mu.Unlock()
	host := ip.String()
	if ip.To4() == nil {
		host = "[" + host + "]"
	}
	u := url.URL{
		Scheme:   "https",
		Host:     host,
		Path:     "/",
		Fragment: url.Values{"control": {token}}.Encode(),
	}
	return u.String(), permit.until, nil
}

func (a *API) keyboard(w http.ResponseWriter, r *http.Request) {
	if a.o.Input == nil {
		http.NotFound(w, r)
		return
	}
	var request textentry.Request
	if r.Method == http.MethodPost {
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, bodyMost)).Decode(&request) != nil {
			http.Error(w, "send a text edit", http.StatusBadRequest)
			return
		}
		if request.Action != "replace" && request.Action != "save" && request.Action != "cancel" {
			http.Error(w, "send a text edit", http.StatusBadRequest)
			return
		}
	}
	if permit, ok := r.Context().Value(inputPermitKey{}).(controlPermit); ok &&
		permit.target != "" {
		if request.Target != "" && request.Target != permit.target {
			http.Error(w, textentry.ErrTarget.Error(), http.StatusGone)
			return
		}
		request.Target = permit.target
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	view, err := a.o.Input.Call(ctx, request)
	if err != nil {
		status := http.StatusServiceUnavailable
		switch {
		case errors.Is(err, textentry.ErrTarget), errors.Is(err, textentry.ErrUnavailable):
			status = http.StatusGone
		case errors.Is(err, textentry.ErrConflict):
			status = http.StatusConflict
		case errors.Is(err, textentry.ErrInvalid):
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	reply(w, view)
}
