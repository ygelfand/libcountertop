package playerview

import (
	"fmt"
	"strings"
	"sync"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	videoplayer "github.com/ygelfand/libcountertop/pkg/display/videopage"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/media/nowplaying"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	seekSettle = 250 * time.Millisecond
	seekLand   = 3 * time.Second
)

type scrub struct {
	mu    sync.Mutex
	sk    nowplaying.Seeker
	to    time.Duration
	held  bool
	until time.Time
	timer *time.Timer
}

func (s *scrub) move(sk nowplaying.Seeker, to time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sk, s.to, s.held = sk, to, true
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(seekSettle, s.land)
}

func (s *scrub) land() {
	s.mu.Lock()
	sk, to := s.sk, s.to
	s.held, s.until = false, time.Now().Add(seekLand)
	s.mu.Unlock()
	if sk != nil && sk.CanSeek() {
		sk.Seek(to)
	}
}

func (s *scrub) shown(elapsed time.Duration) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held || time.Now().Before(s.until) {
		return s.to
	}
	return elapsed
}

func Still(img *ui.Image) string {
	w, h := img.Size()
	key := fmt.Sprintf("art/%p/%dx%d", img, w, h)
	if gogui.HasImage(key) {
		return "mem:" + key
	}
	return gogui.UseImage(key, w, h, widgets.NRGBA(img))
}

func Squared(img *ui.Image) string {
	w, h := img.Size()
	side := min(w, h)
	x0, y0 := (w-side)/2, (h-side)/2
	key := fmt.Sprintf("cover/%p/%dx%d", img, w, h)
	if gogui.HasImage(key) {
		return "mem:" + key
	}
	pix := make([]byte, 0, side*side*4)
	for y := range side {
		for x := range side {
			c := img.At(x0+x, y0+y)
			pix = append(pix, c.R, c.G, c.B, 255)
		}
	}
	return gogui.UseImage(key, side, side, pix)
}

func ClockText(d time.Duration) string {
	d = d.Round(time.Second)
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

func (p *Renderer) Body(w *gogui.Window) gogui.View {
	t := gogui.CurrentTheme()
	now := p.options.Player.Now()
	vw, vh := w.WindowSize()
	tall := vh > vw
	beside := !tall && len(now.Queue) > 0
	side := float32(vh) * 0.42
	switch {
	case tall:
		side = float32(vw) * 0.6
	case beside:
		side = float32(vh) * 0.28
	}

	var cover gogui.View
	if now.Art != nil {
		cover = gogui.Image(
			gogui.ImageCfg{Src: Squared(ui.Picture(now.Art, now.ArtID)), Width: side, Height: side},
		)
	} else {
		st := t.TextStyleIconXLarge
		st.Color = t.Cfg.ColorTextSecondary
		cover = gogui.Column(p.kit.Panel(gogui.ContainerCfg{
			Width: side, Height: side, Sizing: gogui.FixedFixed,
			HAlign: gogui.HAlignCenter, VAlign: gogui.VAlignMiddle,
			Content: []gogui.View{gogui.Label(gogui.IconMusic, st)},
		}))
	}

	words := []gogui.View{
		gogui.Text(
			gogui.TextCfg{
				Text:      nowplaying.Heading(now),
				TextStyle: t.TextStyleTitle,
				Mode:      gogui.TextModeWrap,
			},
		),
	}
	if now.Artist != "" {
		words = append(words, gogui.Label(now.Artist, gogui.TextStyle{}))
	}
	if now.Album != "" {
		words = append(words, gogui.Label(now.Album, widgets.Secondary()))
	}
	if from := p.options.Player.Named(); from != "" {
		words = append(words, gogui.Label(from, widgets.Secondary()))
	}
	var controls []gogui.View
	if now.Length > 0 {
		sk, _ := p.options.Player.Transport().(nowplaying.Seeker)
		controls = append(controls, p.progress(w, now, sk, widgets.Secondary(), nil))
	}
	controls = append(controls, p.transport(now), p.loudness())

	if beside {
		info := gogui.Column(
			gogui.ContainerCfg{
				Sizing:  gogui.FillFit,
				Padding: gogui.NoPadding,
				Spacing: gogui.SpacingSmall,
				Content: words,
			},
		)
		head := gogui.Row(
			gogui.ContainerCfg{
				Sizing:  gogui.FillFit,
				Padding: gogui.NoPadding,
				VAlign:  gogui.VAlignMiddle,
				Spacing: gogui.SpacingLarge,
				Content: []gogui.View{cover, info},
			},
		)
		left := gogui.Column(
			gogui.ContainerCfg{
				Sizing:  gogui.FillFill,
				Padding: gogui.NoPadding,
				Spacing: gogui.SpacingMedium,
				Content: append([]gogui.View{head}, controls...),
			},
		)
		right := gogui.Column(
			gogui.ContainerCfg{
				Width:   float32(vw) * 0.4,
				Sizing:  gogui.FixedFill,
				Padding: gogui.NoPadding,
				Content: []gogui.View{p.queue(now)},
			},
		)
		return gogui.Row(
			gogui.ContainerCfg{
				Sizing:  gogui.FillFill,
				Padding: gogui.NoPadding,
				Spacing: gogui.SpacingLarge,
				Content: []gogui.View{left, right},
			},
		)
	}

	words = append(words, controls...)
	text := gogui.Column(
		gogui.ContainerCfg{Sizing: gogui.FillFit, Spacing: gogui.SpacingMedium, Content: words},
	)
	main := gogui.Row(
		gogui.ContainerCfg{
			Sizing:  gogui.FillFit,
			Spacing: gogui.SpacingLarge,
			Content: []gogui.View{cover, text},
		},
	)
	if tall {
		main = gogui.Column(
			gogui.ContainerCfg{
				Sizing:  gogui.FillFit,
				HAlign:  gogui.HAlignCenter,
				Spacing: gogui.SpacingLarge,
				Content: []gogui.View{cover, text},
			},
		)
	}
	if len(now.Queue) == 0 {
		return gogui.Column(
			gogui.ContainerCfg{
				Sizing:     gogui.FillFill,
				Scrollable: true,
				Padding:    gogui.NoPadding,
				Content:    []gogui.View{main},
			},
		)
	}
	return gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFill,
		Spacing: gogui.SpacingLarge,
		Padding: gogui.NoPadding,
		Content: []gogui.View{main, p.queue(now)},
	})
}

