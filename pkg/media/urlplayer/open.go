package urlplayer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ygelfand/libcountertop/pkg/fetch"
)

const (
	chunk = 256 << 10
	ahead = 4
)

type stream struct {
	body        io.ReadCloser
	ranged      *fetch.Ranged
	contentType string
	size        int64
}

func open(ctx context.Context, client *http.Client, url string, from int64) (stream, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return stream{}, err
	}
	req.Header.Set("Range", "bytes="+strconv.FormatInt(from, 10)+"-")
	resp, err := client.Do(req)
	if err != nil {
		return stream{}, fmt.Errorf("urlplayer: fetching %s: %w", url, err)
	}
	contentType := resp.Header.Get("Content-Type")
	switch resp.StatusCode {
	case http.StatusPartialContent:
		resp.Body.Close()
		size := total(resp.Header.Get("Content-Range"))
		r := fetch.NewRanged(ctx, client, url, size, fetch.Chunks{Size: chunk, Ahead: ahead})
		if from > 0 {
			r.From(from)
		}
		return stream{body: r, ranged: r, contentType: contentType, size: size}, nil
	case http.StatusOK:
		if from > 0 {
			resp.Body.Close()
			return stream{}, fmt.Errorf("urlplayer: %s cannot start at byte %d", url, from)
		}
		return stream{body: resp.Body, contentType: contentType, size: resp.ContentLength}, nil
	}
	resp.Body.Close()
	return stream{}, fmt.Errorf("urlplayer: fetching %s: %s", url, resp.Status)
}

func total(contentRange string) int64 {
	_, after, ok := strings.Cut(contentRange, "/")
	if !ok || after == "*" {
		return 0
	}
	n, _ := strconv.ParseInt(after, 10, 64)
	return n
}
