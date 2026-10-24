package playerview

import (
	"fmt"
	"strings"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/ui"
	videoplayer "github.com/ygelfand/libcountertop/pkg/display/videopage"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/media/nowplaying"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	videoSkip  = 10 * time.Second
	scrimAlpha = 170
)

var (
	videoInk   = gogui.Color{R: 0xf2, G: 0xf2, B: 0xf2, A: 0xff}
	adFill     = gogui.Color{R: 0xff, G: 0xcc, A: 0xff}
	adInk      = gogui.Color{R: 0x10, G: 0x10, B: 0x10, A: 0xff}
	videoQuiet = gogui.Color{R: 0xb0, G: 0xb0, B: 0xb0, A: 0xff}
	videoScrim = gogui.Color{A: scrimAlpha}
)

func (pv *Renderer) Video(w *gogui.Window, p *videoplayer.Page) gogui.View {
	vw, vh := w.WindowSize()
	look := p.Look()
	full := gogui.ContainerCfg{Sizing: gogui.FillFill, Padding: gogui.NoPadding}

	var layers []gogui.View
	if look.Picture {
		if vh > vw && look.FrameW > 0 && look.FrameH > 0 {
			if now := look.Controls.Now(); len(now.Mark) > 0 {
				mark := ui.Picture(now.Mark, now.MarkID)
				above := (vh - vw*look.FrameH/look.FrameW) / 2
				layers = append(
					layers,
					floatAt(
						0,
						0,
						float32(vw),
						float32(above),
						centredImage(Still(mark), float32(vw)/2, float32(above)/2),
					),
				)
			}
		}
	} else {
		black := full
		black.Color = gogui.Color{A: 0xff}
		layers = append(layers, floatAt(0, 0, float32(vw), float32(vh), gogui.Column(black)))
		layers = append(layers, floatAt(0, 0, float32(vw), float32(vh), backdrop(look, vw, vh)))
	}
	if look.Spinning {
		layers = append(
			layers,
			floatAt(
				0,
				0,
				float32(vw),
				float32(vh),
				widgets.Spinner(vw, vh, time.Since(look.Began)),
			),
		)
	}
	if look.Shown {
		bottom := pv.railRoom()
		layers = append(layers, floatAt(0, 0, float32(vw), 0, pv.videoHeader(p)))
		layers = append(layers, gogui.Column(gogui.ContainerCfg{
			Float:       true,
			FloatAnchor: gogui.FloatBottomLeft,
			FloatTieOff: gogui.FloatBottomLeft,
			Width:       float32(vw),
			Sizing:      gogui.FixedFit,
			Padding:     gogui.Padding{Bottom: bottom},
			Content:     []gogui.View{pv.videoBand(w, p, look)},
		}))
	}
	if look.Controls != nil {
		if b := look.Controls.Now().Break; b != nil {
			layers = append(layers, pv.ad(b))
		}
	}

	cfg := full
	cfg.ID = "video"
	cfg.Content = layers
	cfg.OnClick = func(e gogui.EventCtx) {
		if look.Shown {
			p.Conceal()
		} else {
			p.Reveal()
		}
		e.Consume()
		e.Window.InvalidateLayout()
	}
	return gogui.Column(cfg)
}

func (pv *Renderer) railRoom() float32 {
	shown, pinned := pv.options.Shell.Dock()
	if !shown || pinned || pv.options.Dock == nil || pv.options.Dock() != schema.EdgeBottom {
		return 0
	}
	return widgets.RailDepth()
}

func liveAt(now nowplaying.Now) time.Duration {
	if now.Edge > 0 {
		return now.Edge
	}
	return now.Length
}

func floatAt(x, y, w, h float32, v gogui.View) gogui.View {
	cfg := gogui.ContainerCfg{
		Float:        true,
		FloatOffsetX: x,
		FloatOffsetY: y,
		Width:        w,
		Height:       h,
		Sizing:       gogui.FixedFixed,
		Padding:      gogui.NoPadding,
		Content:      []gogui.View{v},
	}
	if h == 0 {
		cfg.Sizing = gogui.FixedFit
	}
	return gogui.Column(cfg)
}

