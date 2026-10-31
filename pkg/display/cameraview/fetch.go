package cameraview

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
)

const (
	snapshotPath = "/api/camera_proxy/"
	streamPath   = "/api/camera_proxy_stream/"
	frameMost    = 8 << 20
)

var (
	ErrNoSite = errors.New("cameras: Home Assistant's address is not known")
	errIdle   = errors.New("cameras: the live stream stopped sending pictures")
)

func get(ctx context.Context, c *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf(
			"cameras: %s answered %d",
			strings.SplitN(url, "?", 2)[0],
			resp.StatusCode,
		)
	}
	return resp, nil
}

func snapshot(
	ctx context.Context,
	c *http.Client,
	base, picture string,
	w, h int,
) (*image.RGBA, error) {
	if base == "" {
		return nil, ErrNoSite
	}
	resp, err := get(ctx, c, base+picture)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	img, _, err := image.Decode(io.LimitReader(resp.Body, frameMost))
	if err != nil {
		return nil, err
	}
	return fit(img, w, h), nil
}

func stream(
	ctx context.Context,
	c *http.Client,
	base, picture string,
	frame func(*image.RGBA),
) error {
	if base == "" {
		return ErrNoSite
	}
	streaming := *c
	streaming.Timeout = 0
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	idle := time.AfterFunc(fetchWait, func() { cancel(errIdle) })
	defer idle.Stop()
	resp, err := get(ctx, &streaming, base+strings.Replace(picture, snapshotPath, streamPath, 1))
	if err != nil {
		return cmp.Or(context.Cause(ctx), err)
	}
	defer resp.Body.Close()
	_, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || params["boundary"] == "" {
		return fmt.Errorf("cameras: the live stream is not multipart: %v", err)
	}
	// Home Assistant declares boundary=--frameboundary and delimits parts with --frameboundary.
	parts := multipart.NewReader(
		bufio.NewReader(resp.Body),
		strings.TrimPrefix(params["boundary"], "--"),
	)
	for ctx.Err() == nil {
		waited := time.Now()
		part, err := parts.NextPart()
		if err != nil {
			return cmp.Or(context.Cause(ctx), err)
		}
		idle.Reset(fetchWait)
		decoding := time.Now()
		img, _, err := image.Decode(io.LimitReader(part, frameMost))
		if cerr := part.Close(); cerr != nil {
			return cerr
		}
		if err != nil {
			continue
		}
		fitting := time.Now()
		fitted := fit(img, 0, 0)
		slog.Debug("camera live frame",
			"size", img.Bounds().Size(),
			"wait", decoding.Sub(waited),
			"decode", fitting.Sub(decoding),
			"fit", time.Since(fitting),
		)
		frame(fitted)
	}
	return ctx.Err()
}

func fit(img image.Image, w, h int) *image.RGBA {
	b := img.Bounds()
	if w <= 0 || h <= 0 || b.Dx() == 0 || b.Dy() == 0 {
		out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		xdraw.Draw(out, out.Bounds(), img, b.Min, xdraw.Src)
		return out
	}
	sw, sh := w, b.Dy()*w/b.Dx()
	if sh > h {
		sw, sh = b.Dx()*h/b.Dy(), h
	}
	out := image.NewRGBA(image.Rect(0, 0, max(sw, 1), max(sh, 1)))
	xdraw.ApproxBiLinear.Scale(out, out.Bounds(), img, b, xdraw.Src, nil)
	return out
}
