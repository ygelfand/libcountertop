package photosview

import (
	"sync/atomic"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/apps"
	"github.com/ygelfand/libcountertop/pkg/display/gpu"
	"github.com/ygelfand/libcountertop/pkg/display/interaction"
	"github.com/ygelfand/libcountertop/pkg/display/navigation"
	"github.com/ygelfand/libcountertop/pkg/display/photoframe"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/say"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const (
	forever = 100 * 365 * 24 * time.Hour
	tick    = time.Second
)

type Options struct {
	Shell    *shell.Shell
	Photos   photoframe.Source
	Settings storage.Section[schema.Photos]
	Page     func() shell.View
	GPU      *gpu.Renderer
	Display  photoframe.Display
}

type App struct{}

func (*App) Covers() bool { return true }

func (*App) Timeout() time.Duration { return forever }

type View struct {
	o       Options
	app     *App
	frame   *photoframe.Frame
	stage   *photoframe.Stage
	presses *interaction.Presses
	ticking atomic.Bool
}

func New(o Options) *View {
	v := &View{
		o:       o,
		app:     &App{},
		frame:   photoframe.New("app", o.Photos),
		presses: interaction.New(),
	}
	v.frame.Changed.Listen(func(int) { o.Shell.Redraw() })
	v.stage = photoframe.NewStage(v.frame, photoframe.StageOptions{
		GPU:     o.GPU,
		Display: o.Display,
		Visible: func() bool { return o.Shell.Visible(v.app) },
	})
	return v
}

func (v *View) App() apps.App {
	return apps.App{
		ID:   schema.AppPhotos,
		Name: func() string { return say.T("photos.app") },
		Icon: "mdi:image-multiple",
		Open: func() {
			v.o.Shell.Push(v.app)
			v.frame.Next(time.Now())
			v.run()
		},
	}
}

func (v *View) Screen(s shell.View) *navigation.Screen {
	if s != v.app {
		return nil
	}
	return &navigation.Screen{View: s, Clear: true, Fixed: true, Build: v.build}
}

func (v *View) run() {
	if v.ticking.Swap(true) {
		return
	}
	time.AfterFunc(tick, func() {
		v.ticking.Store(false)
		if !v.o.Shell.Visible(v.app) {
			return
		}
		if every := v.o.Settings.Read().AppEvery.After(); every > 0 {
			if now := time.Now(); v.frame.Due(now, every) {
				v.frame.Next(now)
			}
		}
		v.run()
	})
}

func (v *View) build(w *gogui.Window) gogui.View {
	vw, vh := w.WindowSize()
	whole := ui.Rect{W: vw, H: vh}
	content := []gogui.View{}
	cfg := v.o.Settings.Read()
	look := photoframe.Look{
		Back:   theme.Color{},
		Fit:    cfg.AppFit,
		Change: cfg.AppChange,
		Span:   cfg.AppEvery.After(),
	}
	back := gogui.ColorTransparent
	if !v.stage.Keep(whole, look) {
		back = gogui.RGB(0, 0, 0)
	}
	if _, ok := v.frame.Current(); !ok {
		content = append(content, widgets.Placed(whole, gogui.Column(gogui.ContainerCfg{
			Sizing:  gogui.FillFill,
			HAlign:  gogui.HAlignCenter,
			VAlign:  gogui.VAlignMiddle,
			Content: []gogui.View{gogui.Label(say.T("photos.status.empty"), widgets.Secondary())},
		})))
	}
	content = append(content, photoframe.Taps(whole, func(forward bool) {
		if forward {
			v.frame.Next(time.Now())
			return
		}
		v.frame.Previous(time.Now())
	})...)
	content = append(content,
		widgets.CloseCorner(v.presses, "photos-close", func() { v.o.Shell.Remove(v.app) }),
	)
	if v.o.Page != nil {
		content = append(content, widgets.SettingsCorner(v.presses, "photos-settings", vw,
			func() { v.o.Shell.Push(v.o.Page()) }))
	}
	return gogui.Column(gogui.ContainerCfg{
		ID:      "photos-app",
		Sizing:  gogui.FillFill,
		Padding: gogui.NoPadding,
		Color:   back,
		Content: content,
	})
}
