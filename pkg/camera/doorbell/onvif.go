package doorbell

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ygelfand/libcountertop/pkg/camera/onvif"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func onvifDevice(ctx context.Context, door schema.DoorbellDoor) (*onvif.Client, error) {
	if door.Host == "" {
		return nil, errUnsupported
	}
	device, err := onvif.Find(ctx, door.Host)
	if err != nil {
		return nil, errors.Join(ErrUnreachable, err)
	}
	c, err := onvif.Dial(ctx, device, door.User, door.Password)
	if err != nil && !errors.Is(err, onvif.ErrRefused) {
		return nil, errors.Join(ErrUnreachable, err)
	}
	return c, err
}

func streamURI(ctx context.Context, door schema.DoorbellDoor) (string, error) {
	c, err := onvifDevice(ctx, door)
	if err != nil {
		return "", err
	}
	profiles, err := channelProfiles(ctx, c, door.Channel)
	if err != nil {
		return "", err
	}
	uri, err := c.StreamURI(ctx, pickProfile(profiles, door.Stream).Token)
	if err != nil {
		return "", errors.Join(ErrUnreachable, err)
	}
	return uri, nil
}

func channelProfiles(
	ctx context.Context,
	c *onvif.Client,
	channel int,
) ([]onvif.MediaProfile, error) {
	profiles, err := c.Profiles(ctx)
	if err != nil {
		return nil, errors.Join(ErrUnreachable, err)
	}
	sources, err := c.VideoSources(ctx)
	if err != nil || len(sources) <= 1 {
		return profiles, nil
	}
	if channel < 0 || channel >= len(sources) {
		return nil, fmt.Errorf(
			"doorbell: channel %d is not one of the %d video sources",
			channel,
			len(sources),
		)
	}
	var mine []onvif.MediaProfile
	for _, p := range profiles {
		if p.Source == sources[channel] {
			mine = append(mine, p)
		}
	}
	if len(mine) == 0 {
		return nil, fmt.Errorf("doorbell: no profiles for video source %q", sources[channel])
	}
	return mine, nil
}

func ownNotice(n onvif.Notice, profiles []onvif.MediaProfile) bool {
	if len(n.Sources) == 0 {
		return true
	}
	for _, s := range n.Sources {
		for _, p := range profiles {
			if s == p.Source || s == p.Config || s == p.Token {
				return true
			}
		}
	}
	return false
}

func pickProfile(profiles []onvif.MediaProfile, s schema.DoorbellStream) onvif.MediaProfile {
	best := profiles[0]
	for _, p := range profiles[1:] {
		bigger := p.Width*p.Height > best.Width*best.Height
		if bigger == (s != schema.DoorbellSub) {
			best = p
		}
	}
	return best
}

func (d *Doorbell) onvifEvents(ctx context.Context, slot int, door schema.DoorbellDoor) error {
	c, err := onvifDevice(ctx, door)
	if err != nil {
		d.markFailure(slot, err)
		return err
	}
	profiles, err := channelProfiles(ctx, c, door.Channel)
	if err != nil {
		d.markFailure(slot, err)
		return err
	}
	pull, err := c.Subscribe(ctx)
	if err != nil {
		d.markFailure(slot, err)
		return err
	}
	defer func() {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), snapWait)
		defer cancel()
		_ = pull.Close(cctx)
	}()
	d.via(slot, sourceOnvif, "")
	d.mark(slot, Ready)
	active := map[Kind]bool{}
	renewed := time.Now()
	for ctx.Err() == nil {
		if time.Since(renewed) >= onvif.RenewEvery() {
			if err := pull.Renew(ctx); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				d.mark(slot, Unreachable)
				return errors.Join(ErrUnreachable, err)
			}
			renewed = time.Now()
		}
		notes, err := pull.Pull(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			d.mark(slot, Unreachable)
			return errors.Join(ErrUnreachable, err)
		}
		for _, n := range notes {
			k, ok := topicKind(n.Topic)
			if !ok || !ownNotice(n, profiles) {
				continue
			}
			if n.On && !active[k] {
				go d.record(ctx, slot, door, k)
			}
			active[k] = n.On
		}
	}
	return nil
}

func (d *Doorbell) markFailure(slot int, err error) {
	switch {
	case errors.Is(err, onvif.ErrRefused):
		d.mark(slot, BadLogin)
	case errors.Is(err, errUnsupported):
		d.mark(slot, Unsupported)
	default:
		d.mark(slot, Unreachable)
	}
}

func topicKind(topic string) (Kind, bool) {
	t := strings.ToLower(topic)
	switch {
	case strings.Contains(t, "visitor"):
		return Visitor, true
	case strings.Contains(t, "people"), strings.Contains(t, "person"), strings.Contains(t, "human"):
		return Person, true
	case strings.Contains(t, "package"):
		return Package, true
	case strings.Contains(t, "vehicle"):
		return Vehicle, true
	case strings.Contains(t, "dog"), strings.Contains(t, "animal"), strings.Contains(t, "pet"):
		return Animal, true
	}
	return "", false
}
