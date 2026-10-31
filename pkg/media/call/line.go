package call

import (
	"context"
	"net/http"

	esphome "github.com/ygelfand/go-esphome-device"

	discovery "github.com/ygelfand/libcountertop/pkg/network/peer"
)

const homeCaller = "homeassistant"

type line interface {
	send(ctx context.Context, what string, m envelope) (int, error)
}

type peerLine struct {
	c    *Calls
	base string
}

func (l peerLine) send(ctx context.Context, what string, m envelope) (int, error) {
	return l.c.post(ctx, l.base, what, m)
}

type homeLine struct {
	home    Home
	session string
}

func (l homeLine) send(ctx context.Context, what string, m envelope) (int, error) {
	data := map[string]string{"session": l.session}
	switch what {
	case "answer":
		data["sdp"] = m.SDP
	case "end":
		data["reason"] = string(m.Reason)
	default:
		return http.StatusNotImplemented, nil
	}
	if err := l.home.Call(ctx, "echolocal.call_"+what, data); err != nil {
		return 0, err
	}
	return http.StatusOK, nil
}

func (c *Calls) Actions() []*esphome.Action {
	if c.options.Home == nil {
		return nil
	}
	return []*esphome.Action{
		{
			Name:        "call_offer",
			Description: "Ring the device with a WebRTC call from Home Assistant; answers ringing, busy or refused.",
			Args: []esphome.Arg{
				{
					Name:        "session",
					Type:        esphome.ArgString,
					Description: "The caller's id for this call.",
				},
				{Name: "sdp", Type: esphome.ArgString, Description: "The caller's WebRTC offer."},
				{Name: "video", Type: esphome.ArgBool},
				{
					Name:        "from",
					Type:        esphome.ArgString,
					Description: "Shown as the caller; empty shows Home Assistant.",
				},
				{Name: "width", Type: esphome.ArgInt, Description: "The caller's video width."},
				{Name: "height", Type: esphome.ArgInt, Description: "The caller's video height."},
				{
					Name:        "user",
					Type:        esphome.ArgString,
					Description: "The calling Home Assistant user's id.",
				},
			},
			Answers: true,
			Run:     c.homeOffer,
		},
		{
			Name:        "call_end",
			Description: "Hang up the call with this session.",
			Args:        []esphome.Arg{{Name: "session", Type: esphome.ArgString}},
			Run:         c.homeEnd,
		},
	}
}

func (c *Calls) homeOffer(call esphome.Call) (any, error) {
	session, sdp := call.String("session"), call.String("sdp")
	if session == "" || sdp == "" {
		return map[string]any{"status": "refused"}, nil
	}
	name := call.String("from")
	if name == "" {
		name = "Home Assistant"
	}
	status := c.incoming(
		envelope{
			Call:   session,
			SDP:    sdp,
			Video:  call.Bool("video"),
			Width:  call.Int("width"),
			Height: call.Int("height"),
			User:   call.String("user"),
		},
		discovery.Peer{ID: homeCaller, Name: name},
		homeLine{home: c.options.Home, session: session},
	)
	switch status {
	case http.StatusAccepted:
		return map[string]any{"status": "ringing"}, nil
	case http.StatusConflict:
		return map[string]any{"status": "busy"}, nil
	}
	return map[string]any{"status": "refused"}, nil
}

func (c *Calls) homeEnd(call esphome.Call) (any, error) {
	c.peerEnded(call.String("session"), ReasonHangup)
	return nil, nil
}
