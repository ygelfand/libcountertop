package widgets

import (
	"fmt"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/clockview"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	kit "github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/tessera"
)

const (
	clockEntity   = "screen.clock"
	bedsideEntity = "screen.nightstand"
	bedsideKeys   = 3
)

func faceKind(display string) clockview.Face {
	name := schema.DefaultFace
	switch display {
	case "analog":
		name = "analog-seconds"
	case "dial":
		name = "analog"
	case "flip":
		name = "flip"
	}
	f, _ := clockview.Of(name)
	return f
}

func (v *View) clockBody(
	w *gogui.Window,
	id string,
	t tessera.Tile,
	l tessera.Layout,
	f frame,
) func(width, height float32) gogui.View {
	switch t.Entity {
	case clockEntity:
		face := faceKind(t.Display)
		every := time.Minute
		if face.Ticks {
			every = time.Second
		}
		v.tick(w, f.now, every)
		r := clockview.Read(f.now, l.Clock24)
		return func(width, height float32) gogui.View {
			return face.Draw(w, ui.Rect{W: int(width), H: int(height)}, r, f.pal)
		}
	case bedsideEntity:
		v.tick(w, f.now, time.Minute)
		r := clockview.Read(f.now, l.Clock24).Undated()
		keys := bedside(t, l)
		return func(width, height float32) gogui.View {
			reach := style.Reach()
			band := float32(0)
			if len(keys) > 0 {
				band = reach * 2
			}
			content := []gogui.View{
				clockview.Plain(w, ui.Rect{W: int(width), H: int(max(1, height-band))}, r, f.pal),
			}
			if len(keys) > 0 {
				content = append(content, v.bedsideKeys(w, id, keys, width, f))
			}
			return gogui.Column(gogui.ContainerCfg{
				Width:   width,
				Height:  height,
				Sizing:  gogui.FixedFixed,
				Padding: gogui.NoPadding,
				HAlign:  gogui.HAlignCenter,
				Content: content,
			})
		}
	}
	return nil
}

func bedside(t tessera.Tile, l tessera.Layout) []tessera.Tile {
	index := indexOf(t, l)
	if index < 0 {
		return nil
	}
	var keys []tessera.Tile
	for k := range bedsideKeys {
		for _, c := range l.Tiles {
			if c.Received && c.Parent == index && c.Key == k {
				keys = append(keys, c)
				break
			}
		}
	}
	return keys
}

func (v *View) bedsideKeys(
	w *gogui.Window,
	id string,
	keys []tessera.Tile,
	width float32,
	f frame,
) gogui.View {
	reach := style.Reach()
	side := reach * 1.2
	views := make([]gogui.View, 0, len(keys))
	for i, c := range keys {
		kid := fmt.Sprintf("%s-key-%d", id, i)
		var do func(gogui.EventCtx)
		if f.fresh && available(c) {
			do = v.press(w, kid, c, false)
		}
		k := key{glyph: iconFor(c, v.arming(c.Entity)), checked: active(c)}
		views = append(views, gogui.Column(gogui.ContainerCfg{
			Width:   width / float32(len(keys)),
			Sizing:  gogui.FixedFit,
			Padding: gogui.NoPadding,
			Spacing: gogui.SpacingPx(reach * 0.1),
			HAlign:  gogui.HAlignCenter,
			Content: []gogui.View{
				v.panelKey(kid, k, side, do),
				gogui.Label(
					kit.Ellipsize(w, c.Name, secondary(), width/float32(len(keys))),
					secondary(),
				),
			},
		}))
	}
	return gogui.Row(gogui.ContainerCfg{
		Width:   width,
		Sizing:  gogui.FixedFit,
		Padding: gogui.NoPadding,
		Spacing: gogui.SpacingPx(0),
		Content: views,
	})
}
