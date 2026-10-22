package sendspin

import (
	"context"
	"hash/maphash"
	"log/slog"
	"sync"
	"time"

	"github.com/Sendspin/sendspin-go/pkg/protocol"
	esphome "github.com/ygelfand/go-esphome-device"

	core "github.com/ygelfand/libcountertop/pkg/audio/sendspin"
	"github.com/ygelfand/libcountertop/pkg/media/nowplaying"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

type Player struct {
	options Dependencies
	workers sync.WaitGroup
	knobs   *Controls
	state   *esphome.TextSensor

	out *core.Output

	mu      sync.Mutex
	running context.CancelFunc
	wake    chan struct{}

	joined *core.Session

	playing track
	group   string

	artwork []byte

	at        time.Duration
	length    time.Duration
	serverAt  int64
	speed     int
	serverNow func() int64
}

func New(options Dependencies) *Player {
	p := &Player{options: options,
		out:  options.Output,
		wake: make(chan struct{}, 1),
	}

	p.knobs = &Controls{
		Table:  NewTable(),
		Device: options.DeviceID,
		Read:   options.Settings.Read,
		Save:   p.save,
	}

	p.state = &esphome.TextSensor{
		Base: esphome.Base{
			ObjectID: "sendspin_state",
			Name:     "Sendspin state",
			Icon:     "mdi:lan-connect",
			Category: esphome.CategoryDiagnostic,
			DeviceID: options.DeviceID,
		},
	}
	p.state.Set(stateOff)

	return p
}

func (p *Player) stepped(by time.Duration) {
	p.mu.Lock()
	joined := p.joined
	p.mu.Unlock()
	if joined == nil {
		return
	}
	slog.Info(
		"the clock was stepped, starting the sendspin session again",
		"by",
		by.Round(time.Millisecond),
	)
	joined.Close()
}

const (
	stateOff     = "off"
	stateWaiting = "waiting"
	stateJoined  = "joined"
	statePlaying = "playing"
)

func (p *Player) Name() string { return "sendspin" }

func (p *Player) Entities() []esphome.Entity {
	return append(p.knobs.Entities(), p.state)
}

func (p *Player) Settings() *Controls { return p.knobs }

func (p *Player) Play()  { p.tell("play") }
func (p *Player) Pause() { p.tell("pause") }
func (p *Player) Stop()  { p.tell("stop", "pause") }

func (p *Player) Next()     { p.tell("next") }
func (p *Player) Previous() { p.tell("previous") }

func (p *Player) tell(want ...string) {
	p.mu.Lock()
	s := p.joined
	p.mu.Unlock()

	if s == nil {
		return
	}

	safe.Go("sendspin command", func() { s.Command(want...) })
}

func (p *Player) Playing() (playing, paused bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	paused = p.group == "paused" || p.group == "stopped" && p.playing.Title != ""
	return p.state.Get() == statePlaying && !paused, paused
}

func (*Player) Kind() nowplaying.Kind { return nowplaying.FromGroup }

func (*Player) Label() string { return "" }

func (p *Player) Now() nowplaying.Now {
	playing, paused := p.Playing()

	p.mu.Lock()
	defer p.mu.Unlock()

	return nowplaying.Now{
		Playing: playing,
		Paused:  paused,
		Title:   p.playing.Title,
		Artist:  p.playing.Artist,
		Album:   p.playing.Album,
		Art:     p.artwork,
		ArtID:   artID(p.artwork),
		Elapsed: p.reached(playing),
		Length:  p.length,
		Can:     nowplaying.CanPause | nowplaying.CanNext | nowplaying.CanPrevious,
	}
}

func (p *Player) reached(playing bool) time.Duration {
	at := p.at
	if playing && p.serverNow != nil && p.speed > 0 {
		at += time.Duration((p.serverNow()-p.serverAt)*int64(p.speed)/1000) * time.Microsecond
	}
	if p.length > 0 && at > p.length {
		return p.length
	}
	return max(at, 0)
}

func (p *Player) moved(progress protocol.ProgressState, stamp int64, serverNow func() int64) {
	if stamp == 0 {
		stamp = serverNow()
	}
	p.mu.Lock()
	p.at = time.Duration(progress.TrackProgress) * time.Millisecond
	p.length = time.Duration(progress.TrackDuration) * time.Millisecond
	p.serverAt, p.speed, p.serverNow = stamp, progress.PlaybackSpeed, serverNow
	at, length := p.at, p.length
	p.mu.Unlock()

	slog.Debug("sendspin progress", "at", at, "length", length, "speed", progress.PlaybackSpeed,
		"stamp_lead_ms", (stamp-serverNow())/1000)
	p.changed()
}

func (p *Player) holds(s *core.Session) {
	p.mu.Lock()
	p.joined = s
	p.group = ""
	p.playing = track{}
	p.mu.Unlock()
	p.changed()

	if s == nil {
		p.options.Media.Ended(p)
		p.options.Media.Release(p)
	}
}

func (p *Player) setState(state string) {
	p.state.Set(state)

	if state == statePlaying {
		p.options.Media.External(p)
		return
	}
	p.changed()
}

func (p *Player) grouped(state string) {
	p.mu.Lock()
	p.group = state
	p.mu.Unlock()
	p.changed()
}

func (p *Player) track() track {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}

func (p *Player) plays(t track) {
	p.mu.Lock()
	if t != p.playing {
		p.at, p.length, p.serverAt, p.speed = 0, 0, 0, 0
	}
	p.playing = t
	p.mu.Unlock()
	p.changed()

	if !t.empty() {
		slog.Info("sendspin now playing", "title", t.Title, "artist", t.Artist, "album", t.Album)
	}
}

func (p *Player) drew(art protocol.ArtworkChunk) {
	p.mu.Lock()
	p.artwork = append([]byte(nil), art.Data...)
	p.mu.Unlock()
	p.changed()
}
func (p *Player) Artwork() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]byte(nil), p.artwork...)
}

