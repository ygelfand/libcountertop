package doorbellview

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/camera/doorbell"
	"github.com/ygelfand/libcountertop/pkg/display/callvideo"
	"github.com/ygelfand/libcountertop/pkg/display/icons"
	"github.com/ygelfand/libcountertop/pkg/display/message"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/surface"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/media/rtc"
	"github.com/ygelfand/libcountertop/pkg/media/rtsp"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	videoLayer = 940
	videoZ     = -2
	retryFirst = 2 * time.Second
	retryMost  = 30 * time.Second
	heardAhead = rtc.PlayRate / 5
	spinStep   = widgets.SpinStep
)

type session struct {
	v     *View
	slot  int
	view  *Live
	began time.Time

	mu      sync.Mutex
	stop    context.CancelFunc
	video   *callvideo.Stream
	shown   bool
	why     string
	audible bool
	talker  doorbell.Talker
	hold    *shell.Hold
	endTalk context.CancelFunc
	ended   bool
	quality schema.DoorbellStream
}

var errDecoder = errors.New("doorbell: this screen can't decode the stream")

type talk int

const (
	talkOff talk = iota
	talkConnecting
	talkOn
)

func talkState(t doorbell.Talker, connecting context.CancelFunc) talk {
	switch {
	case t != nil:
		return talkOn
	case connecting != nil:
		return talkConnecting
	}
	return talkOff
}

func (l *session) talking() talk {
	l.mu.Lock()
	defer l.mu.Unlock()
	return talkState(l.talker, l.endTalk)
}

func (l *session) start() {
	ctx, stop := context.WithCancel(context.Background())
	l.mu.Lock()
	l.stop = stop
	l.audible = l.view.rang
	l.quality = l.v.o.Settings.Read().Doors[l.slot].Stream
	l.mu.Unlock()
	safe.Go("doorbell picture", func() { l.v.doorPicture(l.slot, l.v.screenMost()) })
	safe.Go("doorbell live", func() { l.run(ctx) })
}

func (l *session) end() {
	l.mu.Lock()
	if l.ended {
		l.mu.Unlock()
		return
	}
	l.ended = true
	stop := l.stop
	l.mu.Unlock()
	l.stopTalking()
	if stop != nil {
		stop()
	}
}

func (l *session) run(ctx context.Context) {
	wait := retryFirst
	for ctx.Err() == nil {
		err := l.watch(ctx)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, doorbell.ErrAssistantVideo) {
			if why := l.v.o.Bell.Door(l.slot).VideoWhy; why != "" {
				l.v.notice(l.slot, say.T("doorbell.why.fallback"), why)
			}
			if l.assistant(ctx) {
				return
			}
			l.explain(say.T("doorbell.why.camera"))
			<-ctx.Done()
			return
		}
		slog.Warn("doorbell live ended", "slot", l.slot, "err", err)
		if errors.Is(err, errDecoder) {
			l.mu.Lock()
			main := l.quality == schema.DoorbellMain
			l.quality = schema.DoorbellSub
			l.mu.Unlock()
			if main {
				l.explain(say.T("doorbell.why.toobig"))
				continue
			}
			l.explain(say.T("doorbell.why.decoder"))
			<-ctx.Done()
			return
		}
		if errors.Is(err, doorbell.ErrBadLogin) {
			l.explain(failure(err))
			<-ctx.Done()
			return
		}
		l.explain(say.T("doorbell.why.lost"))
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, retryMost)
	}
}

func (l *session) watch(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		out     *callvideo.Stream
		refused bool
	)
	l.mu.Lock()
	quality := l.quality
	l.mu.Unlock()
	defer func() {
		if out != nil {
			out.Close()
		}
		l.mu.Lock()
		l.video, l.shown = nil, false
		l.mu.Unlock()
		l.v.o.Shell.Redraw()
	}()
	err := l.v.o.Bell.Watch(ctx, l.slot, quality, doorbell.Watcher{
		Picture: func(u rtsp.Unit) {
			if refused {
				return
			}
			if out != nil && out.Refused() {
				refused = true
				cancel()
				return
			}
			if out == nil {
				if !u.Key || l.v.o.Display == nil {
					return
				}
				out = callvideo.NewStream(l.v.o.Display, codecOf(u.Codec), videoLayer, videoZ)
				l.mu.Lock()
				l.video = out
				l.mu.Unlock()
			}
			out.Show(rtc.Frame{Data: u.Data, PTS: u.PTS, Key: u.Key})
			if out.Stats().Shown > 0 {
				l.mu.Lock()
				first := !l.shown
				l.shown, l.why = true, ""
				l.mu.Unlock()
				if first {
					l.v.o.Shell.Redraw()
				}
			}
		},
		Sound: l.play,
	})
	if refused {
		slog.Warn("doorbell video decoder refused the stream", "slot", l.slot, "stream", quality)
		return errDecoder
	}
	return err
}