func centredImage(src string, w, h float32) gogui.View {
	return gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFill,
		Padding: gogui.NoPadding,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Content: []gogui.View{gogui.Image(gogui.ImageCfg{Src: src, Width: w, Height: h})},
	})
}

func backdrop(look videoplayer.Look, vw, vh int) gogui.View {
	if look.Backdrop != nil && look.Note == "" {
		bw, bh := look.Backdrop.Size()
		scale := min(float32(vw)/float32(max(bw, 1)), float32(vh)/float32(max(bh, 1)))
		return centredImage(Still(look.Backdrop), float32(bw)*scale, float32(bh)*scale)
	}
	side := float32(min(vw, vh)) * 2 / 5
	var content []gogui.View
	if look.Logo != nil {
		lw, lh := look.Logo.Size()
		iw, ih := side, side*float32(lh)/float32(max(lw, 1))
		if ih > side {
			iw, ih = side*float32(lw)/float32(max(lh, 1)), side
		}
		content = append(
			content,
			gogui.Image(gogui.ImageCfg{Src: Still(look.Logo), Width: iw, Height: ih}),
		)
	}
	if look.Note != "" {
		st := gogui.CurrentTheme().TextStyleDef
		st.Color = gogui.Color{R: 0xdd, G: 0xdd, B: 0xdd, A: 0xff}
		content = append(content, gogui.Text(gogui.TextCfg{
			Text:      look.Note,
			TextStyle: st,
			Mode:      gogui.TextModeWrap,
		}))
	}
	return gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFill,
		Padding: gogui.PaddingLarge,
		Spacing: gogui.SpacingLarge,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Content: content,
	})
}

func (pv *Renderer) videoHeader(p *videoplayer.Page) gogui.View {
	back := iconStyle(videoInk)
	back.Size = reach() * 0.9
	return gogui.Row(gogui.ContainerCfg{
		Sizing:  gogui.FillFit,
		Padding: gogui.PaddingLarge,
		Color:   videoScrim,
		Content: []gogui.View{
			pv.options.UI.Presses.Button(
				"video-back",
				gogui.IconArrowLeft,
				back,
				gogui.Color{},
				func(e gogui.EventCtx) {
					pv.options.Shell.Remove(p)
				},
			),
		},
	})
}

func (pv *Renderer) videoBand(
	w *gogui.Window,
	p *videoplayer.Page,
	look videoplayer.Look,
) gogui.View {
	c := look.Controls
	now := c.Now()
	vw, vh := w.WindowSize()

	title := now.Title
	if title == "" {
		title = c.Label()
	}
	head := gogui.CurrentTheme().TextStyleDef
	head.Color, head.Size = videoInk, head.Size*1.6
	quiet := gogui.CurrentTheme().TextStyleDef
	quiet.Color = videoQuiet

	words := []gogui.View{gogui.Label(title, head)}
	if sub := strings.Join(nonEmpty(now.Artist, now.Album), " · "); sub != "" {
		words = append(words, gogui.Label(sub, quiet))
	}
	heading := []gogui.View{
		gogui.Column(
			gogui.ContainerCfg{
				Sizing:  gogui.FillFit,
				Padding: gogui.NoPadding,
				Clip:    true,
				Content: words,
			},
		),
	}
	if mark := ui.Picture(now.Mark, now.MarkID); mark != nil && vw > vh {
		mw, mh := mark.Size()
		side := reach()
		heading = append(
			[]gogui.View{
				gogui.Image(
					gogui.ImageCfg{
						Src:    Still(mark),
						Width:  side * float32(mw) / float32(max(mh, 1)),
						Height: side,
					},
				),
			},
			heading...)
	}

	rows := []gogui.View{
		gogui.Row(
			gogui.ContainerCfg{
				Sizing:  gogui.FillFit,
				Padding: gogui.NoPadding,
				Spacing: gogui.SpacingMedium,
				VAlign:  gogui.VAlignMiddle,
				Content: heading,
			},
		),
	}
	if now.Length > 0 {
		var sk nowplaying.Seeker = c
		var marks []videoplayer.Mark
		if mk, ok := c.(videoplayer.Marked); ok {
			marks = mk.Marks()
		}
		rows = append(rows, pv.progress(w, now, sk, quiet, marks))
	}
	if now.LiveWithin > 0 {
		rows = append(rows, pv.live(now, c, quiet))
	}
	side := reach() * 5
	rows = append(rows, gogui.Row(gogui.ContainerCfg{
		Sizing:  gogui.FillFit,
		Padding: gogui.NoPadding,
		VAlign:  gogui.VAlignMiddle,
		Content: []gogui.View{
			pv.videoVolume(side),
			pv.videoTransport(p, now, c),
			gogui.Row(
				gogui.ContainerCfg{Sizing: gogui.FixedFit, Width: side, Padding: gogui.NoPadding},
			),
		},
	}))

	return gogui.Column(gogui.ContainerCfg{
		ID:      "video-band",
		Sizing:  gogui.FillFit,
		Padding: gogui.PaddingLarge,
		Spacing: gogui.SpacingMedium,
		Color:   videoScrim,
		Content: rows,
		OnClick: func(e gogui.EventCtx) {
			p.Linger()
			e.Consume()
		},
	})
}

