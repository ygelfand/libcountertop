package fetch

import (
	"crypto/tls"
	"net/http"
	"sync/atomic"
	"time"
)

var skip atomic.Bool

func Verify(on bool) { skip.Store(!on) }

func Skipping() bool { return skip.Load() }

func Client(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: shared}
}

var shared http.RoundTripper = &policy{
	strict: http.DefaultTransport,
	lax:    lax(),
}

type policy struct {
	strict, lax http.RoundTripper
}

func (p *policy) RoundTrip(r *http.Request) (*http.Response, error) {
	if skip.Load() {
		return p.lax.RoundTrip(r)
	}
	return p.strict.RoundTrip(r)
}

func lax() http.RoundTripper {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultTransport
	}

	c := t.Clone()
	c.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // the point of it
	return c
}
