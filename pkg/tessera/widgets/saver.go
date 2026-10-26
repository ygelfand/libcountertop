package widgets

import (
	"strings"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/clockview"
	"github.com/ygelfand/libcountertop/pkg/display/icons"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	kit "github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/tessera"
)

const (
	pictureSaver = "saver"
	saverCamera  = 15 * time.Second
	saverCover   = time.Hour

	mediaPause      = 1
	mediaVolumeSet  = 4
	mediaVolumeStep = 1024
	mediaPlay       = 16384

	playGlyph       = 0xF040A
	pauseGlyph      = 0xF03E4
	volumeDownGlyph = 0xF075E
	volumeUpGlyph   = 0xF075D
)

func (v *View) Saver(w *gogui.Window, at ui.Rect) []gogui.View {
	screen := v.pick(at)
	if screen == nil {
		return nil
	}
	layout, shown := screen.Layout()
	if !shown {
		return nil
	}
	s, ok := layout.Shown()
	if !ok {
		return nil
	}
	now := time.Now()
	black := gogui.RGB(0, 0, 0)
	ground := kit.Placed(at, gogui.Column(gogui.ContainerCfg{
		Width:   float32(at.W),
		Height:  float32(at.H),
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		Color:   black,
	}))
	if s.Kind == tessera.SaverClock {
		return []gogui.View{ground, v.saverClock(w, at, s, layout, now)}
	}
	return append([]gogui.View{ground}, v.saverPicture(w, at, screen, s, now)...)
}

func night(pal theme.Theme) theme.Theme {
	pal.Background, pal.Surface = theme.RGB(0x000000), theme.RGB(0x000000)
	pal.Text, pal.Muted = theme.RGB(0xffffff), theme.RGB(0xb0b0b0)
	return pal
}

func (v *View) saverClock(
	w *gogui.Window,
	at ui.Rect,
	s tessera.Saver,
	l tessera.Layout,
	now time.Time,
) gogui.View {
	v.tick(w, now, time.Minute)
	reach := style.Reach()
	pal := night(v.o.UI.Palette())
	line := ""
	if s.Temperature != "" {
		line = s.Temperature
	} else {
		var words []string
		for _, it := range s.Items {
			if t := barText(it, l.Clock24, now); t != "" {
				words = append(words, t)
			}
		}
		line = strings.Join(words, "  ·  ")
	}
	band := 0
	if line != "" {
		band = int(reach * 2)
	}
	face := clockview.Plain(
		w,
		ui.Rect{W: at.W, H: at.H - band},
		clockview.Read(now, l.Clock24),
		pal,
	)
	content := []gogui.View{face}
	if line != "" {
		st := gogui.CurrentTheme().Cfg.TextStyleDef
		st.Size *= 1.6
		st.Color = gogui.RGB(pal.Muted.R, pal.Muted.G, pal.Muted.B)
		content = append(content, gogui.Label(line, st))
	}
	return kit.Placed(at, gogui.Column(gogui.ContainerCfg{
		Width:   float32(at.W),
		Height:  float32(at.H),
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		HAlign:  gogui.HAlignCenter,
		Content: content,
	}))
}

