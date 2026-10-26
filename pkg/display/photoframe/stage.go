package photoframe

import (
	"context"
	_ "embed"
	"errors"
	"image"
	"log/slog"
	"slices"
	"sync"
	"time"

	"golang.org/x/image/draw"

	"github.com/ygelfand/libcountertop/pkg/display/geometry"
	"github.com/ygelfand/libcountertop/pkg/display/gpu"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	stageEvery = time.Second / 60
	changeFor  = 1200 * time.Millisecond
	blurShrink = 24
	pairBase   = 20
	pairFloats = 16
)

var matColor = [3]float32{0xf4 / 255.0, 0xf1 / 255.0, 0xea / 255.0}

//go:embed photo.glsl
var photoShader string

type Display interface{ Orientation() geometry.Orientation }

type StageOptions struct {
	GPU     *gpu.Renderer
	Display Display
	Visible func() bool
}

type pair struct {
	id      string
	w, h    int
	shown   time.Time
	version int
	set     bool
}

type Stage struct {
	o     StageOptions
	frame *Frame

	mu      sync.Mutex
	layer   *gpu.Layer
	area    ui.Rect
	rot     int
	look    Look
	fresh   bool
	broken  bool
	pairs   [2]pair
	newest  int
	changed time.Time
	sent    []float32
	running bool
	stop    context.CancelFunc
	done    chan struct{}
}

func NewStage(f *Frame, o StageOptions) *Stage { return &Stage{o: o, frame: f} }

func (s *Stage) Keep(area ui.Rect, look Look) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken || s.o.GPU == nil || area.W <= 0 || area.H <= 0 {
		return false
	}
	if s.layer != nil && (s.area.W != area.W || s.area.H != area.H || !s.layer.Alive()) {
		s.close()
	}
	if s.layer == nil {
		l, err := s.o.GPU.Open(area.W, area.H)
		if errors.Is(err, gpu.ErrNoHelper) {
			return false
		}
		if err != nil {
			slog.Error("photos: opening the GPU layer", "frame", s.frame.name, "err", err)
			s.broken = true
			return false
		}
		s.layer, s.fresh, s.area = l, true, ui.Rect{}
		s.pairs, s.sent = [2]pair{}, nil
	}
	if rot := int(s.o.Display.Orientation()); s.area != area || s.rot != rot {
		if err := s.layer.Place(area); err != nil {
			if s.layer.Alive() {
				slog.Error("photos: placing the GPU layer", "frame", s.frame.name, "err", err)
				s.broken = true
			}
			s.close()
			return false
		}
		s.area, s.rot = area, rot
	}
	s.look = look
	if !s.running {
		s.running = true
		ctx, cancel := context.WithCancel(context.Background())
		s.stop, s.done = cancel, make(chan struct{})
		go s.run(ctx)
	}
	return true
}

func (s *Stage) Close() {
	s.mu.Lock()
	stop, done := s.stop, s.done
	s.mu.Unlock()
	if stop != nil {
		stop()
		<-done
	}
}

func (s *Stage) run(ctx context.Context) {
	defer func() {
		s.mu.Lock()
		s.close()
		s.running = false
		close(s.done)
		s.mu.Unlock()
	}()
	t := time.NewTicker(stageEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if s.o.Visible != nil && !s.o.Visible() {
				return
			}
			s.shade(now)
		}
	}
}

func (s *Stage) close() {
	if s.layer != nil {
		_ = s.layer.Close()
		s.layer = nil
	}
}

func (s *Stage) shade(now time.Time) {
	shot, ok := s.frame.Current()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.layer == nil {
		return
	}
	if err := s.draw(shot, ok, now); err != nil {
		if s.layer.Alive() {
			slog.Error("photos: GPU layer failed", "frame", s.frame.name, "err", err)
			s.broken = true
		}
		s.close()
	}
}

