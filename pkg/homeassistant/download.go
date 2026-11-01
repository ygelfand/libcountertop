package homeassistant

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
)

const (
	// One answer has to fit an encrypted ESPHome frame (64 KiB) after base64.
	chunk  = 40 << 10
	window = 8
	tries  = 3
)

var pause = time.Second

type Downloads struct{ h *Controller }

func (h *Controller) Downloads() *Downloads { return &Downloads{h: h} }

func (d *Downloads) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body != nil {
		_ = r.Body.Close()
	}
	if r.Method != http.MethodGet {
		return nil, fmt.Errorf("homeassistant: downloads only GET, not %s", r.Method)
	}
	from, to, ranged, err := span(r.Header.Get("Range"))
	if err != nil {
		return nil, err
	}
	url := r.URL.String()
	ctx := r.Context()

	first := int64(chunk)
	if ranged && to >= 0 && to-from+1 < first {
		first = to - from + 1
	}
	p, err := d.h.Fetch(ctx, url, from, first)
	if err != nil {
		return nil, err
	}
	resp := &http.Response{
		Status:     strconv.Itoa(p.Status) + " " + http.StatusText(p.Status),
		StatusCode: p.Status,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{},
		Request:    r,
	}
	if p.Type != "" {
		resp.Header.Set("Content-Type", p.Type)
	}
	if p.Status != http.StatusOK && p.Status != http.StatusPartialContent {
		resp.Body = io.NopCloser(bytes.NewReader(p.Data))
		resp.ContentLength = int64(len(p.Data))
		return resp, nil
	}

	end := int64(-1)
	if p.Total > 0 {
		end = p.Total
	}
	if ranged && to >= 0 && (end < 0 || to+1 < end) {
		end = to + 1
	}
	got := from + int64(len(p.Data))
	if int64(len(p.Data)) < first || (end >= 0 && got >= end) {
		end = got
	}
	resp.ContentLength = -1
	if end >= 0 {
		resp.ContentLength = end - from
	}
	resp.Body = io.NopCloser(bytes.NewReader(p.Data))
	if end < 0 || got < end {
		resp.Body = d.stream(ctx, url, p.Data, got, end)
	}
	if ranged {
		resp.StatusCode, resp.Status = http.StatusPartialContent, "206 Partial Content"
		if end >= 0 {
			total := "*"
			if p.Total > 0 {
				total = strconv.FormatInt(p.Total, 10)
			}
			resp.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%s", from, end-1, total))
		}
		return resp, nil
	}
	resp.StatusCode, resp.Status = http.StatusOK, "200 OK"
	return resp, nil
}

func span(h string) (from, to int64, ranged bool, err error) {
	if h == "" {
		return 0, -1, false, nil
	}
	spec, ok := strings.CutPrefix(h, "bytes=")
	a, b, dash := strings.Cut(spec, "-")
	if !ok || !dash || a == "" || strings.Contains(spec, ",") {
		return 0, 0, false, fmt.Errorf(
			"homeassistant: downloads take one bytes=FROM-[TO] range, not %q",
			h,
		)
	}
	if from, err = strconv.ParseInt(a, 10, 64); err != nil {
		return 0, 0, false, fmt.Errorf("homeassistant: range %q: %w", h, err)
	}
	to = -1
	if b != "" {
		if to, err = strconv.ParseInt(b, 10, 64); err != nil || to < from {
			return 0, 0, false, fmt.Errorf("homeassistant: range %q", h)
		}
	}
	return from, to, true, nil
}

type result struct {
	data []byte
	last bool
	err  error
}

type stream struct {
	cancel context.CancelFunc
	queue  chan chan result
	cur    []byte
	err    error
}

func (d *Downloads) stream(ctx context.Context, url string, head []byte, from, end int64) *stream {
	ctx, cancel := context.WithCancel(ctx)
	s := &stream{cancel: cancel, queue: make(chan chan result, window-1), cur: head}
	safe.Go("home assistant download", func() {
		defer close(s.queue)
		for off := from; end < 0 || off < end; off += chunk {
			n := int64(chunk)
			if end >= 0 {
				n = min(n, end-off)
			}
			c := make(chan result, 1)
			select {
			case s.queue <- c:
			case <-ctx.Done():
				return
			}
			safe.Go(
				"home assistant download piece",
				func() { c <- d.piece(ctx, url, off, n, end < 0) },
			)
		}
	})
	return s
}

func (d *Downloads) piece(ctx context.Context, url string, off, n int64, open bool) result {
	p, err := d.h.Fetch(ctx, url, off, n)
	for try := 1; err != nil && try < tries && ctx.Err() == nil; try++ {
		slog.Warn("download piece failed", "url", url, "at", off, "try", try, "err", err)
		select {
		case <-time.After(time.Duration(try) * pause):
		case <-ctx.Done():
			return result{err: ctx.Err()}
		}
		p, err = d.h.Fetch(ctx, url, off, n)
	}
	switch {
	case err != nil:
		return result{err: err}
	case open && p.Status == http.StatusRequestedRangeNotSatisfiable:
		return result{last: true}
	case open && (p.Status == http.StatusOK || p.Status == http.StatusPartialContent):
		return result{data: p.Data, last: int64(len(p.Data)) < n}
	case p.Status != http.StatusOK && p.Status != http.StatusPartialContent:
		return result{
			err: fmt.Errorf(
				"homeassistant: bytes %d-%d: %s",
				off,
				off+n-1,
				http.StatusText(p.Status),
			),
		}
	case int64(len(p.Data)) != n:
		return result{
			err: fmt.Errorf("homeassistant: bytes %d-%d: %w", off, off+n-1, io.ErrUnexpectedEOF),
		}
	}
	return result{data: p.Data}
}

func (s *stream) Read(p []byte) (int, error) {
	for len(s.cur) == 0 {
		if s.err != nil {
			return 0, s.err
		}
		c, ok := <-s.queue
		if !ok {
			s.err = io.EOF
			continue
		}
		r := <-c
		s.cur, s.err = r.data, r.err
		if r.err == nil && r.last {
			s.err = io.EOF
			s.cancel()
		}
	}
	n := copy(p, s.cur)
	s.cur = s.cur[n:]
	return n, nil
}

func (s *stream) Close() error {
	s.cancel()
	return nil
}
