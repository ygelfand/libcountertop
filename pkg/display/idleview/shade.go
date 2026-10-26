package idleview

import (
	"errors"
	"log/slog"
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/gpu"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/visual"
	"github.com/ygelfand/libcountertop/pkg/display/visualview"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const shadeEvery = time.Second / 60

type shading struct {
	layer  *gpu.Layer
	vis    visual.Visual
	source config.Source
	area   ui.Rect
	rot    int
	fresh  bool
	last   time.Time
}

func (v *View) shaded(n int, vis visual.Visual, area ui.Rect, source config.Source) {
	v.gmu.Lock()
	defer v.gmu.Unlock()
	if v.broken[n] == vis {
		return
	}
	g := v.gl[n]
	if g != nil && (g.vis != vis || g.area.W != area.W || g.area.H != area.H || !g.layer.Alive()) {
		v.unshade(n)
		g = nil
	}
	if g == nil {
		l, err := v.options.GPU.Open(area.W, area.H)
		if errors.Is(err, gpu.ErrNoHelper) {
			return
		}
		if err != nil {
			slog.Error("idle: opening the GPU layer", "err", err)
			v.broken[n] = vis
			return
		}
		g = &shading{layer: l, vis: vis, fresh: true}
		v.gl[n] = g
	}
	g.source = source
	if rot := int(v.options.Display.Orientation()); g.area != area || g.rot != rot {
		if err := g.layer.Place(area); err != nil {
			if g.layer.Alive() {
				slog.Error("idle: placing the GPU layer", "err", err)
				v.broken[n] = vis
			}
			v.unshade(n)
			return
		}
		g.area, g.rot = area, rot
	}
}

func (v *View) unshade(n int) {
	if g := v.gl[n]; g != nil {
		_ = g.layer.Close()
		v.gl[n] = nil
	}
}

func (v *View) unshadeSlot(n int) {
	v.gmu.Lock()
	defer v.gmu.Unlock()
	v.unshade(n)
}

func (v *View) unshadeAll() {
	v.gmu.Lock()
	defer v.gmu.Unlock()
	for n := range v.gl {
		v.unshade(n)
	}
}

func (v *View) shade() {
	v.gmu.Lock()
	defer v.gmu.Unlock()
	var x visual.Input
	for n, g := range v.gl {
		if g == nil {
			continue
		}
		if x.Now == 0 {
			x = v.options.Visuals.Input()
		}
		now := time.Now()
		in := visualview.From(x, g.source)
		if !g.last.IsZero() {
			in.Dt = now.Sub(g.last)
		}
		g.last = now
		if err := g.vis.Shade(g.layer, g.fresh, g.area, in); err != nil {
			if g.layer.Alive() {
				slog.Error("idle: GPU visual failed", "err", err)
				v.broken[n] = g.vis
			}
			v.unshade(n)
			continue
		}
		if g.fresh {
			slog.Debug(
				"idle: visual ready",
				"slot",
				n,
				"took",
				time.Since(now).Round(time.Millisecond),
			)
		}
		g.fresh = false
	}
}

func (v *View) painting(slots []config.IdleVisual) bool {
	v.gmu.Lock()
	defer v.gmu.Unlock()
	for n, slot := range slots {
		if !slot.Photos() && v.gl[n] == nil && v.broken[n] == nil {
			return true
		}
	}
	return false
}
