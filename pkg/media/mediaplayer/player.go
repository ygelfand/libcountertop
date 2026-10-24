package mediaplayer

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/audio/sound"
	"github.com/ygelfand/libcountertop/pkg/audio/upsample"
	"github.com/ygelfand/libcountertop/pkg/hook"
	"github.com/ygelfand/libcountertop/pkg/media/nowplaying"
	"github.com/ygelfand/libcountertop/pkg/media/ownership"
	"github.com/ygelfand/libcountertop/pkg/media/pcm"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
)

type Speaker interface {
	sound.Player
	pcm.Sink
	PlayVoice(mono []int16)
}

type Volume interface {
	SetVolume(fraction float32)
	StepVolume(delta int)
	Mute(on bool)
}

type Options[P Speaker] struct {
	Speaker        P
	Sound          *sound.Driver[P]
	Rate, Channels int
	DuckDB         func() float64
	Volume         Volume
	Changed        func()
	Device         uint32
}

type Player[P Speaker] struct {
	Begun hook.Hook[nowplaying.Source]

	mp     *esphome.MediaPlayer
	title  *esphome.TextSensor
	artist *esphome.TextSensor
	stream *pcm.Stream
	sound  *sound.Driver[P]
	volume Volume
	notify func()

	speaking atomic.Bool

	owner *ownership.Owner[nowplaying.Source]
	queue *queue[P]
}

func Formats(rate, channels int) []esphome.MediaFormat {
	return []esphome.MediaFormat{
		{Format: "wav", SampleRate: uint32(rate), Channels: uint32(channels), SampleBytes: 2},
		{
			Format:       "wav",
			SampleRate:   upsample.VoiceRate,
			Channels:     1,
			SampleBytes:  2,
			Announcement: true,
		},
	}
}

func New[P Speaker](o Options[P]) *Player[P] {
	p := &Player[P]{
		mp: &esphome.MediaPlayer{
			Base: esphome.Base{ObjectID: "speaker", Name: "Speaker", Icon: "mdi:speaker"},
			Features: esphome.MediaPlayerFeatureVolumeSet |
				esphome.MediaPlayerFeatureVolumeStep |
				esphome.MediaPlayerFeatureVolumeMute |
				esphome.MediaPlayerFeaturePlayMedia |
				esphome.MediaPlayerFeatureBrowseMedia |
				esphome.MediaPlayerFeaturePlay |
				esphome.MediaPlayerFeaturePause |
				esphome.MediaPlayerFeatureStop |
				esphome.MediaPlayerFeatureAnnounce,
			SupportedFormats: Formats(o.Rate, o.Channels),
			SupportsPause:    true,
		},
		title: &esphome.TextSensor{Base: esphome.Base{
			ObjectID: "now_playing",
			Name:     "Now playing",
			Icon:     "mdi:music-note",
			DeviceID: o.Device,
		}},
		artist: &esphome.TextSensor{Base: esphome.Base{
			ObjectID: "artist",
			Name:     "Artist",
			Icon:     "mdi:account-music",
			DeviceID: o.Device,
		}},
		sound:  o.Sound,
		volume: o.Volume,
		notify: o.Changed,
		owner: ownership.New(func(s nowplaying.Source) uint8 {
			if s == nil {
				return uint8(nowplaying.FromQueue)
			}
			return uint8(s.Kind())
		}),
	}
	p.queue = &queue[P]{p: p}
	p.mp.OnCommand = p.command
	p.stream = pcm.New(pcm.Options{
		Sink:        o.Speaker,
		Rate:        o.Rate,
		Channels:    o.Channels,
		Changed:     p.Changed,
		DuckDB:      o.DuckDB,
		Arbitration: o.Sound.Backgrounds(),
	})
	p.mp.SetState(esphome.MediaPlayerIdle)
	return p
}

func (p *Player[P]) Entity() *esphome.MediaPlayer { return p.mp }

func (p *Player[P]) Track() []esphome.Entity { return []esphome.Entity{p.title, p.artist} }

func (p *Player[P]) Stream() *pcm.Stream { return p.stream }

func (p *Player[P]) Start(ctx context.Context) { p.stream.Start(ctx) }

func (p *Player[P]) Close() error { return p.stream.Close() }

func (p *Player[P]) command(c esphome.MediaCommand) {
	if c.HasVolume {
		p.volume.SetVolume(c.Volume)
		p.Changed()
	}

	if c.HasMediaURL && c.MediaURL != "" {
		p.play(c.MediaURL, c.Announcement)
	}
	if !c.HasCommand {
		return
	}

	switch c.Command {
	case esphome.MediaPlayerVolumeUp:
		p.volume.StepVolume(1)
		p.Changed()
	case esphome.MediaPlayerVolumeDown:
		p.volume.StepVolume(-1)
		p.Changed()
	case esphome.MediaPlayerMute:
		p.Mute(true)
	case esphome.MediaPlayerUnmute:
		p.Mute(false)
	case esphome.MediaPlayerStop:
		p.Ended(p.queue)
		p.Stop()
	case esphome.MediaPlayerPause:
		p.Pause()
	case esphome.MediaPlayerPlay:
		p.Unpause()
	case esphome.MediaPlayerToggle:
		if playing, _ := p.Playing(); playing {
			p.Pause()
		} else {
			p.Unpause()
		}
	}
}

// Home Assistant serves the url as WAV, at the card's rate for music and the pipeline's for an announcement.
func (p *Player[P]) play(url string, announcement bool) {
	if announcement {
		p.announce(url)
		return
	}
	p.Began(p.queue)
	p.stream.Play(url)
}