func failure(err error) string {
	switch {
	case errors.Is(err, doorbell.ErrNoPassword):
		return say.T("doorbell.why.nopassword")
	case errors.Is(err, doorbell.ErrBadLogin):
		if code, ok := doorbell.LoginCode(err); ok {
			return say.F("doorbell.why.refused", map[string]any{"Code": code})
		}
		return say.T("doorbell.status.login")
	case errors.As(err, new(*doorbell.OtherDoorbell)):
		return say.T("doorbell.why.other")
	case errors.Is(err, doorbell.ErrUnreachable):
		return say.T("doorbell.why.unreachable")
	}
	return err.Error()
}

func (v *View) notice(slot int, title, body string) {
	if v.o.Messages == nil {
		return
	}
	v.o.Messages.Show(message.Message{
		Title: v.doorName(slot) + ": " + title,
		Body:  body,
		Tone:  schema.ToneWarning,
	}, cardFor)
}

func codecOf(c rtsp.Codec) uint32 {
	if c == rtsp.H265 {
		return surface.HEVC
	}
	return surface.AVC
}

func (l *session) play(pcm []int16) {
	sp := l.v.o.Speaker
	if sp == nil {
		return
	}
	l.mu.Lock()
	on := l.audible && l.talker == nil
	l.mu.Unlock()
	if on && sp.Queued() <= heardAhead {
		sp.Play(pcm)
	}
}

func (l *session) assistant(ctx context.Context) bool {
	if l.v.o.Cameras == nil {
		return false
	}
	cctx, cancel := context.WithTimeout(ctx, cameraWait)
	entity, ok := l.v.o.Bell.Camera(cctx, l.slot)
	cancel()
	if !ok || ctx.Err() != nil {
		return false
	}
	l.v.o.Shell.Remove(l.view)
	return l.v.o.Cameras.Watch(entity)
}

func (l *session) explain(why string) {
	l.mu.Lock()
	l.why = why
	l.mu.Unlock()
	l.v.o.Shell.Redraw()
}

func (l *session) toggleSound() {
	l.mu.Lock()
	l.audible = !l.audible
	l.mu.Unlock()
	l.v.o.Shell.Redraw()
}

func (l *session) toggleTalk() {
	switch l.talking() {
	case talkConnecting:
		return
	case talkOn:
		l.stopTalking()
		return
	}
	if l.v.o.Mic == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.mu.Lock()
	l.endTalk = cancel
	l.mu.Unlock()
	l.v.o.Shell.Redraw()
	safe.Go("doorbell talk", func() { l.talk(ctx) })
}

func (l *session) talk(ctx context.Context) {
	t, err := l.v.o.Bell.Talk(ctx, l.slot)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		slog.Warn("doorbell talk", "slot", l.slot, "err", err)
		l.explain(say.F("doorbell.why.talk", map[string]any{"Why": failure(err)}))
		l.stopTalking()
		return
	}
	if ctx.Err() != nil {
		_ = t.Close()
		return
	}
	hold := l.v.o.Shell.Hold(l.view)
	l.mu.Lock()
	l.talker, l.hold = t, hold
	l.mu.Unlock()
	l.v.o.Shell.Redraw()
	frames, release := l.v.o.Mic()
	defer release()
	for {
		select {
		case <-ctx.Done():
			return
		case pcm, ok := <-frames:
			if !ok {
				return
			}
			if err := t.Speak(pcm); err != nil {
				slog.Warn("doorbell talk", "slot", l.slot, "err", err)
				l.explain(say.T("doorbell.why.talk"))
				go l.stopTalking()
				return
			}
		}
	}
}

func (l *session) stopTalking() {
	l.mu.Lock()
	t, hold, cancel := l.talker, l.hold, l.endTalk
	l.talker, l.hold, l.endTalk = nil, nil, nil
	l.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if t != nil {
		if err := t.Close(); err != nil {
			slog.Debug("doorbell talk close", "slot", l.slot, "err", err)
		}
	}
	hold.Keep(false)
	l.v.o.Shell.Redraw()
}

func (l *session) close() {
	l.end()
	l.v.o.Shell.Remove(l.view)
}

