package cameraview

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/ygelfand/libcountertop/pkg/display/callvideo"
	"github.com/ygelfand/libcountertop/pkg/display/surface"
	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/media/rtc"
)

const (
	streamLayer = 930
	streamZ     = -2
	offerWait   = 10 * time.Second
	answerWait  = 20 * time.Second
	firstWait   = 15 * time.Second
	closeWait   = 5 * time.Second
	heardAhead  = rtc.PlayRate / 5
)

var (
	errNoPicture   = errors.New("cameras: the stream sent no picture")
	errUndecodable = errors.New("cameras: the decoder refused the stream")
	errLost        = errors.New("cameras: the stream connection was lost")
)

type Speaker interface {
	Play(pcm []int16)
	Queued() int
}

type Streams interface {
	Speaks(protocol int) bool
	OpenWebRTC(ctx context.Context, entity, offer string) (homeassistant.WebRTC, error)
	CloseWebRTC(ctx context.Context, w homeassistant.WebRTC) error
}

func (c *Cameras) stream(ctx context.Context, cam homeassistant.Camera, live func()) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	playing, played := context.WithCancel(ctx)
	defer played()

	out := callvideo.NewStream(c.o.Display, surface.AVC, streamLayer, streamZ)
	defer out.Close()
	var once sync.Once
	v, err := rtc.NewViewer(
		func(f rtc.Frame) {
			out.Show(f)
			switch {
			case out.Refused():
				cancel(errUndecodable)
			case out.Stats().Shown > 0:
				once.Do(func() {
					c.mu.Lock()
					if c.watching == cam.ID {
						c.video = out
					}
					c.mu.Unlock()
					live()
					played()
					c.o.Shell.Redraw()
				})
			}
		},
		c.play(),
		func(s webrtc.PeerConnectionState) {
			if s == webrtc.PeerConnectionStateFailed ||
				s == webrtc.PeerConnectionStateDisconnected {
				cancel(fmt.Errorf("%w: %s", errLost, s))
			}
		},
	)
	if err != nil {
		return err
	}
	defer v.Close()
	defer c.unstage(out)

	octx, ocancel := context.WithTimeout(ctx, offerWait)
	offer, err := v.Offer(octx)
	ocancel()
	if err != nil {
		return err
	}
	actx, acancel := context.WithTimeout(ctx, answerWait)
	session, err := c.o.Streams.OpenWebRTC(actx, cam.ID, offer)
	acancel()
	if err != nil {
		return err
	}
	defer func() {
		cctx, ccancel := context.WithTimeout(context.Background(), closeWait)
		defer ccancel()
		if err := c.o.Streams.CloseWebRTC(cctx, session); err != nil {
			slog.Debug("camera stream close", "camera", cam.ID, "err", err)
		}
	}()
	if err := v.Answered(session.Answer); err != nil {
		return err
	}
	slog.Info("camera answered", "camera", cam.ID, "audio", audioOf(session.Answer))
	select {
	case <-playing.Done():
	case <-time.After(firstWait):
		return errNoPicture
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	slog.Info("camera streaming", "camera", cam.ID)
	<-ctx.Done()
	if cause := context.Cause(ctx); !errors.Is(cause, context.Canceled) {
		return cause
	}
	return nil
}

func audioOf(sdp string) string {
	at := strings.Index(sdp, "m=audio ")
	if at < 0 {
		return "none"
	}
	section := sdp[at:]
	if end := strings.Index(section[1:], "\nm="); end >= 0 {
		section = section[:end+1]
	}
	if strings.HasPrefix(section, "m=audio 0 ") {
		return "rejected"
	}
	direction := "sendrecv"
	for _, d := range []string{"inactive", "sendonly", "recvonly"} {
		if strings.Contains(section, "a="+d) {
			direction = d
		}
	}
	codec := "?"
	if at := strings.Index(section, "a=rtpmap:"); at >= 0 {
		line := section[at+len("a=rtpmap:"):]
		if end := strings.IndexAny(line, "\r\n"); end >= 0 {
			line = line[:end]
		}
		if _, name, ok := strings.Cut(line, " "); ok {
			codec = name
		}
	}
	return direction + " " + codec
}

func (c *Cameras) play() func([]int16) {
	if c.o.Speaker == nil {
		return nil
	}
	return func(pcm []int16) {
		c.mu.Lock()
		on := c.audible
		c.mu.Unlock()
		if on && c.o.Speaker.Queued() <= heardAhead {
			c.o.Speaker.Play(pcm)
		}
	}
}

func (c *Cameras) toggleSound() {
	c.mu.Lock()
	c.audible = !c.audible
	c.mu.Unlock()
	c.o.Shell.Redraw()
}

func (c *Cameras) unstage(out *callvideo.Stream) {
	c.mu.Lock()
	if c.video == out {
		c.video = nil
	}
	c.mu.Unlock()
	c.o.Shell.Redraw()
}
