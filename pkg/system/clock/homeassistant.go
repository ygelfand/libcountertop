package clock

import (
	"context"
	"log/slog"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"
	"github.com/ygelfand/go-esphome-device/api"
	"google.golang.org/protobuf/proto"
)

// Home Assistant answers in whole seconds.
const homeResolution = time.Second

func (c *Clock) Handle(_ context.Context, conn *esphome.Conn, msg proto.Message) error {
	if reply, ok := msg.(*api.GetTimeResponse); ok {
		c.fromHome(reply.GetEpochSeconds())
		c.zone.FromHome(reply.GetTimezone())
		return nil
	}

	if c.shouldAsk(conn) {
		if err := conn.Send(&api.GetTimeRequest{}); err != nil {
			slog.Debug("asking Home Assistant the time failed", "err", err)
		}
	}
	return nil
}

func (c *Clock) shouldAsk(conn *esphome.Conn) bool {
	if conn == nil {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if conn == c.asked {
		return false
	}
	c.asked = conn
	return true
}

func (c *Clock) fromHome(epoch uint32) {
	if epoch == 0 {
		return
	}

	offset := time.Until(time.Unix(int64(epoch), 0))
	if err := c.accept(offset, homeResolution, "home assistant"); err != nil {
		slog.Warn("could not take the time from Home Assistant", "err", err)
	}
}
