package call

import (
	"log/slog"
	"slices"
	"strings"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/audio/tone"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type Setting = setting.Setting[config.Call]

const Group setting.Group = "Calls"

type entities struct {
	knobs *setting.Controls[config.Call]
	state *esphome.TextSensor
	peer  *esphome.TextSensor
}

func NewTable(more func() []setting.Option) *setting.Table[config.Call] {
	chime := func(name, icon string, field func(*config.Call) *config.Chime) Setting {
		return setting.Pick(
			Setting{Name: name, Group: Group, Icon: icon, More: more},
			config.CallChimes(),
			field,
		)
	}
	return setting.NewTable("call", []setting.Group{Group}, []Setting{
		setting.Flag(
			Setting{Name: "incoming", Group: Group, Icon: "mdi:phone-incoming"},
			func(c *config.Call) *bool { return &c.Incoming },
		),
		setting.Flag(
			Setting{Name: "broadcasts", Group: Group, Icon: "mdi:bullhorn"},
			func(c *config.Call) *bool { return &c.Broadcasts },
		),
		setting.Flag(
			Setting{Name: "auto_answer", Group: Group, Icon: "mdi:phone-in-talk"},
			func(c *config.Call) *bool { return &c.AutoAnswer },
		),
		setting.Flag(
			Setting{Name: "pause_wake", Group: Group, Icon: "mdi:microphone-off"},
			func(c *config.Call) *bool { return &c.PauseWake },
		),
		setting.Flag(
			Setting{Name: "auto_video", Group: Group, Icon: "mdi:video"},
			func(c *config.Call) *bool { return &c.AutoVideo },
		),
		setting.Pick(
			Setting{Name: "stream", Group: Group, Icon: "mdi:video-box"},
			config.CallStreams(),
			func(c *config.Call) *config.CallStream { return &c.Stream },
		),
		setting.Pick(
			Setting{Name: "answer_from", Group: Group, Icon: "mdi:account-check"},
			config.CallAudiences(),
			func(c *config.Call) *config.CallAudience { return &c.AnswerFrom },
		),
		setting.Words(
			Setting{Name: "answer_peers", Group: Group, Icon: "mdi:account-multiple"},
			255,
			func(c *config.Call) *string { return &c.AnswerPeers },
		),
		setting.Words(
			Setting{Name: "answer_users", Group: Group, Icon: "mdi:account-group"},
			255,
			func(c *config.Call) *string { return &c.AnswerUsers },
		),
		setting.Flag(
			Setting{Name: "answer_api", Group: Group, Icon: "mdi:api"},
			func(c *config.Call) *bool { return &c.AnswerAPI },
		),
		chime("ring", "mdi:bell-ring", func(c *config.Call) *config.Chime { return &c.Ring }),
		chime("video_ring", "mdi:bell-ring-outline",
			func(c *config.Call) *config.Chime { return &c.VideoRing }),
		chime("ringback", "mdi:phone-outgoing",
			func(c *config.Call) *config.Chime { return &c.Ringback }),
	}, setting.Messages{Text: say.T, Missing: say.Missing})
}

func (c *Calls) build() {
	c.ha = entities{
		knobs: &setting.Controls[config.Call]{
			Table: NewTable(c.Tones),
			Read:  c.options.Settings.Read,
			Save:  c.store,
		},
		state: &esphome.TextSensor{
			Base: esphome.Base{ObjectID: "call_state", Name: "Call", Icon: "mdi:phone"},
		},
		peer: &esphome.TextSensor{
			Base: esphome.Base{ObjectID: "call_peer", Name: "Call with", Icon: "mdi:account-voice"},
		},
	}
}

func (c *Calls) Controls() *setting.Controls[config.Call] { return c.ha.knobs }

func (c *Calls) Entities() []esphome.Entity {
	return append(c.ha.knobs.Entities(), c.ha.state, c.ha.peer)
}

func (c *Calls) Restore(config.Call) {
	c.ha.knobs.Publish()
	c.publish(nil)
}

func (c *Calls) publish(s *session) {
	if s == nil {
		c.ha.state.Set(Idle.String())
		c.ha.peer.Set("")
		return
	}
	c.mu.Lock()
	state, peer := s.State, s.Peer.Name
	c.mu.Unlock()
	c.ha.state.Set(state.String())
	c.ha.peer.Set(peer)
}

func (c *Calls) store(s Setting, v string) error {
	err := setting.Store(c.options.Settings, s, v)
	c.redraw()
	return err
}

func (c *Calls) set(name, v string) {
	if err := c.ha.knobs.Change(name, v); err != nil {
		slog.Error("saving a call setting failed", "setting", name, "err", err)
		c.ha.knobs.Publish()
	}
}

func (c *Calls) SetIncoming(on bool) { c.set("incoming", setting.OnOff(on)) }

func (c *Calls) SetAutoAnswer(on bool) { c.set("auto_answer", setting.OnOff(on)) }

func (c *Calls) SetPauseWake(on bool) { c.set("pause_wake", setting.OnOff(on)) }

func (c *Calls) SetAutoVideo(on bool) { c.set("auto_video", setting.OnOff(on)) }

func (c *Calls) SetStream(v config.CallStream) { c.set("stream", string(v)) }

func (c *Calls) SetAnswerFrom(v config.CallAudience) { c.set("answer_from", string(v)) }

func (c *Calls) SetAnswerAPI(on bool) { c.set("answer_api", setting.OnOff(on)) }

func (c *Calls) ToggleAnswerPeer(id string) {
	peers := c.options.Settings.Read().Peers()
	if i := slices.Index(peers, id); i >= 0 {
		peers = slices.Delete(peers, i, i+1)
	} else {
		peers = append(peers, id)
	}
	c.set("answer_peers", strings.Join(peers, " "))
}

func (c *Calls) ToggleAnswerUser(id string) {
	users := c.options.Settings.Read().Users()
	if i := slices.Index(users, id); i >= 0 {
		users = slices.Delete(users, i, i+1)
	} else {
		users = append(users, id)
	}
	c.set("answer_users", strings.Join(users, " "))
}

func (c *Calls) SetRing(v config.Chime) { c.set("ring", string(v)) }

func (c *Calls) SetVideoRing(v config.Chime) { c.set("video_ring", string(v)) }

func (c *Calls) SetRingback(v config.Chime) { c.set("ringback", string(v)) }

func (c *Calls) Tones() []setting.Option {
	if c.options.Tones == nil {
		return nil
	}
	return c.options.Tones.Choices()
}

func (c *Calls) Preview(v config.Chime) {
	c.options.Sound.Tone(config.StreamAlerts, ringLevel, c.tone(v, config.ChimeNone))
}

func (c *Calls) tone(v, fallback config.Chime) tone.Sound {
	if c.options.Tones == nil {
		if _, custom := v.Custom(); custom {
			v = fallback
		}
		return tone.Sound{Notes: tone.Wake(v)}
	}
	return c.options.Tones.Sound(v, fallback)
}