func (p *Player[P]) announce(url string) {
	p.Sounding(true)
	claim := p.sound.Claim("announce", func(ctx context.Context, spk P) error {
		samples, err := pcm.Fetch(ctx, url, upsample.VoiceRate)
		if err != nil {
			return err
		}
		spk.PlayVoice(samples)
		spk.PlayVoice(make([]int16, upsample.VoiceRate*pcm.Tail/1000))
		return nil
	})

	safe.Go("announce", func() {
		<-claim.Done()
		p.Sounding(false)

		if err := claim.Err(); err != nil {
			slog.Error("playing the announcement failed", "url", url, "err", err)
		}
	})
}

func (p *Player[P]) Sounding(on bool) {
	p.speaking.Store(on)
	p.Changed()
}

func (p *Player[P]) Mute(on bool) {
	p.mp.SetMuted(on)
	p.volume.Mute(on)
	p.Changed()
}

func (p *Player[P]) Changed() {
	p.mp.SetState(p.state())
	now := p.Now()
	if !now.Playing && !now.Paused {
		now.Title, now.Artist = "", ""
	}
	show(p.title, now.Title)
	show(p.artist, now.Artist)
	if p.notify != nil {
		p.notify()
	}
}

func (p *Player[P]) state() esphome.MediaPlayerState {
	playing, paused := p.Playing()

	switch {
	case playing || p.speaking.Load():
		return esphome.MediaPlayerPlaying
	case paused:
		return esphome.MediaPlayerPaused
	default:
		return esphome.MediaPlayerIdle
	}
}

func (p *Player[P]) Began(s nowplaying.Source) {
	replaced, fresh, displaced := p.owner.Begin(s)
	p.Changed()
	if fresh {
		p.Begun.Emit(s)
	}
	if displaced {
		replaced.Stop()
	}
}

func (p *Player[P]) Present(s nowplaying.Source) { p.Begun.Emit(s) }

func (p *Player[P]) Ended(s nowplaying.Source) { p.owner.End(s) }

func (p *Player[P]) External(s nowplaying.Source) {
	if s == nil {
		p.owner.Clear()
	} else {
		p.owner.Set(s)
	}
	p.Changed()
}

func (p *Player[P]) Release(s nowplaying.Source) {
	if p.owner.Release(s) {
		p.Changed()
	}
}

func (p *Player[P]) ResetSession() { p.owner.ResetSession() }

func (p *Player[P]) Holder() nowplaying.Source { return p.source() }

func (p *Player[P]) Sourced() (string, bool) {
	s, held := p.owner.Get()
	if held {
		return fmt.Sprintf("%T", s), true
	}
	return "the local queue", false
}

func (p *Player[P]) Transport() nowplaying.Controller {
	if s := p.source(); s != nil {
		return s
	}
	return p.queue
}

func (p *Player[P]) Now() nowplaying.Now {
	if s := p.source(); s != nil {
		return s.Now()
	}
	return p.queued()
}

func (p *Player[P]) From() nowplaying.Kind {
	if s := p.source(); s != nil {
		return s.Kind()
	}
	return nowplaying.FromQueue
}

func (p *Player[P]) Named() string {
	if s := p.source(); s != nil {
		return s.Label()
	}
	return ""
}

func (p *Player[P]) Next() {
	if s := p.external(); s != nil {
		s.Next()
	}
}

func (p *Player[P]) Previous() {
	if s := p.external(); s != nil {
		s.Previous()
	}
}

func (p *Player[P]) Playing() (playing, paused bool) {
	playing, paused = p.stream.Playing()
	if s := p.external(); s != nil {
		now := s.Now()
		playing = playing || now.Playing && !now.Paused
		paused = paused || now.Paused
	}
	return playing, paused
}

func (p *Player[P]) Pause() {
	p.stream.Pause()
	if s := p.external(); s != nil {
		s.Pause()
	}
}

func (p *Player[P]) Unpause() {
	p.stream.Unpause()
	if s := p.external(); s != nil {
		s.Play()
	}
}

func (p *Player[P]) Stop() {
	p.stream.Stop()
	if s := p.external(); s != nil {
		s.Stop()
	}
	p.Changed()
}

func (p *Player[P]) source() nowplaying.Source { s, _ := p.owner.Get(); return s }

func show(t *esphome.TextSensor, v string) {
	if t.Get() != v {
		t.Set(v)
	}
}

func (p *Player[P]) external() nowplaying.Source {
	if s := p.source(); s != nowplaying.Source(p.queue) {
		return s
	}
	return nil
}

func (p *Player[P]) queued() nowplaying.Now {
	playing, paused := p.stream.Playing()
	return nowplaying.Now{
		Playing: playing,
		Paused:  paused,
		Can:     nowplaying.CanPause | nowplaying.CanStop,
	}
}

type queue[P Speaker] struct{ p *Player[P] }

func (q *queue[P]) Play()                 { q.p.stream.Unpause() }
func (q *queue[P]) Pause()                { q.p.stream.Pause() }
func (q *queue[P]) Stop()                 { q.p.stream.Stop(); q.p.Changed() }
func (q *queue[P]) Next()                 {}
func (q *queue[P]) Previous()             {}
func (q *queue[P]) Now() nowplaying.Now   { return q.p.queued() }
func (q *queue[P]) Kind() nowplaying.Kind { return nowplaying.FromQueue }
func (q *queue[P]) Label() string         { return "" }
