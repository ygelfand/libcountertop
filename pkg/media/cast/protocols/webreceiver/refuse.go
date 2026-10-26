package webreceiver

import (
	"log/slog"
	"time"
)

const quickClose = 5 * time.Second

func (r *Receiver) closed() {
	r.mu.Lock()
	began, app, title := r.began, r.asked.ID, r.title
	r.mu.Unlock()
	if !began.IsZero() && time.Since(began) < quickClose && r.env.Refuse != nil {
		slog.Info("a cast app could not start here", "app", title, "id", app,
			"after", time.Since(began).Round(100*time.Millisecond))
		r.env.Refuse(app)
	} else {
		slog.Info("the cast page closed itself")
	}
	r.end()
}

func (r *Receiver) end() {
	if end := r.env.End; end != nil {
		end()
	}
}