func clockWidth(w *gogui.Window, length time.Duration, st gogui.TextStyle) float32 {
	shape := ClockText(length)
	var widest float32
	for d := '0'; d <= '9'; d++ {
		text := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return d
			}
			return r
		}, shape)
		widest = max(widest, w.TextWidth(text, st))
	}
	return widest
}

func clockLabel(
	text string,
	width float32,
	st gogui.TextStyle,
	align gogui.HorizontalAlign,
) gogui.View {
	return gogui.Row(
		gogui.ContainerCfg{
			Width:   width,
			Sizing:  gogui.FixedFit,
			Padding: gogui.NoPadding,
			HAlign:  align,
			Content: []gogui.View{gogui.Label(text, st)},
		},
	)
}

func (p *Renderer) progress(
	w *gogui.Window,
	now nowplaying.Now,
	sk nowplaying.Seeker,
	st gogui.TextStyle,
	marks []videoplayer.Mark,
) gogui.View {
	at := p.seeking.shown(now.Elapsed)
	canSeek := sk != nil && sk.CanSeek()
	lo, hi := time.Duration(0), now.Length
	painted := spans(marks, now.Length)
	if now.Edge > 0 {
		lo, hi = now.Earliest, now.Edge
		painted = append(
			painted,
			outOfReach(now.Length, 0, lo),
			outOfReach(now.Length, hi, now.Length),
		)
	}
	cfg := gogui.SliderCfg{
		ID:       "seek",
		Height:   reach(),
		Sizing:   gogui.FillFit,
		Value:    float32(at.Seconds()),
		Max:      float32(now.Length.Seconds()),
		Disabled: !canSeek,
		OnChange: func(v float32, e gogui.EventCtx) {
			p.seeking.move(sk, min(max(time.Duration(v*float32(time.Second)), lo), hi))
			e.Window.InvalidateLayout()
		},
	}
	bar := p.options.UI.Grip(p.kit.SeekBar(cfg, painted))
	content := []gogui.View{bar}
	if now.LiveWithin == 0 || now.Edge > 0 {
		wide := clockWidth(w, now.Length, st)
		content = []gogui.View{
			clockLabel(ClockText(at), wide, st, gogui.HAlignRight),
			bar,
			clockLabel(ClockText(now.Length), wide, st, gogui.HAlignLeft),
		}
	}
	return gogui.Row(gogui.ContainerCfg{
		Sizing:  gogui.FillFit,
		VAlign:  gogui.VAlignMiddle,
		Spacing: gogui.SpacingMedium,
		Content: content,
	})
}

