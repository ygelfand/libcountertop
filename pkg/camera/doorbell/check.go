package doorbell

import (
	"context"
	"errors"

	"github.com/ygelfand/libcountertop/pkg/camera/onvif"
	"github.com/ygelfand/libcountertop/pkg/media/rtsp"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func Check(ctx context.Context, door schema.DoorbellDoor) ([]byte, error) {
	video := plannedVideo(door)
	if video == sourceAssistant {
		return nil, nil
	}
	if !door.Signed() && video != sourceRTSP {
		return nil, ErrNoPassword
	}
	ctx, cancel := context.WithTimeout(ctx, snapWait)
	defer cancel()
	if video == sourceReolink {
		return reolinkCheck(ctx, door)
	}
	return nil, rtspCheck(ctx, door)
}

func rtspCheck(ctx context.Context, door schema.DoorbellDoor) error {
	uri, err := streamURI(ctx, door)
	if errors.Is(err, onvif.ErrRefused) {
		return errors.Join(ErrBadLogin, err)
	}
	if err != nil {
		return err
	}
	c, err := rtsp.Dial(ctx, uri, rtsp.Options{User: door.User, Password: door.Password})
	if errors.Is(err, rtsp.ErrRefused) {
		return errors.Join(ErrBadLogin, err)
	}
	if err != nil {
		return errors.Join(ErrUnreachable, err)
	}
	return c.Close()
}