func (pv *Renderer) live(
	now nowplaying.Now,
	c videoplayer.Controls,
	st gogui.TextStyle,
) gogui.View {
	fill := widgets.Color(pv.options.UI.Palette().Danger)
	var content []gogui.View
	edge := liveAt(now)
	if behind := edge - now.Elapsed; behind > now.LiveWithin {
		fill = gogui.Color{R: 0x60, G: 0x60, B: 0x60, A: 0xff}
		content = append(content, gogui.Label("-"+ClockText(behind), st))
	}
	content = append(
		content,
		gogui.Row(gogui.ContainerCfg{Sizing: gogui.FillFit, Padding: gogui.NoPadding}),
	)
	pill := gogui.CurrentTheme().TextStyleDef
	pill.Color = videoInk
	content = append(content, pv.options.UI.Presses.Pressable(gogui.Row, gogui.ContainerCfg{
		ID:      "video-live",
		Color:   fill,
		Radius:  gogui.RadiusLarge,
		Padding: gogui.PaddingSmall,
		Content: []gogui.View{gogui.Label(say.T("player.live"), pill)},
	}, func(gogui.EventCtx) {
		if c.CanSeek() && edge > 0 {
			c.Seek(edge)
		}
	}))
	return gogui.Row(
		gogui.ContainerCfg{
			Sizing:  gogui.FillFit,
			Padding: gogui.NoPadding,
			VAlign:  gogui.VAlignMiddle,
			Content: content,
		},
	)
}

func (pv *Renderer) ad(b *nowplaying.Break) gogui.View {
	label := say.T("player.ad")
	if b.Clips > 0 {
		label = say.F("player.ad_clip", map[string]any{"Clip": b.Clip, "Clips": b.Clips})
	}
	if b.Length > 0 {
		label += " · " + ClockText(max(b.Length-b.At, 0))
	}
	pill := gogui.CurrentTheme().TextStyleDef
	pill.Color = adInk
	return gogui.Row(gogui.ContainerCfg{
		Float:        true,
		FloatAnchor:  gogui.FloatTopRight,
		FloatTieOff:  gogui.FloatTopRight,
		FloatOffsetX: -reach() * 0.5,
		FloatOffsetY: reach() * 0.5,
		Color:        adFill,
		Radius:       gogui.RadiusLarge,
		Padding:      gogui.PaddingSmall,
		Content:      []gogui.View{gogui.Label(label, pill)},
	})
}