type control struct {
	glyph   string
	do      func(nowplaying.Controller)
	need    nowplaying.Controls
	primary bool
}

func (p *Renderer) transport(now nowplaying.Now) gogui.View {
	middle := control{glyph: gogui.IconPlay, do: nowplaying.Controller.Play, primary: true}
	if now.Playing && !now.Paused {
		middle = control{
			glyph:   gogui.IconPause,
			do:      nowplaying.Controller.Pause,
			need:    nowplaying.CanPause,
			primary: true,
		}
	}
	t := gogui.CurrentTheme()
	var shown []gogui.View
	for i, b := range []control{
		{glyph: gogui.IconFastBackward, do: nowplaying.Controller.Previous, need: nowplaying.CanPrevious},
		middle,
		{glyph: gogui.IconStop, do: nowplaying.Controller.Stop, need: nowplaying.CanStop},
		{glyph: gogui.IconFastForward, do: nowplaying.Controller.Next, need: nowplaying.CanNext},
	} {
		if b.need != 0 && !now.Can.Has(b.need) {
			continue
		}
		do := b.do
		st := t.TextStyleIconLarge
		st.Size = reach() * 0.45
		if b.primary {
			st.Size = reach() * 0.75
		}
		shown = append(
			shown,
			p.kit.KeyButton(
				fmt.Sprintf("transport-%d", i),
				b.glyph,
				st,
				widgets.Chosen(b.primary),
				func(gogui.EventCtx) { do(p.options.Player.Transport()) },
			),
		)
	}
	return gogui.Row(
		gogui.ContainerCfg{
			Sizing:  gogui.FillFit,
			VAlign:  gogui.VAlignMiddle,
			Spacing: gogui.SpacingMedium,
			Content: shown,
		},
	)
}

func (p *Renderer) loudness() gogui.View {
	return gogui.Row(gogui.ContainerCfg{
		Sizing:  gogui.FillFit,
		VAlign:  gogui.VAlignMiddle,
		Spacing: gogui.SpacingMedium,
		Content: []gogui.View{
			gogui.Label(gogui.IconSpeaker, iconStyle(gogui.CurrentTheme().Cfg.ColorTextSecondary)),
			p.options.UI.Grip(p.kit.Slider(gogui.SliderCfg{
				ID:     "media-volume",
				Height: reach(),
				Sizing: gogui.FillFit,
				Value:  float32(p.options.Volume.Level()),
				Max:    100,
				OnChange: func(v float32, e gogui.EventCtx) {
					p.options.Volume.Set(int(v + 0.5))
					e.Window.InvalidateLayout()
				},
			})),
		},
	})
}

func (p *Renderer) queue(now nowplaying.Now) gogui.View {
	rows := []gogui.View{gogui.Label(say.T("player.next"), widgets.Secondary())}
	for i, tr := range now.Queue {
		play := tr.Play
		line := []gogui.View{gogui.Text(gogui.TextCfg{Text: tr.Title, Mode: gogui.TextModeWrap})}
		if tr.Artist != "" {
			line = append(line, gogui.Label(tr.Artist, widgets.Secondary()))
		}
		var onClick func(gogui.EventCtx)
		if play != nil {
			onClick = func(gogui.EventCtx) { play() }
		}
		var content []gogui.View
		if tr.Art != nil {
			side := reach()
			content = append(
				content,
				gogui.Image(
					gogui.ImageCfg{
						Src:    Squared(ui.Picture(tr.Art, tr.ArtID)),
						Width:  side,
						Height: side,
					},
				),
			)
		}
		content = append(
			content,
			gogui.Column(
				gogui.ContainerCfg{
					Sizing:  gogui.FillFit,
					Padding: gogui.NoPadding,
					Clip:    true,
					Content: line,
				},
			),
		)
		if tr.Length > 0 {
			content = append(content, gogui.Label(ClockText(tr.Length), widgets.Secondary()))
		}
		row := gogui.ContainerCfg{
			ID:      fmt.Sprintf("queue-%d", i),
			Sizing:  gogui.FillFit,
			VAlign:  gogui.VAlignMiddle,
			Padding: gogui.PaddingSmall,
			Spacing: gogui.SpacingMedium,
			Radius:  gogui.RadiusMedium,
			Content: content,
		}
		p.kit.ListRow(&row, style.Rest)
		rows = append(rows, p.options.UI.Presses.Pressable(gogui.Row, row, onClick))
	}
	return gogui.Column(
		gogui.ContainerCfg{
			ID:         "queue",
			Sizing:     gogui.FillFill,
			Scrollable: true,
			OnGesture:  p.options.UI.HoldStill,
			Spacing:    gogui.SpacingSmall,
			Content:    rows,
		},
	)
}