func (v *View) saverPicture(
	w *gogui.Window,
	at ui.Rect,
	screen *tessera.Screen,
	s tessera.Saver,
	now time.Time,
) []gogui.View {
	reach := style.Reach()
	every := saverCamera
	if s.Kind == tessera.SaverMedia {
		every = saverCover
	}
	v.want(pictureSaver, s.Kind+"|"+s.Entity+"|"+s.Mark)
	img := v.keepFresh(w, screen, pictureSaver, s.Entity, every, func() {
		screen.AskPicture(pictureSaver, map[string]string{
			"saver":  s.Kind,
			"entity": s.Entity,
			"size":   "",
			"bg":     "000000",
		})
	}, now)
	var out []gogui.View
	if img != nil {
		pw, ph := fitted(img, at)
		box := ui.Rect{X: at.X + (at.W-pw)/2, Y: at.Y + (at.H-ph)/2, W: pw, H: ph}
		out = append(
			out,
			kit.Placed(box, kit.Picture(v.source(pictureSaver, s.Entity, img), pw, ph)),
		)
	}
	white := gogui.RGB(0xff, 0xff, 0xff)
	big := gogui.CurrentTheme().Cfg.TextStyleDef
	big.Size *= 2
	big.Color = white
	small := gogui.CurrentTheme().Cfg.TextStyleDef
	small.Size *= 1.2
	small.Color = white.WithOpacity(0.75)
	var words []gogui.View
	if s.Kind == tessera.SaverMedia {
		title := s.Title
		if title == "" {
			title = s.Name
		}
		words = append(
			words,
			gogui.Text(gogui.TextCfg{Text: title, TextStyle: big, Mode: gogui.TextModeWrap}),
		)
		var by []string
		for _, part := range []string{s.Artist, s.Album} {
			if part != "" {
				by = append(by, part)
			}
		}
		if len(by) > 0 {
			words = append(words, gogui.Label(strings.Join(by, " · "), small))
		}
	} else {
		words = append(words, gogui.Label(s.Name, big))
	}
	keys := v.saverKeys(w, s)
	edge := reach * 0.75
	keyW := float32(0)
	if len(keys) > 0 {
		keyW = reach * 1.6
	}
	out = append(out, kit.Placed(at, gogui.Row(gogui.ContainerCfg{
		Width:   float32(at.W),
		Height:  float32(at.H),
		Sizing:  gogui.FixedFixed,
		Padding: gogui.PadAll(edge),
		Spacing: gogui.SpacingPx(reach * 0.5),
		VAlign:  gogui.VAlignBottom,
		Content: []gogui.View{
			gogui.Column(gogui.ContainerCfg{
				Width:   float32(at.W) - 2*edge - keyW - reach*0.5,
				Sizing:  gogui.FixedFit,
				Padding: gogui.NoPadding,
				Spacing: gogui.SpacingPx(reach * 0.2),
				Content: words,
			}),
			gogui.Column(gogui.ContainerCfg{
				Sizing:  gogui.FitFit,
				Padding: gogui.NoPadding,
				Spacing: gogui.SpacingPx(reach * 0.3),
				Content: keys,
			}),
		},
	})))
	return out
}

func (v *View) saverKeys(w *gogui.Window, s tessera.Saver) []gogui.View {
	if s.Kind != tessera.SaverMedia {
		return nil
	}
	target := tessera.Tile{Entity: s.Entity}
	press := func(id string, glyph rune, action string) gogui.View {
		st := gogui.CurrentTheme().TextStyleIconMedium
		st.Family = icons.Family
		st.Color = gogui.RGB(0xff, 0xff, 0xff)
		return v.o.UI.Toolkit.IconKey(gogui.ContainerCfg{
			ID:      id,
			Width:   style.Reach() * 1.6,
			Height:  style.Reach() * 1.6,
			Sizing:  gogui.FixedFixed,
			Radius:  gogui.RadiusLarge,
			HAlign:  gogui.HAlignCenter,
			VAlign:  gogui.VAlignMiddle,
			Padding: gogui.NoPadding,
		}, icons.Glyph(glyph), st, style.Partial, func(e gogui.EventCtx) {
			v.send(w, target, action, nil, nil, "")
			e.Consume()
		})
	}
	var keys []gogui.View
	if s.Features&mediaVolumeSet != 0 || s.Features&mediaVolumeStep != 0 {
		keys = append(keys,
			press("tessera-saver-up", volumeUpGlyph, "media_player.volume_up"),
			press("tessera-saver-down", volumeDownGlyph, "media_player.volume_down"),
		)
	}
	if s.Features&(mediaPlay|mediaPause) != 0 {
		glyph := rune(playGlyph)
		if s.Playing {
			glyph = pauseGlyph
		}
		keys = append(keys, press("tessera-saver-play", glyph, "media_player.media_play_pause"))
	}
	return keys
}