var artSeed = maphash.MakeSeed()

func artID(art []byte) uint64 {
	if len(art) == 0 {
		return 0
	}
	return maphash.Bytes(artSeed, art)
}

func (p *Player) Restore(c schema.Sendspin) { p.knobs.PublishFrom(c) }

func (p *Player) Run(ctx context.Context) error {
	unwatch := p.options.Clock.WatchSteps(p.stepped)
	defer unwatch()
	defer p.stop()

	for {
		p.settle(ctx)

		select {
		case <-ctx.Done():
			return nil
		case <-p.wake:
		}
	}
}

func (p *Player) SetEnabled(on bool) {
	if err := p.knobs.Change("sendspin", setting.OnOff(on)); err != nil {
		slog.Error("saving a setting failed", "setting", "media.sendspin", "err", err)
	}
}

func (p *Player) save(s Setting, v string) error {
	if err := setting.Store(p.options.Settings, s, v); err != nil {
		return err
	}
	p.rethink()
	return nil
}

func (p *Player) rethink() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Player) settle(parent context.Context) {
	want := p.options.Settings.Read().Enabled

	p.mu.Lock()
	already := p.running != nil
	p.mu.Unlock()

	switch {
	case want && already, !want && !already:
		return
	case !want:
		p.stop()
		p.state.Set(stateOff)
		return
	}

	ctx, cancel := context.WithCancel(parent)
	p.mu.Lock()
	p.running = cancel
	p.mu.Unlock()

	name := p.options.Device.Name()
	if p.options.OpenPort != nil {
		p.options.OpenPort()
	}
	l := p.listener()

	p.workers.Add(2)
	safe.Go("sendspin listen", func() {
		defer p.workers.Done()
		if err := l.Serve(ctx, name); err != nil {
			slog.Error("sendspin listener stopped", "err", err)
		}
	})
	safe.Go(
		"sendspin advertise",
		func() { defer p.workers.Done(); advertise(ctx, name, core.Port) },
	)

	p.state.Set(stateWaiting)
	slog.Info("sendspin waiting for a server", "name", name, "port", core.Port)
}

func (p *Player) stop() {
	p.mu.Lock()
	cancel := p.running
	p.running = nil
	p.mu.Unlock()

	if cancel == nil {
		return
	}
	cancel()
	p.workers.Wait()
}

type Media interface {
	Began(nowplaying.Source)
	Ended(nowplaying.Source)
	Release(nowplaying.Source)
	External(nowplaying.Source)
	Changed()
}
type Clock interface {
	WatchSteps(func(time.Duration)) func()
}
type Device interface {
	Name() string
	Identity() core.Identity
}
type Dependencies struct {
	Output      *core.Output
	Arbitration core.Arbitration
	Device      Device
	Settings    storage.Section[schema.Sendspin]
	DeviceID    uint32
	Clock       Clock
	OpenPort    func()
	Media       Media
}

func (p *Player) listener() *core.Listener {
	return core.NewListener(
		p.out,
		p.options.Arbitration,
		p.options.Device.Identity,
		core.Callbacks{
			State:      p.setState,
			Connection: p.holds,
			Began:      func() { p.options.Media.Began(p) },
			Grouped:    p.grouped,
			Artwork:    p.drew,
			Metadata: func(m *protocol.MetadataState, now func() int64) {
				p.plays(p.track().merge(m))
				if m.Progress != nil {
					p.moved(*m.Progress, m.Timestamp, now)
				}
			},
		},
	)
}

const ArtworkSize = 512

func (p *Player) changed() {
	if p.options.Media != nil {
		p.options.Media.Changed()
	}
}
