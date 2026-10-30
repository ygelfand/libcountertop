package satellite

import (
	"context"
	"log/slog"
	"sync"

	esphome "github.com/ygelfand/go-esphome-device"
	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/libcountertop/pkg/assistant/turn"
	"github.com/ygelfand/libcountertop/pkg/inference/wakeslots"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
)

// Announce and StartConversation both require a media player.
const Features = esphome.DefaultVoiceFeatures |
	esphome.FeatureSpeaker |
	esphome.FeatureAnnounce |
	esphome.FeatureStartConversation |
	esphome.FeatureTimers

type Timers interface {
	Event(esphome.TimerEvent)
	Forget()
	Ringing() bool
	Stop() bool
}

type Alarms interface {
	Ringing() bool
	Stop() bool
	Snooze() bool
}

type Sound interface {
	Busy() bool
	Silence()
}

type Player interface {
	Playing() (playing, paused bool)
	Pause()
}

type Options struct {
	// OnTrace observes subscription and accepted-turn categories only: no text
	// or audio. Set before New; callbacks may run concurrently and must not
	// block or re-enter the satellite. Slots are zero-based, or -1 for API events.
	OnTrace func(kind string, slot int)
	Slots   int
	Initial []string
	Timers  Timers
	Alarms  Alarms
	Sound   Sound
	Player  Player
	Save    func(slot int, id string) error
}

type Satellite struct {
	vs        *esphome.VoiceSatellite
	turn      *turn.Conversation
	selection wakeslots.Selection
	options   Options
}

func New(vs *esphome.VoiceSatellite, deps turn.Dependencies, options Options) *Satellite {
	s := &Satellite{vs: vs, options: options}
	s.selection.Set(options.Initial)
	vs.ActiveWakeWords = wakeslots.Advertised(options.Initial)
	vs.MaxActiveWakeWords = uint32(options.Slots)

	deps.Word = s.selection.ID
	s.turn = turn.NewConversation(vs, deps)
	s.turn.OnTrace = options.OnTrace

	vs.OnTimer = options.Timers.Event
	vs.OnSubscribed = func(subscribed bool) {
		if options.OnTrace != nil {
			kind := "api_unsubscribed"
			if subscribed {
				kind = "api_subscribed"
			}
			options.OnTrace(kind, -1)
		}
		if !subscribed {
			options.Timers.Forget()
		}
	}
	return s
}

func (s *Satellite) Name() string { return "conversation" }

var _ esphome.ConnCloser = (*Satellite)(nil)

func (s *Satellite) Handle(ctx context.Context, c *esphome.Conn, msg proto.Message) error {
	return s.vs.Handle(ctx, c, msg)
}

func (s *Satellite) ConnClosed(c *esphome.Conn) { s.vs.ConnClosed(c) }

func (s *Satellite) Run(ctx context.Context) error {
	s.turn.Run(ctx)
	return nil
}

func (s *Satellite) Ready() bool { return s.vs.Subscribed() }

func (s *Satellite) Turn() *turn.Conversation { return s.turn }

func (s *Satellite) Start(slot int) { s.turn.Start(slot) }

func (s *Satellite) AfterTurn(fn func()) {
	var (
		once sync.Once
		mu   sync.Mutex
		stop func()
	)
	run := func() {
		once.Do(func() { safe.Go("after turn", fn) })
		mu.Lock()
		defer mu.Unlock()
		if stop != nil {
			stop()
		}
	}
	mu.Lock()
	stop = s.turn.Ended.Listen(func(struct{}) { run() })
	mu.Unlock()
	if !s.turn.Busy() {
		run()
	}
}

func (s *Satellite) Listen() (release func()) { return s.turn.Listen() }

func (s *Satellite) Action() {
	if s.Stop() {
		return
	}

	// Home Assistant runs the first pipeline for a wake that reports no phrase.
	s.turn.Start(0)
}

func (s *Satellite) ActionHold(slot int) {
	if s.alarmRinging() {
		if !s.options.Alarms.Snooze() {
			s.options.Alarms.Stop()
		}
		return
	}
	s.turn.Start(slot)
}

func (s *Satellite) alarmRinging() bool {
	return s.options.Alarms != nil && s.options.Alarms.Ringing()
}

func (s *Satellite) Interrupt() {
	if s.turn.Phase() == turn.Listening {
		slog.Debug("stop word ignored, the turn is listening")
		return
	}
	if !s.options.Sound.Busy() && !s.options.Timers.Ringing() && !s.alarmRinging() {
		if playing, _ := s.options.Player.Playing(); !playing {
			slog.Debug("stop word ignored, nothing to stop")
			return
		}
	}
	s.Stop()
}

func (s *Satellite) Stop() bool {
	if s.options.Alarms != nil && s.options.Alarms.Stop() {
		return true
	}
	if s.options.Timers.Stop() {
		return true
	}

	if s.turn.Busy() {
		s.turn.Cancel()
		return true
	}

	if s.options.Sound.Busy() {
		s.options.Sound.Silence()
		return true
	}

	if playing, _ := s.options.Player.Playing(); playing {
		s.options.Player.Pause()
		return true
	}
	return false
}

// Home Assistant takes the echoed list as authoritative and reverts a slot missing from it.
func (s *Satellite) OnWakeWord(load func(ids []string) []string, selected func()) {
	s.vs.OnSetActiveWakeWords = func(ids []string) {
		accepted := load(ids)
		s.SetSlots(accepted)

		for slot := range s.options.Slots {
			id := ""
			if slot < len(accepted) {
				id = accepted[slot]
			}
			if err := s.options.Save(slot, id); err != nil {
				slog.Error("saving the wake word failed", "slot", slot+1, "err", err)
			}
		}
		if len(wakeslots.Advertised(accepted)) != len(wakeslots.Advertised(ids)) {
			slog.Warn("some wake words were refused", "asked", ids, "running", accepted)
		}
		if selected != nil {
			selected()
		}
	}
}

func (s *Satellite) Slots() []string { return s.selection.Get() }

func (s *Satellite) SetSlots(slots []string) {
	s.selection.Set(slots)
	s.vs.ActiveWakeWords = wakeslots.Advertised(slots)
	slog.Info("wake words listening", "slots", slots)
}