func (s *Stage) draw(shot Shot, ok bool, now time.Time) error {
	if s.fresh {
		if err := s.layer.Program(photoShader, 0); err != nil {
			return err
		}
		s.fresh = false
	}
	cur := &s.pairs[s.newest]
	switch {
	case !ok:
		s.pairs = [2]pair{}
	case shot.Version != cur.version || !cur.set:
		k := s.newest
		blend := cur.set && s.look.Change != schema.TransitionCut
		if blend {
			k = 1 - k
		}
		if err := s.load(k, shot); err != nil {
			return err
		}
		s.newest = k
		s.changed = time.Time{}
		if blend {
			now = time.Now()
			s.changed = now
		}
	}
	vals := s.values(now)
	if slices.Equal(vals, s.sent) {
		return nil
	}
	s.sent = vals
	return s.layer.Values(vals, 0, 0, 0)
}

func (s *Stage) load(k int, shot Shot) error {
	sharp := fit(shot.Image, max(s.area.W, s.area.H))
	b := sharp.Bounds()
	if err := s.layer.Texture(k, sharp); err != nil {
		return err
	}
	if err := s.layer.Texture(2+k, blurred(sharp)); err != nil {
		return err
	}
	s.pairs[k] = pair{
		id:      shot.ID,
		w:       b.Dx(),
		h:       b.Dy(),
		shown:   shot.Shown,
		version: shot.Version,
		set:     true,
	}
	return nil
}

func (s *Stage) values(now time.Time) []float32 {
	w, h := s.area.W, s.area.H
	look := s.look
	u := make([]float32, pairBase+2*pairFloats)
	p := float32(1)
	old := s.pairs[1-s.newest]
	if !s.changed.IsZero() && old.set {
		p = min(float32(now.Sub(s.changed))/float32(changeFor), 1)
	}
	u[0] = p
	u[1] = float32(slices.Index(schema.PhotoTransitions(), look.Change))
	u[2] = float32(s.newest)
	if p < 1 {
		u[3] = 1
	}
	u[4], u[5], u[6] = float32(look.Back.R)/255, float32(look.Back.G)/255, float32(look.Back.B)/255
	if box := look.Box; !box.Empty() {
		side := float32(min(box.Dx(), box.Dy()))
		u[7] = scrimMix
		u[8], u[9], u[10], u[11] = float32(
			box.Min.X,
		), float32(
			box.Min.Y,
		), float32(
			box.Dx(),
		), float32(
			box.Dy(),
		)
		u[12], u[13] = side*scrimPad, side*scrimSoft
	}
	u[14], u[15], u[16] = matColor[0], matColor[1], matColor[2]
	for k, pr := range s.pairs {
		if !pr.set || (k != s.newest && p >= 1) {
			continue
		}
		pl := place(look.Fit, pr.w, pr.h, w, h, pr.id, now.Sub(pr.shown), look.Span)
		at := u[pairBase+k*pairFloats:]
		copy(at[0:4], pl.dst[:])
		copy(at[4:8], pl.src[:])
		at[8] = float32(pl.back)
		copy(at[9:13], pl.blur[:])
		at[13], at[14], at[15] = pl.mat, pl.shade, 1
	}
	return u
}

func fit(src image.Image, most int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if most > 0 && max(w, h) > most {
		if w >= h {
			w, h = most, max(h*most/w, 1)
		} else {
			w, h = max(w*most/h, 1), most
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	if w == b.Dx() && h == b.Dy() {
		draw.Draw(out, out.Bounds(), src, b.Min, draw.Src)
		return out
	}
	draw.ApproxBiLinear.Scale(out, out.Bounds(), src, b, draw.Src, nil)
	return out
}

func blurred(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, max(b.Dx()/blurShrink, 2), max(b.Dy()/blurShrink, 2)))
	draw.CatmullRom.Scale(out, out.Bounds(), src, b, draw.Src, nil)
	return out
}
