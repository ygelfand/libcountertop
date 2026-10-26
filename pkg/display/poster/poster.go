package poster

import (
	"context"
	"image"
	"log/slog"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/display/photoframe"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/hook"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const (
	check   = 5 * time.Second
	awayFor = 5 * time.Minute
)

type Poster struct {
	options Dependencies
	knobs   *Controls
	next    *esphome.Button
	back    *esphome.Button
	showing *esphome.TextSensor

	Changed hook.Hook[int]

	kick chan struct{}

	frame *photoframe.Frame
}

func New(o Dependencies) *Poster {
	p := &Poster{
		options: o,
		kick:    make(chan struct{}, 1),
		frame:   photoframe.New("wallpaper", o.Photos),
	}
	p.frame.Changed.Listen(func(v int) { p.Changed.Emit(v) })
	p.build()
	return p
}

func (p *Poster) Name() string { return "poster" }

func (p *Poster) Entities() []esphome.Entity {
	return append(p.knobs.Entities(), p.next, p.back, p.showing)
}

func (p *Poster) Settings() *Controls { return p.knobs }

func (p *Poster) Restore(c config.Poster) {
	p.knobs.PublishFrom(c)
	p.showing.Set(c.Last)
}

func (p *Poster) Run(ctx context.Context) error {
	t := time.NewTicker(check)
	defer t.Stop()
	away := false
	for {
		kicked := false
		select {
		case <-ctx.Done():
			return nil
		case <-p.kick:
			kicked = true
		case <-t.C:
		}
		woke := false
		if since := p.options.Touch.Since(); since > awayFor {
			away = true
		} else if away && since < check {
			away, woke = false, true
		}
		p.step(time.Now(), kicked, woke)
	}
}

func (p *Poster) step(now time.Time, kicked, woke bool) {
	cfg := p.options.Settings.Read()
	if !cfg.Enabled {
		p.drop()
		return
	}
	due := kicked || p.frame.Due(now, 0)
	if !due {
		switch every := cfg.Every.Period(); {
		case cfg.Every == config.PosterWake:
			due = woke
		case every > 0:
			due = p.frame.Due(now, every)
		}
	}
	if !due || !p.frame.Next(now) {
		return
	}
	p.record()
}

func (p *Poster) Previous() {
	if p.options.Settings.Read().Enabled && p.frame.Previous(time.Now()) {
		p.record()
	}
}

func (p *Poster) record() {
	pic, _ := p.frame.Showing()
	if err := p.options.Settings.Update(func(c *config.Poster) { c.Last = pic.ID }); err != nil {
		slog.Error("saving a setting failed", "setting", "poster last", "err", err)
	}
	p.showing.Set(pic.ID)
	slog.Info("poster changed", "asset", pic.ID)
}

func (p *Poster) drop() { p.frame.Drop() }

func (p *Poster) Frame() *photoframe.Frame { return p.frame }

func (p *Poster) Shows() bool { return p.options.Settings.Read().Enabled }

func (p *Poster) Look(box image.Rectangle, bg theme.Color) photoframe.Look {
	cfg := p.options.Settings.Read()
	return photoframe.Look{
		Box:    box,
		Back:   bg,
		Fit:    cfg.Fit,
		Change: cfg.Change,
		Span:   cfg.Every.Period(),
	}
}

func (p *Poster) Next() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

func (p *Poster) build() {
	p.knobs = &Controls{
		Table:  NewTable(),
		Device: p.options.DeviceID,
		Read:   p.options.Settings.Read,
		Save:   p.save,
	}

	p.next = &esphome.Button{Base: esphome.Base{
		ObjectID: "poster_next", Name: "Next wallpaper", Icon: "mdi:skip-next",
		Category: esphome.CategoryConfig, DeviceID: p.options.DeviceID,
	}}
	p.next.OnPress = p.Next

	p.back = &esphome.Button{Base: esphome.Base{
		ObjectID: "poster_previous", Name: "Previous wallpaper", Icon: "mdi:skip-previous",
		Category: esphome.CategoryConfig, DeviceID: p.options.DeviceID,
	}}
	p.back.OnPress = p.Previous

	p.showing = &esphome.TextSensor{
		Base: esphome.Base{ObjectID: "poster_showing", Name: "Poster showing", Icon: "mdi:image",
			Category: esphome.CategoryDiagnostic, DeviceID: p.options.DeviceID},
	}
}

func (p *Poster) save(s Setting, v string) error {
	if err := setting.Store(p.options.Settings, s, v); err != nil {
		return err
	}
	if s.Name == "fit" || s.Name == "transition" {
		p.Changed.Emit(0)
	} else if s.Name != "every" {
		p.Next()
	}
	return nil
}

func (p *Poster) change(name, v string) {
	if err := p.knobs.Change(name, v); err != nil {
		slog.Error("saving a setting failed", "setting", "poster "+name, "err", err)
	}
}

func (p *Poster) SetEnabled(on bool) { p.change("on", setting.OnOff(on)) }

func (p *Poster) SetEvery(v config.PosterEvery) { p.change("every", string(v)) }

func (p *Poster) SetFit(v config.PhotoFit) { p.change("fit", string(v)) }

func (p *Poster) SetChange(v config.PhotoTransition) { p.change("transition", string(v)) }

type Touch interface{ Since() time.Duration }

type Dependencies struct {
	Settings storage.Section[config.Poster]
	Touch    Touch
	Photos   photoframe.Source
	DeviceID uint32
}
