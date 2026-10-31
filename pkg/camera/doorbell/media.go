package doorbell

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ygelfand/reolinkproxy/pkg/baichuan"
	"github.com/ygelfand/reolinkproxy/pkg/codec"

	"github.com/ygelfand/libcountertop/pkg/audio/resample"
	"github.com/ygelfand/libcountertop/pkg/audio/upsample"
	"github.com/ygelfand/libcountertop/pkg/media/rtsp"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	backFrame   = 20 * time.Millisecond
	reolinkH264 = "H264"
)

var (
	ErrAssistantVideo = errors.New("doorbell: video comes from Home Assistant")
	errVideoEnded     = errors.New("doorbell: the doorbell stopped sending video")
)

type Watcher struct {
	Picture func(rtsp.Unit)
	Sound   func(pcm []int16)
}

type Talker interface {
	Speak(pcm []int16) error
	Close() error
}

func (d *Doorbell) PlannedVideo(slot int) string {
	door, ok := doorAt(d.o.Settings.Read(), slot)
	if !ok {
		return ""
	}
	return plannedVideo(door)
}

func plannedVideo(door schema.DoorbellDoor) string {
	switch door.Video {
	case schema.VideoReolink:
		return sourceReolink
	case schema.VideoONVIF:
		return sourceOnvif
	case schema.VideoRTSP:
		return sourceRTSP
	}
	return sourceAssistant
}

func (d *Doorbell) Watch(
	ctx context.Context,
	slot int,
	quality schema.DoorbellStream,
	w Watcher,
) error {
	door, ok := doorAt(d.o.Settings.Read(), slot)
	if !ok {
		return errUnsupported
	}
	door.Stream = quality
	source := plannedVideo(door)
	switch source {
	case sourceReolink:
		return d.tryVideo(
			ctx,
			slot,
			source,
			w,
			func(w Watcher) error { return d.reolinkWatch(ctx, slot, door, w) },
		)
	case sourceOnvif, sourceRTSP:
		return d.tryVideo(
			ctx,
			slot,
			source,
			w,
			func(w Watcher) error { return rtspWatch(ctx, door, w) },
		)
	}
	d.seeVideo(slot, sourceAssistant, Unset, "")
	return ErrAssistantVideo
}

func (d *Doorbell) tryVideo(
	ctx context.Context,
	slot int,
	source string,
	w Watcher,
	run func(Watcher) error,
) error {
	d.seeVideo(slot, source, Connecting, "")
	var once sync.Once
	picture := w.Picture
	w.Picture = func(u rtsp.Unit) {
		once.Do(func() { d.seeVideo(slot, source, Ready, "") })
		if picture != nil {
			picture(u)
		}
	}
	err := run(w)
	switch {
	case ctx.Err() != nil:
	case err == nil:
	case errors.Is(err, ErrUnreachable):
		d.seeVideo(slot, source, Unreachable, err.Error())
	default:
		d.seeVideo(slot, source, BadLogin, err.Error())
	}
	return err
}

func (d *Doorbell) Talk(ctx context.Context, slot int) (Talker, error) {
	door, ok := doorAt(d.o.Settings.Read(), slot)
	if !ok {
		return nil, errUnsupported
	}
	switch door.Talk {
	case schema.TalkReolink:
		return d.reolinkTalk(ctx, slot, door)
	case schema.TalkONVIF:
		return onvifTalk(ctx, door)
	}
	t, err := d.reolinkTalk(ctx, slot, door)
	if err == nil || ctx.Err() != nil || door.Host == "" || errors.Is(err, ErrBadLogin) {
		return t, err
	}
	slog.Warn("doorbell talk falls back to ONVIF", "door", door.Name, "why", err)
	return onvifTalk(ctx, door)
}

func doorAt(cfg schema.Doorbell, slot int) (schema.DoorbellDoor, bool) {
	if slot < 0 || slot >= len(cfg.Doors) || !cfg.Doors[slot].Set() {
		return schema.DoorbellDoor{}, false
	}
	return cfg.Doors[slot], true
}

func stream(s schema.DoorbellStream) baichuan.Stream {
	if s == schema.DoorbellSub {
		return baichuan.StreamSub
	}
	return baichuan.StreamMain
}

func (d *Doorbell) reolinkWatch(
	ctx context.Context,
	slot int,
	door schema.DoorbellDoor,
	w Watcher,
) error {
	l := d.link(slot, door)
	c, err := l.ensure(ctx)
	if err != nil {
		return err
	}
	media, err := c.StartPreview(ctx, uint8(door.Channel), stream(door.Stream))
	if err != nil {
		l.reset(c, err)
		return errors.Join(ErrUnreachable, err)
	}
	defer media.Close()
	slog.Info("doorbell video started", "door", door.Name, "stream", stream(door.Stream))
	var clock micros
	ear := newEar(w.Sound)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.Done():
			l.reset(c, c.Err())
			return errors.Join(ErrUnreachable, c.Err())
		case p, ok := <-media.Packets:
			if !ok {
				slog.Warn("doorbell video ended", "door", door.Name, "err", c.Err())
				return errors.Join(ErrUnreachable, errVideoEnded)
			}
			switch p.Kind {
			case baichuan.MediaPacketIFrame, baichuan.MediaPacketPFrame:
				if w.Picture == nil {
					continue
				}
				codec := rtsp.H265
				if p.Codec == reolinkH264 {
					codec = rtsp.H264
				}
				w.Picture(rtsp.Unit{
					Codec: codec,
					Data:  p.Data,
					PTS:   clock.at(p.TimestampMicrosecs),
					Key:   p.Kind == baichuan.MediaPacketIFrame,
				})
			case baichuan.MediaPacketAAC:
				if w.Sound != nil {
					ear.adts(p.Data)
				}
			case baichuan.MediaPacketADPCM:
				if w.Sound != nil {
					ear.adpcmBlock(p.Data)
				}
			}
		}
	}
}