const PriorityMini = 5

func (p *Renderer) Mini(w *gogui.Window) gogui.View {
	now := p.options.Player.Now()
	if p.options.Shell.Open() || !now.Playing && !now.Paused {
		return nil
	}
	t := gogui.CurrentTheme()
	vw, vh := w.WindowSize()
	side := reach() * 1.2

	var art gogui.View
	if now.Art != nil {
		art = gogui.Image(
			gogui.ImageCfg{Src: Squared(ui.Picture(now.Art, now.ArtID)), Width: side, Height: side},
		)
	} else {
		st := t.TextStyleIconLarge
		st.Color = t.Cfg.ColorTextSecondary
		art = gogui.Column(
			gogui.ContainerCfg{
				Width:   side,
				Height:  side,
				Sizing:  gogui.FixedFixed,
				HAlign:  gogui.HAlignCenter,
				VAlign:  gogui.VAlignMiddle,
				Padding: gogui.NoPadding,
				Content: []gogui.View{gogui.Label(gogui.IconMusic, st)},
			},
		)
	}

	words := []gogui.View{gogui.Label(nowplaying.Heading(now), gogui.TextStyle{})}
	if now.Artist != "" {
		words = append(words, gogui.Label(now.Artist, widgets.Secondary()))
	}

	mark := t.TextStyleIconLarge
	mark.Color = t.Cfg.TextStyleDef.Color
	mark.Size = reach() * 0.6
	type control struct {
		glyph string
		do    func(gogui.EventCtx)
	}
	middle := control{gogui.IconPlay, func(gogui.EventCtx) { p.options.Player.Transport().Play() }}
	if now.Playing && !now.Paused {
		middle = control{
			gogui.IconPause,
			func(gogui.EventCtx) { p.options.Player.Transport().Pause() },
		}
	}
	controls := []control{middle}
	if now.Can.Has(nowplaying.CanNext) {
		controls = append(
			controls,
			control{
				gogui.IconFastForward,
				func(gogui.EventCtx) { p.options.Player.Transport().Next() },
			},
		)
	}
	controls = append(
		controls,
		control{gogui.IconExpand, func(gogui.EventCtx) { p.options.Player.Open() }},
	)

	content := []gogui.View{
		art,
		gogui.Column(
			gogui.ContainerCfg{
				Sizing:  gogui.FillFit,
				Padding: gogui.NoPadding,
				Clip:    true,
				Content: words,
			},
		),
	}
	for i, c := range controls {
		content = append(content, p.kit.IconKey(gogui.ContainerCfg{
			ID:      fmt.Sprintf("mini-%d", i),
			Width:   side,
			Height:  side,
			Sizing:  gogui.FixedFixed,
			HAlign:  gogui.HAlignCenter,
			VAlign:  gogui.VAlignMiddle,
			Padding: gogui.NoPadding,
			Radius:  gogui.RadiusMedium,
		}, c.glyph, mark, style.Rest, c.do))
	}
	bar := gogui.Row(p.kit.Panel(gogui.ContainerCfg{
		ID:      "mini",
		Sizing:  gogui.FillFit,
		VAlign:  gogui.VAlignMiddle,
		Padding: gogui.PaddingSmall,
		Spacing: gogui.SpacingMedium,
		Shadow:  &gogui.BoxShadow{Color: gogui.Black.WithOpacity(0.5), OffsetY: 4, BlurRadius: 18},
		OnClick: func(e gogui.EventCtx) { e.Consume() },
		Content: content,
	}))
	pin := gogui.VAlignBottom
	if p.options.Dock != nil && p.options.Dock() == schema.EdgeBottom {
		pin = gogui.VAlignTop
	}
	return gogui.Column(gogui.ContainerCfg{
		Width:   float32(vw),
		Height:  float32(vh),
		Sizing:  gogui.FixedFixed,
		Padding: gogui.PaddingLarge,
		VAlign:  pin,
		Content: []gogui.View{bar},
	})
}
