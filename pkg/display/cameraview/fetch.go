package cameraview

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"

	"github.com/ygelfand/libcountertop/pkg/display/picture"
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
	data, err := io.ReadAll(io.LimitReader(resp.Body, frameMost))
	if err != nil {
		return nil, err
	}
	img, err := decode(data, w, h)
	if err != nil {
		return nil, err
	}
	return fit(img, w, h), nil
}

func stream(
	ctx context.Context,
	c *http.Client,
	base, picture string,
	w, h int,
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
	var buf bytes.Buffer
	for ctx.Err() == nil {
		waited := time.Now()
		part, err := parts.NextPart()
		if err != nil {
			return cmp.Or(context.Cause(ctx), err)
		}
		idle.Reset(fetchWait)
		buf.Reset()
		_, err = buf.ReadFrom(io.LimitReader(part, frameMost))
		if cerr := part.Close(); cerr != nil {
			return cerr
		}
		if err != nil {
			return cmp.Or(context.Cause(ctx), err)
		}
		decoding := time.Now()
		img, err := decode(buf.Bytes(), w, h)
		if err != nil {
			continue
		}
		fitting := time.Now()
		pic := rgba(img)
		slog.Debug("camera live frame",
			"size", pic.Bounds().Size(),
			"wait", decoding.Sub(waited),
			"decode", fitting.Sub(decoding),
			"fit", time.Since(fitting),
		)
		frame(pic)
	}
	return ctx.Err()
}

func decode(data []byte, w, h int) (image.Image, error) {
	return picture.Decode(data, image.Point{X: w, Y: h}, picture.Fast)
}

func rgba(img image.Image) *image.RGBA {
	if pic, ok := img.(*image.RGBA); ok {
		return pic
	}
	return fit(img, 0, 0)
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