type micros struct {
	started bool
	first   uint32
	last    uint32
	wraps   int64
}

func (m *micros) at(us uint32) time.Duration {
	if !m.started {
		m.started, m.first, m.last = true, us, us
	}
	if us < m.last && m.last-us > 1<<31 {
		m.wraps++
	}
	m.last = us
	return time.Duration(m.wraps<<32+int64(us)-int64(m.first)) * time.Microsecond
}

func rtspWatch(ctx context.Context, door schema.DoorbellDoor, w Watcher) error {
	uri, err := streamURI(ctx, door)
	if err != nil {
		return err
	}
	o := rtsp.Options{User: door.User, Password: door.Password, Video: w.Picture}
	if w.Sound != nil {
		ear := newEar(w.Sound)
		o.Audio = ear.rtsp
	}
	c, err := rtsp.Dial(ctx, uri, o)
	if err != nil {
		if errors.Is(err, rtsp.ErrRefused) {
			return err
		}
		return errors.Join(ErrUnreachable, err)
	}
	defer c.Close()
	if err := c.Run(ctx); err != nil {
		return errors.Join(ErrUnreachable, err)
	}
	return nil
}

type reolinkTalker struct {
	door    string
	s       *baichuan.TalkSession
	ctx     context.Context
	down    *resample.Rational
	enc     codec.ADPCMEncoder
	pending []int16
	block   int
	sent    int
}

func (d *Doorbell) reolinkTalk(
	ctx context.Context,
	slot int,
	door schema.DoorbellDoor,
) (Talker, error) {
	if !door.Signed() {
		return nil, ErrNoPassword
	}
	l := d.link(slot, door)
	c, err := l.ensure(ctx)
	if err != nil {
		return nil, err
	}
	slog.Info("doorbell talk starting", "door", door.Name)
	s, err := c.StartTalk(ctx, uint8(door.Channel))
	if err != nil {
		slog.Warn("doorbell talk refused", "door", door.Name, "err", err)
		select {
		case <-c.Done():
			l.reset(c, err)
		default:
		}
		return nil, err
	}
	slog.Info("doorbell talk started",
		"door", door.Name, "rate", s.SampleRate(), "block", s.SamplesPerBlock())
	return &reolinkTalker{
		door:  door.Name,
		s:     s,
		ctx:   context.WithoutCancel(ctx),
		down:  resample.NewRational(upsample.VoiceRate, s.SampleRate(), 1),
		block: s.SamplesPerBlock(),
	}, nil
}

func (t *reolinkTalker) Speak(pcm []int16) error {
	t.pending = append(t.pending, t.down.Run(pcm)...)
	for len(t.pending) >= t.block {
		b, err := t.enc.EncodeBlock(t.pending[:t.block])
		if err != nil {
			return err
		}
		if err := t.s.WriteADPCMBlock(t.ctx, b); err != nil {
			slog.Warn("doorbell talk send failed", "door", t.door, "sent", t.sent, "err", err)
			return err
		}
		t.sent++
		t.pending = t.pending[t.block:]
	}
	return nil
}

func (t *reolinkTalker) Close() error {
	ctx, cancel := context.WithTimeout(t.ctx, snapWait)
	defer cancel()
	slog.Info("doorbell talk ended", "door", t.door, "sent", t.sent)
	return t.s.Close(ctx)
}

type onvifTalker struct {
	c       *rtsp.Client
	stop    context.CancelFunc
	done    sync.WaitGroup
	down    *resample.Rational
	alaw    bool
	frame   int
	pending []int16
}

func onvifTalk(ctx context.Context, door schema.DoorbellDoor) (Talker, error) {
	uri, err := streamURI(ctx, door)
	if err != nil {
		return nil, err
	}
	c, err := rtsp.Dial(ctx, uri, rtsp.Options{
		User:        door.User,
		Password:    door.Password,
		Backchannel: true,
	})
	if err != nil {
		return nil, err
	}
	rate := c.BackRate()
	if rate <= 0 {
		_ = c.Close()
		return nil, fmt.Errorf("doorbell: backchannel %q has no clock rate", c.BackCodec())
	}
	var alaw bool
	switch c.BackCodec() {
	case "PCMA":
		alaw = true
	case "PCMU":
	default:
		_ = c.Close()
		return nil, fmt.Errorf("doorbell: backchannel sends %q", c.BackCodec())
	}
	run, stop := context.WithCancel(context.WithoutCancel(ctx))
	t := &onvifTalker{
		c:     c,
		stop:  stop,
		down:  resample.NewRational(upsample.VoiceRate, rate, 1),
		alaw:  alaw,
		frame: rate * int(backFrame/time.Millisecond) / 1000,
	}
	t.done.Go(func() { _ = c.Run(run) })
	return t, nil
}

func (t *onvifTalker) Speak(pcm []int16) error {
	t.pending = append(t.pending, t.down.Run(pcm)...)
	for len(t.pending) >= t.frame {
		chunk := t.pending[:t.frame]
		payload := encodeMuLaw(chunk)
		if t.alaw {
			payload = codec.EncodePCMA(chunk)
		}
		if err := t.c.SendBack(payload, len(chunk)); err != nil {
			return err
		}
		t.pending = t.pending[t.frame:]
	}
	return nil
}

func (t *onvifTalker) Close() error {
	t.stop()
	err := t.c.Close()
	t.done.Wait()
	return err
}
