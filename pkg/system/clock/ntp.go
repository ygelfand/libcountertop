package clock

import (
	"context"
	"errors"
	"time"

	"github.com/beevik/ntp"
)

const (
	timeout = 5 * time.Second
	precise = 10 * time.Millisecond
)

var pool = []string{"0.pool.ntp.org", "1.pool.ntp.org", "2.pool.ntp.org"}

func (c *Clock) sync(ctx context.Context) error {
	err := errors.New("clock: no time servers")
	for _, server := range c.servers() {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		var r *ntp.Response
		if r, err = ntp.QueryWithOptions(server, ntp.QueryOptions{Timeout: timeout}); err != nil {
			continue
		}
		if err = r.Validate(); err != nil {
			continue
		}
		return c.accept(r.ClockOffset, precise, server)
	}
	return err
}

func (c *Clock) servers() []string {
	if c.options.Servers == nil {
		return pool
	}
	offered := c.options.Servers()
	if len(offered) == 0 {
		return pool
	}

	servers := make([]string, 0, len(offered))
	for _, ip := range offered {
		servers = append(servers, ip.String())
	}
	return servers
}
