package youtube

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ygelfand/libcountertop/pkg/media/cast"
)

const (
	NSMDX          = "urn:x-cast:com.google.youtube.mdx"
	NSCAC          = "urn:x-cast:com.google.cast.cac"
	NSDebugOverlay = "urn:x-cast:com.google.cast.debugoverlay"
)

const Name = "youtube"

const (
	typeGetStatus = "getMdxSessionStatus"
	typeStatus    = "mdxSessionStatus"
)

type Protocol struct {
	screens func(app string) (screen, device string, ok bool)
	keeper  *screens
}

func Maker(env cast.Env) cast.Protocol { return New(env) }

func New(env cast.Env) *Protocol {
	k := newScreens(env)
	return &Protocol{screens: k.screen, keeper: k}
}

func (p *Protocol) Run(ctx context.Context) {
	if p.keeper != nil {
		p.keeper.run(ctx)
	}
}

func (p *Protocol) Name() string { return Name }

func (p *Protocol) Namespaces() []string { return []string{NSMDX, NSCAC, NSDebugOverlay} }

type sessionStatus struct {
	Type string     `json:"type"`
	Data statusData `json:"data"`
}

type statusData struct {
	ScreenID string `json:"screenId"`
	DeviceID string `json:"deviceId"`
}

func (p *Protocol) Receive(app *cast.Application, m cast.Message) ([]cast.Message, error) {
	if m.Namespace != NSMDX {
		return nil, cast.ErrUnspoken
	}
	var h struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(m.Payload), &h); err != nil {
		return nil, fmt.Errorf("youtube: an mdx message: %w", err)
	}
	if h.Type != typeGetStatus {
		return nil, cast.ErrUnspoken
	}

	if p.keeper != nil {
		p.keeper.wake(app.AppID)
	}
	screen, device, ok := p.screens(app.AppID)
	if !ok {
		return nil, cast.ErrUnspoken
	}
	body, err := json.Marshal(
		sessionStatus{Type: typeStatus, Data: statusData{ScreenID: screen, DeviceID: device}},
	)
	if err != nil {
		return nil, err
	}
	return []cast.Message{cast.Reply(m, NSMDX, string(body))}, nil
}
