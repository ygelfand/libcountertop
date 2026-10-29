package callcmd

import (
	"fmt"
	"strings"
	"time"

	call "github.com/ygelfand/libcountertop/pkg/media/call"
	"github.com/ygelfand/libcountertop/pkg/network/peer"
)

func Dial(c *call.Calls, peers []peer.Peer, args []string) (string, error) {
	want := strings.ToLower(args[0])
	for _, p := range peers {
		if p.ID == want || strings.ToLower(p.Name) == want {
			if err := c.Start(p, len(args) > 1 && args[1] == "video"); err != nil {
				return "", err
			}
			return "calling " + p.Name, nil
		}
	}
	return "", fmt.Errorf("no device %q on the network", args[0])
}

func State(controller *call.Calls, _ []string) (string, error) {
	c, ok := controller.Now()
	if !ok {
		return "idle", nil
	}
	out := fmt.Sprintf(
		"%s\t%s\t%s\tvideo=%v\tmuted=%v\tcamera_off=%v\t%s",
		c.State,
		c.Peer.Name,
		c.ID,
		c.Video,
		c.Muted,
		c.CameraOff,
		time.Since(c.Since).Round(time.Second),
	)
	st, ok := controller.Stats()
	if !ok {
		return out, nil
	}
	l := st.Link
	out += fmt.Sprintf(
		"\naudio\tsent=%d recv=%d lost=%d encode=%s",
		l.AudioSent,
		l.AudioRecv,
		l.AudioLost,
		l.EncodeAverage(),
	)
	out += fmt.Sprintf(
		"\nvideo\tsent=%d recv=%d keys=%d waiting=%d key_requests=%d key_asked=%d",
		l.VideoSent,
		l.VideoRecv,
		l.VideoKeys,
		l.VideoWaiting,
		l.KeysRequested,
		l.KeysAsked,
	)
	for _, side := range []struct {
		name string
		s    call.LayerStats
	}{{"remote", st.Remote}, {"self", st.Self}} {
		out += fmt.Sprintf(
			"\n%s\topen=%v %dx%d frames=%d failed=%v",
			side.name,
			side.s.Open,
			side.s.Width,
			side.s.Height,
			side.s.Frames,
			side.s.Failed,
		)
	}
	return out, nil
}