func (l *session) build(w *gogui.Window) gogui.View {
	vw, vh := w.WindowSize()
	l.mu.Lock()
	shown, why, audible := l.shown, l.why, l.audible
	talking := talkState(l.talker, l.endTalk)
	l.mu.Unlock()
	name := l.v.doorName(l.slot)

	back := gogui.RGB(0, 0, 0)
	var content []gogui.View
	if shown {
		back = gogui.ColorTransparent
	} else {
		if pic, ok := l.v.shots.get(doorKey(l.slot)); ok {
			sw, sh := fitInside(pic.Width, pic.Height, vw, vh)
			content = append(
				content,
				gogui.Image(gogui.ImageCfg{Src: pic.Src, Width: sw, Height: sh}),
			)
		} else {
			content = append(content, gogui.Label(name, widgets.Secondary()))
		}
		if why == "" {
			content = append(content,
				widgets.Placed(ui.Rect{W: vw, H: vh}, widgets.Spinner(vw, vh, time.Since(l.began))))
			l.v.spinAgain(w)
		}
	}
	content = append(content,
		widgets.CloseCorner(l.v.presses, "doorbell-live-close", l.close),
		banner(vw, name, why),
		l.controls(vw, vh, audible, talking),
	)
	return gogui.Column(gogui.ContainerCfg{
		ID:      "doorbell-live",
		Sizing:  gogui.FillFill,
		Padding: gogui.NoPadding,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Color:   back,
		Content: content,
	})
}

func (l *session) controls(vw, vh int, audible bool, talking talk) gogui.View {
	st := gogui.CurrentTheme().TextStyleIconMedium
	st.Family = icons.Family
	st.Size = style.Reach() * 1.2
	st.Color = gogui.RGB(0xff, 0xff, 0xff)
	side := int(st.Size * 2)
	gap := int(gogui.PadLarge) * 2
	var buttons []gogui.View
	sound := "mdi:volume-off"
	if audible {
		sound = "mdi:volume-high"
	}
	if l.v.o.Speaker != nil {
		buttons = append(buttons, l.v.presses.Button(
			"doorbell-live-sound", icons.Glyph(icons.Named(sound)), st,
			gogui.RGBA(0, 0, 0, 0x99), func(gogui.EventCtx) { l.toggleSound() }))
	}
	if l.v.o.Mic != nil {
		glyph, tint := "mdi:microphone", gogui.RGBA(0, 0, 0, 0x99)
		switch talking {
		case talkConnecting:
			glyph, tint = "mdi:dots-horizontal", gogui.RGBA(0, 0, 0, 0x55)
		case talkOn:
			glyph, tint = "mdi:microphone-off", gogui.RGB(0xd3, 0x2f, 0x2f)
		}
		buttons = append(buttons, l.v.presses.Button(
			"doorbell-live-talk", icons.Glyph(icons.Named(glyph)), st,
			tint, func(gogui.EventCtx) { l.toggleTalk() }))
	}
	width := len(buttons)*side + max(len(buttons)-1, 0)*gap
	pad := int(gogui.PadLarge)
	return widgets.Placed(
		ui.Rect{X: (vw - width) / 2, Y: vh - pad - side, W: width, H: side},
		gogui.Row(gogui.ContainerCfg{
			Sizing:  gogui.FillFill,
			Padding: gogui.NoPadding,
			Spacing: gogui.SpacingPx(float32(gap)),
			Content: buttons,
		}),
	)
}

func banner(vw int, name, why string) gogui.View {
	st := gogui.CurrentTheme().TextStyleTitle
	st.Color = gogui.RGB(0xff, 0xff, 0xff)
	lines := []gogui.View{gogui.Label(name, st)}
	if why != "" {
		small := gogui.CurrentTheme().Cfg.TextStyleDef
		small.Color = gogui.RGB(0xff, 0xff, 0xff)
		lines = append(lines, gogui.Label(why, small))
	}
	pad := int(gogui.PadLarge)
	tall := int(st.Size*2.4) + pad*2
	if why != "" {
		tall += int(st.Size * 1.6)
	}
	return widgets.Placed(
		ui.Rect{Y: pad, W: vw, H: tall},
		gogui.Row(gogui.ContainerCfg{
			Sizing:  gogui.FillFill,
			HAlign:  gogui.HAlignCenter,
			VAlign:  gogui.VAlignTop,
			Padding: gogui.NoPadding,
			Content: []gogui.View{gogui.Column(gogui.ContainerCfg{
				Sizing: gogui.FitFit,
				Radius: gogui.RadiusLarge,
				Color:  gogui.RGBA(0, 0, 0, 0x99),
				HAlign: gogui.HAlignCenter,
				Padding: gogui.NewPadding(
					gogui.PadSmall,
					gogui.PadLarge,
					gogui.PadSmall,
					gogui.PadLarge,
				),
				Content: lines,
			})},
		}),
	)
}

func fitInside(w, h, vw, vh int) (float32, float32) {
	if w <= 0 || h <= 0 {
		return float32(vw), float32(vh)
	}
	sw, sh := float32(vw), float32(h)*float32(vw)/float32(w)
	if sh > float32(vh) {
		sw, sh = float32(w)*float32(vh)/float32(h), float32(vh)
	}
	return sw, sh
}

func (v *View) doorName(slot int) string {
	for _, d := range v.o.Bell.Doors() {
		if d.Slot == slot && d.Name != "" {
			return d.Name
		}
	}
	return say.F("doorbell.slot", map[string]any{"N": slot + 1})
}