func (pv *Renderer) videoTransport(
	p *videoplayer.Page,
	now nowplaying.Now,
	c videoplayer.Controls,
) gogui.View {
	type control struct {
		glyph    string
		do       func()
		primary  bool
		disabled bool
	}
	atLive := now.LiveWithin > 0 && liveAt(now) > 0 && liveAt(now)-now.Elapsed <= now.LiveWithin
	middle := control{glyph: gogui.IconPlay, do: c.Play, primary: true}
	if now.Playing && !now.Paused {
		middle = control{glyph: gogui.IconPause, do: c.Pause, primary: true}
	}
	var shown []control
	if now.Can.Has(nowplaying.CanPrevious) {
		shown = append(shown, control{glyph: gogui.IconStepBackward, do: c.Previous})
	}
	if c.CanSeek() {
		shown = append(
			shown,
			control{glyph: gogui.IconBackward, do: func() { c.Seek(c.Now().Elapsed - videoSkip) }},
		)
	}
	if now.Can.Has(nowplaying.CanPause) {
		shown = append(shown, middle)
	}
	if c.CanSeek() {
		shown = append(
			shown,
			control{
				glyph:    gogui.IconForward,
				do:       func() { c.Seek(c.Now().Elapsed + videoSkip) },
				disabled: atLive,
			},
		)
	}
	if now.Can.Has(nowplaying.CanNext) {
		shown = append(shown, control{glyph: gogui.IconStepForward, do: c.Next})
	}
	if now.Can.Has(nowplaying.CanStop) {
		shown = append(shown, control{glyph: gogui.IconStop, do: func() {
			c.Stop()
			pv.options.Shell.Remove(p)
		}})
	}

	t := gogui.CurrentTheme()
	var buttons []gogui.View
	for i, b := range shown {
		st, fill := t.TextStyleIconLarge, gogui.Color{R: 0x2a, G: 0x2a, B: 0x2a, A: 0xff}
		st.Color, st.Size = videoInk, reach()*0.9
		if b.primary {
			fill = t.Cfg.ColorAccent
			st.Color, st.Size = t.Cfg.ColorBackground, reach()*1.2
		}
		var do func(gogui.EventCtx)
		if b.disabled {
			fill = gogui.Color{R: 0x1a, G: 0x1a, B: 0x1a, A: 0xff}
			st.Color = gogui.Color{R: 0x55, G: 0x55, B: 0x55, A: 0xff}
		} else {
			act := b.do
			do = func(gogui.EventCtx) {
				p.Linger()
				act()
			}
		}
		buttons = append(
			buttons,
			pv.options.UI.Presses.Button(fmt.Sprintf("video-%d", i), b.glyph, st, fill, do),
		)
	}
	return gogui.Row(
		gogui.ContainerCfg{
			Sizing:  gogui.FillFit,
			HAlign:  gogui.HAlignCenter,
			VAlign:  gogui.VAlignMiddle,
			Spacing: gogui.SpacingPx(reach()),
			Content: buttons,
		},
	)
}

func (pv *Renderer) videoVolume(width float32) gogui.View {
	speaker := gogui.CurrentTheme().TextStyleIconLarge
	speaker.Color, speaker.Size = videoInk, reach()*0.7
	return gogui.Row(gogui.ContainerCfg{
		Sizing:  gogui.FixedFit,
		Width:   width,
		Padding: gogui.NoPadding,
		VAlign:  gogui.VAlignMiddle,
		Spacing: gogui.SpacingMedium,
		Content: []gogui.View{
			gogui.Label(gogui.IconSpeaker, speaker),
			pv.options.UI.Grip(pv.kit.Slider(gogui.SliderCfg{
				ID:     "media-volume",
				Height: reach(),
				Sizing: gogui.FillFit,
				Value:  float32(pv.options.Volume.Level()),
				Max:    100,
				OnChange: func(v float32, e gogui.EventCtx) {
					pv.options.Volume.Set(int(v + 0.5))
					e.Window.InvalidateLayout()
				},
			})),
		},
	})
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
