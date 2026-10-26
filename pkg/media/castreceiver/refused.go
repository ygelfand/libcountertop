package castreceiver

import (
	"context"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/fetch"
	"github.com/ygelfand/libcountertop/pkg/media/cast"
	"github.com/ygelfand/libcountertop/pkg/say"
)

const (
	refusedFor = 10 * time.Minute
	NoticeFor  = 15 * time.Second
)

type refused struct {
	mu    sync.Mutex
	until map[string]time.Time
	now   func() time.Time
}

func newRefused() *refused {
	return &refused{until: map[string]time.Time{}, now: time.Now}
}

func (r *refused) add(app string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.until[app] = r.now().Add(refusedFor)
}

func (r *refused) allows(app string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	until, ok := r.until[app]
	if !ok {
		return true
	}
	if r.now().Before(until) {
		return false
	}
	delete(r.until, app)
	return true
}

func (s *Server) tellRefused(app string) {
	if s.config.Notice == nil {
		return
	}
	name := app
	if c, err := cast.Describe(context.Background(), fetch.Client(describeWait), app); err == nil &&
		c.Name != "" {
		name = c.Name
	}
	s.config.Notice(say.F("cast.refused", map[string]any{"Name": name}), "")
}
