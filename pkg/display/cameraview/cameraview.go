package cameraview

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"
	"golang.org/x/sync/errgroup"

	"github.com/ygelfand/libcountertop/pkg/display/apps"
	"github.com/ygelfand/libcountertop/pkg/display/callvideo"
	"github.com/ygelfand/libcountertop/pkg/display/dashboard"
	"github.com/ygelfand/libcountertop/pkg/display/icons"
	"github.com/ygelfand/libcountertop/pkg/display/interaction"
	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/navigation"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/theme"
	"github.com/ygelfand/libcountertop/pkg/display/ui"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/homecontrol"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const (
	listEvery   = 4 * time.Minute
	fetchWait   = 10 * time.Second
	failedRetry = time.Minute
	liveRetry   = time.Second
	linger      = 3 * time.Minute
	fetchAtMost = 8
	gapShare    = 0.05
	TabKind     = "cameras"
)

var scaleShare = map[int]float64{1: 0.2, 2: 0.25, 3: 1.0 / 3, 4: 0.5, 5: 1}

type Source interface {
	Cameras(context.Context) ([]homeassistant.Camera, error)
}

type Site interface{ URL() string }

type Options struct {
	Shell    *shell.Shell
	Source   Source
	Streams  Streams
	Speaker  Speaker
	Display  callvideo.Display
	Site     Site
	Settings storage.Section[schema.Cameras]
	Client   *http.Client
	Picker   func() shell.View
	Tabs     *dashboard.Tabs
}

type Grid struct{}

func (*Grid) Covers() bool { return true }

func (*Grid) Timeout() time.Duration { return linger }

type Live struct{}

func (*Live) Covers() bool { return true }

func (*Live) Timeout() time.Duration { return linger }

type frame struct {
	key string
	src string
	w   int
	h   int
}

type Cameras struct {
	o     Options
	grid  *Grid
	live  *Live
	knobs *setting.Controls[schema.Cameras]

	mu        sync.Mutex
	list      []homeassistant.Camera
	listErr   error
	frames    map[string]frame
	failing   map[string]time.Time
	fetching  map[string]struct{}
	gen       uint64
	page      int
	pages     int
	shown     []string
	tile      image.Point
	screen    image.Point
	pager     *widgets.Pager
	presses   *interaction.Presses
	win       *gogui.Window
	watching  string
	why       string
	liveBegan time.Time
	spinning  atomic.Bool
	liveShot  frame
	video     *callvideo.Stream
	refused   map[string]struct{}
	audible   bool
	stopGrid  context.CancelFunc
	stopLive  context.CancelFunc
}

func New(o Options) *Cameras {
	c := &Cameras{
		o:        o,
		grid:     &Grid{},
		live:     &Live{},
		frames:   map[string]frame{},
		failing:  map[string]time.Time{},
		fetching: map[string]struct{}{},
		refused:  map[string]struct{}{},
		pager:    widgets.NewPager("cameras-slide"),
		presses:  interaction.New(),
	}
	c.knobs = &setting.Controls[schema.Cameras]{
		Table: NewTable(),
		Read:  o.Settings.Read,
		Save: func(s Setting, v string) error {
			err := setting.Store(o.Settings, s, v)
			c.follow()
			return err
		},
	}
	o.Shell.Changed.Listen(func(shell.Change) { c.follow() })
	if o.Tabs != nil {
		o.Tabs.Changed.Listen(func(struct{}) { c.follow() })
	}
	return c
}

func (c *Cameras) Controls() *setting.Controls[schema.Cameras] { return c.knobs }

func (c *Cameras) App() apps.App {
	return apps.App{
		ID:   schema.AppCameras,
		Name: func() string { return say.T("cameras.app") },
		Icon: "mdi:cctv",
		Open: func() { c.o.Shell.Push(c.grid) },
	}
}

func (c *Cameras) Screen(v shell.View) *navigation.Screen {
	switch v {
	case c.grid:
		return &navigation.Screen{View: v, Clear: true, Build: c.buildGrid}
	case c.live:
		return &navigation.Screen{View: v, Clear: true, Fixed: true, Build: c.buildLive}
	}
	return nil
}

func (c *Cameras) follow() {
	grid := c.o.Shell.Visible(c.grid) || c.tabbed()
	live := c.o.Shell.Visible(c.live)
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case grid && c.stopGrid == nil:
		ctx, stop := context.WithCancel(context.Background())
		c.stopGrid = stop
		safe.Go("cameras grid", func() { c.runGrid(ctx) })
	case !grid && c.stopGrid != nil:
		c.stopGrid()
		c.stopGrid = nil
	}
	if !live && c.stopLive != nil {
		c.stopLive()
		c.stopLive = nil
	}
}

func (c *Cameras) runGrid(ctx context.Context) {
	cfg := c.o.Settings.Read()
	c.refreshList(ctx)
	c.fetchShown(ctx)
	list := time.NewTicker(listEvery)
	defer list.Stop()
	refresh, stopRefresh := every(cfg.Refresh)
	defer stopRefresh()
	rotate, stopRotate := every(cfg.Rotate)
	defer stopRotate()
	for {
		select {
		case <-ctx.Done():
			return
		case <-list.C:
			c.refreshList(ctx)
		case <-refresh:
			c.fetchShown(ctx)
		case <-rotate:
			c.turn(1)
			c.fetchShown(ctx)
		}
	}
}

func every(seconds int) (<-chan time.Time, func()) {
	if seconds <= 0 {
		return nil, func() {}
	}
	t := time.NewTicker(time.Duration(seconds) * time.Second)
	return t.C, t.Stop
}

func (c *Cameras) refreshList(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, fetchWait)
	defer cancel()
	list, err := c.o.Source.Cameras(ctx)
	c.mu.Lock()
	if err == nil {
		c.list = list
	}
	c.listErr = err
	c.mu.Unlock()
	if err != nil {
		slog.Warn("listing cameras failed", "err", err)
	}
	c.o.Shell.Redraw()
}

func (c *Cameras) picked() []homeassistant.Camera {
	pick := c.o.Settings.Read().Pick
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []homeassistant.Camera
	for _, cam := range c.list {
		if homecontrol.Includes(pick, cam.Entity) {
			out = append(out, cam)
		}
	}
	return out
}

func (c *Cameras) camera(id string) (homeassistant.Camera, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := slices.IndexFunc(c.list, func(cam homeassistant.Camera) bool { return cam.ID == id })
	if i < 0 {
		return homeassistant.Camera{}, false
	}
	return c.list[i], true
}

func (c *Cameras) fetchShown(ctx context.Context) {
	c.mu.Lock()
	shown := slices.Clone(c.shown)
	c.mu.Unlock()
	c.fetch(ctx, shown)
}

func (c *Cameras) fetch(ctx context.Context, wanted []string) {
	c.mu.Lock()
	size, now := c.tile, time.Now()
	var ids []string
	for _, id := range wanted {
		if _, busy := c.fetching[id]; busy || now.Before(c.failing[id]) {
			continue
		}
		c.fetching[id] = struct{}{}
		ids = append(ids, id)
	}
	c.mu.Unlock()
	base := c.o.Site.URL()
	var g errgroup.Group
	g.SetLimit(fetchAtMost)
	for _, id := range ids {
		g.Go(func() error {
			defer func() {
				c.mu.Lock()
				delete(c.fetching, id)
				c.mu.Unlock()
			}()
			cam, ok := c.camera(id)
			if !ok || cam.Picture == "" {
				return nil
			}
			ctx, cancel := context.WithTimeout(ctx, fetchWait)
			defer cancel()
			began := time.Now()
			img, err := snapshot(ctx, c.o.Client, base, cam.Picture, size.X, size.Y)
			slog.Debug("camera snapshot", "camera", id, "took", time.Since(began), "err", err)
			if err != nil {
				c.mu.Lock()
				c.failing[id] = time.Now().Add(failedRetry)
				c.mu.Unlock()
				return nil
			}
			c.keep(id, img)
			c.o.Shell.Redraw()
			return nil
		})
	}
	_ = g.Wait()
}

func (c *Cameras) keep(id string, img *image.RGBA) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.failing, id)
	c.gen++
	key := fmt.Sprintf("camera/%s/%d", id, c.gen)
	src := gogui.UseImage(key, img.Rect.Dx(), img.Rect.Dy(), img.Pix)
	if old, ok := c.frames[id]; ok {
		gogui.DropImage(old.key)
	}
	c.frames[id] = frame{key: key, src: src, w: img.Rect.Dx(), h: img.Rect.Dy()}
}

func (c *Cameras) turn(by int) {
	c.mu.Lock()
	from, win, width := c.page, c.win, float32(c.screen.X)
	if c.pages > 1 {
		c.page = ((c.page+by)%c.pages + c.pages) % c.pages
	}
	to := c.page
	c.mu.Unlock()
	if win != nil && from != to {
		win.QueueCommand(func(w *gogui.Window) { c.pager.Slid(w, from, to, by > 0, width) })
	}
	c.o.Shell.Redraw()
}

func (c *Cameras) neighbour(by int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pages <= 1 {
		return -1
	}
	return ((c.page+by)%c.pages + c.pages) % c.pages
}

func (c *Cameras) turnTo(to int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	from := c.page
	c.page = to
	return from
}

func RowHeight(scale int, w float64) float64 {
	share, ok := scaleShare[scale]
	if !ok {
		share = scaleShare[3]
	}
	return w * share / unknownShape
}

func (c *Cameras) buildGrid(w *gogui.Window) gogui.View {
	vw, vh := w.WindowSize()
	return c.collage(w, ui.Rect{W: vw, H: vh}, true)
}

func (c *Cameras) Board(w *gogui.Window, at ui.Rect, _ dashboard.Tab, _ theme.Theme) gogui.View {
	return widgets.Placed(at, c.collage(w, ui.Rect{W: at.W, H: at.H}, false))
}

func (*Cameras) Glyph(dashboard.Tab) string { return gogui.IconVideo }

func (c *Cameras) DashboardTabs() []dashboard.Tab {
	if cfg := c.o.Settings.Read(); !cfg.Tab || cfg.Pick.Empty() {
		return nil
	}
	return []dashboard.Tab{{Kind: TabKind, Key: TabKind, Name: say.T("cameras.app")}}
}

func (c *Cameras) tabbed() bool {
	if c.o.Tabs == nil || c.o.Shell.Covered() {
		return false
	}
	t, ok := c.o.Tabs.Showing()
	return ok && t.Kind == TabKind
}

func (c *Cameras) collage(w *gogui.Window, at ui.Rect, full bool) gogui.View {
	vw, vh := at.W, at.H
	cams := c.picked()
	if len(cams) == 0 {
		if !full {
			return c.note(c.emptyNote())
		}
		return gogui.Column(gogui.ContainerCfg{
			Sizing:  gogui.FillFill,
			Padding: gogui.NoPadding,
			Color:   gogui.RGB(0, 0, 0),
			Content: append(c.corners(vw), c.note(c.emptyNote())),
		})
	}
	target := RowHeight(c.o.Settings.Read().Scale, float64(vw))

	c.mu.Lock()
	shapes := make([]float64, len(cams))
	for i, cam := range cams {
		shapes[i] = unknownShape
		if f, ok := c.frames[cam.ID]; ok && f.w > 0 && f.h > 0 {
			shapes[i] = float64(f.w) / float64(f.h)
		}
	}
	c.mu.Unlock()
	layout := Collage(shapes, float64(vw), float64(vh), target, float64(style.Reach()*gapShare))

	c.mu.Lock()
	c.win = w
	c.pages = len(layout)
	c.page = min(c.page, c.pages-1)
	shown, _ := onPage(cams, layout, c.page)
	ids := make([]string, len(shown))
	for i, cam := range shown {
		ids[i] = cam.ID
	}
	var added []string
	for _, id := range ids {
		if !slices.Contains(c.shown, id) {
			added = append(added, id)
		}
	}
	c.shown, c.screen = ids, image.Point{X: vw, Y: vh}
	c.tile = image.Point{X: vw, Y: int(math.Ceil(min(float64(vh), target)))}
	page, pages := c.page, c.pages
	c.mu.Unlock()
	if len(added) > 0 {
		safe.Go("cameras page", func() { c.fetch(context.Background(), added) })
	}

	s := c.pager.Now()
	views := []gogui.View{
		widgets.Placed(
			ui.Rect{X: int(s.Offset), W: vw, H: vh},
			c.pageView(onPage(cams, layout, page)),
		),
	}
	if s.Moving() && s.Other < pages {
		views = append(views, widgets.Placed(
			ui.Rect{X: int(s.OtherShift(float32(vw))), W: vw, H: vh},
			c.pageView(onPage(cams, layout, s.Other)),
		))
	}
	if pages > 1 {
		dots := int(style.Reach() * 0.8)
		views = append(views, widgets.PageDots(
			"cameras-dot",
			ui.Rect{Y: vh - dots, W: vw, H: dots},
			pages,
			page,
		))
	}
	cfg := gogui.ContainerCfg{
		ID:        "cameras",
		Sizing:    gogui.FillFill,
		Padding:   gogui.NoPadding,
		Clip:      true,
		OnGesture: c.swipe,
	}
	if full {
		views = append(views, c.corners(vw)...)
		cfg.Color = gogui.RGB(0, 0, 0)
	}
	cfg.Content = views
	return gogui.Column(cfg)
}

func onPage(
	cams []homeassistant.Camera,
	layout [][]ui.Rect,
	page int,
) ([]homeassistant.Camera, []ui.Rect) {
	if page < 0 || page >= len(layout) {
		return nil, nil
	}
	from := 0
	for _, p := range layout[:page] {
		from += len(p)
	}
	return cams[from : from+len(layout[page])], layout[page]
}

func (c *Cameras) pageView(shown []homeassistant.Camera, rects []ui.Rect) gogui.View {
	c.mu.Lock()
	frames := make([]frame, len(shown))
	for i, cam := range shown {
		frames[i] = c.frames[cam.ID]
	}
	c.mu.Unlock()

	tiles := make([]gogui.View, len(shown))
	for i, cam := range shown {
		r := rects[i]
		tiles[i] = widgets.Placed(r, c.tileView(cam, frames[i], float32(r.W), float32(r.H)))
	}
	return gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFill,
		Padding: gogui.NoPadding,
		Content: tiles,
	})
}

func (c *Cameras) swipe(e gogui.EventCtx) {
	c.mu.Lock()
	width := float32(c.screen.X)
	c.mu.Unlock()
	ev := e.Event
	switch ev.GestureType {
	case gogui.GesturePan:
		switch ev.GesturePhase {
		case gogui.GesturePhaseBegan, gogui.GesturePhaseChanged:
			if math.Abs(float64(ev.GestureDX)) >= math.Abs(float64(ev.GestureDY)) {
				c.pager.Drag(e.Window, ev.GestureDX, width, c.neighbour, c.turnTo)
			}
		default:
			c.pager.Lift(e.Window, 0)
		}
	case gogui.GestureSwipe:
		c.pager.Lift(e.Window, ev.VelocityX)
	default:
		return
	}
	e.Consume()
	e.Window.InvalidateLayout()
}

func (c *Cameras) tileView(cam homeassistant.Camera, f frame, tw, th float32) gogui.View {
	var content []gogui.View
	if f.src != "" {
		content = append(content, gogui.Image(gogui.ImageCfg{
			Src:    f.src,
			Width:  tw,
			Height: th,
		}))
	} else {
		content = append(content, gogui.Label(cam.Name, widgets.Secondary()))
	}
	return c.presses.Pressable(gogui.Column, gogui.ContainerCfg{
		ID:      "camera-" + cam.ID,
		Width:   tw,
		Height:  th,
		Sizing:  gogui.FixedFixed,
		Padding: gogui.NoPadding,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Content: content,
	}, func(gogui.EventCtx) { c.watch(cam.ID) })
}

func (c *Cameras) Picture(ctx context.Context, entity string, w, h int) (*image.RGBA, error) {
	if _, ok := c.camera(entity); !ok {
		c.refreshList(ctx)
	}
	cam, ok := c.camera(entity)
	if !ok || cam.Picture == "" {
		return nil, errNoPicture
	}
	return snapshot(ctx, c.o.Client, c.o.Site.URL(), cam.Picture, w, h)
}

func (c *Cameras) Watch(entity string) bool {
	if _, ok := c.camera(entity); !ok {
		c.refreshList(context.Background())
	}
	if _, ok := c.camera(entity); !ok {
		return false
	}
	c.watch(entity)
	return true
}

func (c *Cameras) watch(id string) {
	c.mu.Lock()
	if c.stopLive != nil {
		c.stopLive()
	}
	ctx, stop := context.WithCancel(context.Background())
	c.stopLive, c.watching, c.why, c.liveBegan = stop, id, "", time.Now()
	if c.liveShot.key != "" {
		gogui.DropImage(c.liveShot.key)
	}
	c.liveShot = frame{}
	c.mu.Unlock()
	c.o.Shell.Push(c.live)
	safe.Go("camera live", func() { c.runLive(ctx, id) })
}

func (c *Cameras) runLive(ctx context.Context, id string) {
	pics, stopPics := c.startPictures(ctx, id)
	defer func() { stopPics() }()
	cam, ok := c.camera(id)
	if !ok {
		<-ctx.Done()
		return
	}
	if why := c.unstreamable(cam); why != "" {
		c.explain(id, why, nil)
		<-ctx.Done()
		return
	}
	err := c.stream(ctx, cam, stopPics)
	if ctx.Err() != nil {
		return
	}
	c.explain(id, reason(err), err)
	if errors.Is(err, errUndecodable) {
		c.mu.Lock()
		c.refused[id] = struct{}{}
		c.mu.Unlock()
	}
	if pics.Err() != nil {
		_, stopPics = c.startPictures(ctx, id)
	}
	<-ctx.Done()
}

func (c *Cameras) unstreamable(cam homeassistant.Camera) string {
	switch {
	case c.o.Display == nil:
		return say.T("cameras.why.display")
	case c.o.Streams == nil || !c.o.Streams.Speaks(homeassistant.WebRTCProtocol):
		return say.T("cameras.why.webrtc")
	case !cam.Streams():
		return say.T("cameras.why.pictures")
	case c.undecodable(cam.ID):
		return say.T("cameras.why.format")
	}
	return ""
}

func (c *Cameras) explain(id, why string, err error) {
	slog.Warn("camera showing pictures", "camera", id, "why", why, "err", err)
	c.mu.Lock()
	if c.watching == id {
		c.why = why
	}
	c.mu.Unlock()
	c.o.Shell.Redraw()
}

func reason(err error) string {
	var refused *homeassistant.Failed
	switch {
	case errors.Is(err, errUndecodable):
		return say.T("cameras.why.format")
	case errors.Is(err, errNoPicture):
		return say.T("cameras.why.nopicture")
	case errors.Is(err, errLost):
		return say.T("cameras.why.lost")
	case errors.Is(err, homeassistant.ErrNoWebRTC):
		return say.T("cameras.why.webrtc")
	case errors.As(err, &refused):
		return say.F("cameras.why.refused", map[string]any{"Message": refused.Message})
	case errors.Is(err, context.DeadlineExceeded):
		return say.T("cameras.why.slow")
	}
	return say.T("cameras.why.failed")
}

func (c *Cameras) startPictures(
	ctx context.Context,
	id string,
) (context.Context, context.CancelFunc) {
	pics, stop := context.WithCancel(ctx)
	safe.Go("camera pictures", func() { c.pictures(pics, id) })
	return pics, stop
}

func (c *Cameras) undecodable(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.refused[id]
	return ok
}

func (c *Cameras) pictures(ctx context.Context, id string) {
	for ctx.Err() == nil {
		cam, ok := c.camera(id)
		if !ok {
			return
		}
		c.mu.Lock()
		size := c.screen
		c.mu.Unlock()
		err := stream(
			ctx,
			c.o.Client,
			c.o.Site.URL(),
			cam.Picture,
			size.X,
			size.Y,
			func(img *image.RGBA) {
				c.showLive(id, img)
			},
		)
		if ctx.Err() != nil {
			return
		}
		slog.Debug("camera live stream ended", "camera", id, "err", err)
		snap, sctx := context.WithTimeout(ctx, fetchWait)
		if img, err := snapshot(
			snap,
			c.o.Client,
			c.o.Site.URL(),
			cam.Picture,
			size.X,
			size.Y,
		); err == nil {
			c.showLive(id, img)
		}
		sctx()
		select {
		case <-ctx.Done():
			return
		case <-time.After(liveRetry):
		}
	}
}

func (c *Cameras) showLive(id string, img *image.RGBA) {
	c.mu.Lock()
	if c.watching != id {
		c.mu.Unlock()
		return
	}
	c.gen++
	key := fmt.Sprintf("camera/live/%d", c.gen)
	src := gogui.UseImage(key, img.Rect.Dx(), img.Rect.Dy(), img.Pix)
	if c.liveShot.key != "" {
		gogui.DropImage(c.liveShot.key)
	}
	c.liveShot = frame{key: key, src: src, w: img.Rect.Dx(), h: img.Rect.Dy()}
	c.mu.Unlock()
	c.o.Shell.Redraw()
}

func (c *Cameras) buildLive(w *gogui.Window) gogui.View {
	vw, vh := w.WindowSize()
	c.mu.Lock()
	f, id, began, video, audible, why := c.liveShot, c.watching, c.liveBegan, c.video, c.audible, c.why
	thumb := c.frames[id]
	c.mu.Unlock()
	var content []gogui.View
	back := gogui.RGB(0, 0, 0)
	switch {
	case video != nil:
		back = gogui.ColorTransparent
	case f.src != "":
		content = append(content, gogui.Image(gogui.ImageCfg{
			Src:    f.src,
			Width:  float32(f.w),
			Height: float32(f.h),
		}))
	case thumb.src != "":
		sw, sh := enlarge(thumb.w, thumb.h, vw, vh)
		content = append(
			content,
			gogui.Image(gogui.ImageCfg{Src: thumb.src, Width: sw, Height: sh}),
		)
	default:
		if cam, ok := c.camera(id); ok {
			content = append(content, gogui.Label(cam.Name, widgets.Secondary()))
		}
	}
	if video == nil && f.src == "" && why == "" {
		content = append(content,
			widgets.Placed(ui.Rect{W: vw, H: vh}, widgets.Spinner(vw, vh, time.Since(began))))
		c.spinAgain(w)
	}
	content = append(content, widgets.CloseCorner(c.presses, "camera-live-close", c.closeLive))
	if video != nil || f.src != "" {
		content = append(content, mode(vw, video != nil))
	}
	if video == nil && why != "" {
		content = append(content, caption(vw, why))
	}
	if video != nil && c.o.Speaker != nil {
		content = append(content, c.sounder(vw, vh, audible))
	}
	return gogui.Column(gogui.ContainerCfg{
		ID:      "camera-live",
		Sizing:  gogui.FillFill,
		Padding: gogui.NoPadding,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Color:   back,
		Content: content,
		OnClick: func(e gogui.EventCtx) {
			c.closeLive()
			e.Consume()
		},
	})
}

func (c *Cameras) closeLive() {
	c.o.Shell.Remove(c.live)
	c.mu.Lock()
	if c.liveShot.key != "" {
		gogui.DropImage(c.liveShot.key)
	}
	c.liveShot = frame{}
	c.mu.Unlock()
}

func mode(vw int, live bool) gogui.View {
	st := gogui.CurrentTheme().TextStyleIconMedium
	st.Family = icons.Family
	st.Size = style.Reach() * 0.5
	st.Color = gogui.RGB(0xff, 0xff, 0xff)
	glyph := icons.Glyph(icons.Named("mdi:image"))
	if live {
		glyph = icons.Glyph(icons.Named("mdi:video"))
	}
	side := int(st.Size * 2)
	return widgets.Placed(
		ui.Rect{X: (vw - side) / 2, Y: int(gogui.PadLarge), W: side, H: side},
		gogui.Row(gogui.ContainerCfg{
			Width:   float32(side),
			Height:  float32(side),
			Sizing:  gogui.FixedFixed,
			Radius:  gogui.RadiusLarge,
			Color:   gogui.RGBA(0, 0, 0, 0x99),
			HAlign:  gogui.HAlignCenter,
			VAlign:  gogui.VAlignMiddle,
			Padding: gogui.NoPadding,
			Content: []gogui.View{gogui.Label(glyph, st)},
		}),
	)
}

func caption(vw int, text string) gogui.View {
	st := gogui.CurrentTheme().Cfg.TextStyleDef
	st.Color = gogui.RGB(0xff, 0xff, 0xff)
	badge := int(style.Reach()*0.5) * 2
	top := int(gogui.PadLarge)*2 + badge
	return widgets.Placed(
		ui.Rect{X: 0, Y: top, W: vw, H: int(st.Size * 2.4)},
		gogui.Row(gogui.ContainerCfg{
			Sizing:  gogui.FillFill,
			HAlign:  gogui.HAlignCenter,
			VAlign:  gogui.VAlignMiddle,
			Padding: gogui.NoPadding,
			Content: []gogui.View{gogui.Row(gogui.ContainerCfg{
				Sizing: gogui.FitFit,
				Radius: gogui.RadiusLarge,
				Color:  gogui.RGBA(0, 0, 0, 0x99),
				Padding: gogui.NewPadding(
					gogui.PadSmall,
					gogui.PadLarge,
					gogui.PadSmall,
					gogui.PadLarge,
				),
				Content: []gogui.View{gogui.Label(text, st)},
			})},
		}),
	)
}

func (c *Cameras) sounder(vw, vh int, audible bool) gogui.View {
	st := gogui.CurrentTheme().TextStyleIconMedium
	st.Family = icons.Family
	st.Size = style.Reach() * 1.4
	st.Color = gogui.RGB(0xff, 0xff, 0xff)
	glyph := icons.Glyph(icons.Named("mdi:volume-off"))
	if audible {
		glyph = icons.Glyph(icons.Named("mdi:volume-high"))
	}
	side := int(st.Size * 2)
	pad := int(gogui.PadLarge)
	return widgets.Placed(
		ui.Rect{X: (vw - side) / 2, Y: vh - pad - side, W: side, H: side},
		c.presses.Button(
			"camera-live-sound",
			glyph,
			st,
			gogui.RGBA(0, 0, 0, 0x99),
			func(gogui.EventCtx) { c.toggleSound() },
		),
	)
}

func (c *Cameras) spinAgain(w *gogui.Window) {
	if c.spinning.Swap(true) {
		return
	}
	time.AfterFunc(widgets.SpinStep, func() {
		c.spinning.Store(false)
		w.QueueCommand(func(w *gogui.Window) { w.InvalidateLayout() })
	})
}

func enlarge(w, h, vw, vh int) (float32, float32) {
	if w <= 0 || h <= 0 {
		return float32(vw), float32(vh)
	}
	sw, sh := float32(vw), float32(h)*float32(vw)/float32(w)
	if sh > float32(vh) {
		sw, sh = float32(w)*float32(vh)/float32(h), float32(vh)
	}
	return sw, sh
}

func (c *Cameras) emptyNote() string {
	c.mu.Lock()
	err, listed := c.listErr, c.list != nil
	c.mu.Unlock()
	switch {
	case c.o.Site.URL() == "":
		return say.T("cameras.nosite")
	case err != nil:
		return say.T("cameras.failed")
	case !listed:
		return say.T("cameras.loading")
	}
	return say.T("cameras.none")
}

func (c *Cameras) corners(vw int) []gogui.View {
	return []gogui.View{
		widgets.CloseCorner(c.presses, "cameras-close", func() { c.o.Shell.Remove(c.grid) }),
		widgets.SettingsCorner(
			c.presses,
			"cameras-settings",
			vw,
			func() { c.o.Shell.Push(c.Page()) },
		),
	}
}

func (c *Cameras) note(text string) gogui.View {
	return gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFill,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Content: []gogui.View{gogui.Label(text, widgets.Secondary())},
	})
}

func (c *Cameras) Page() *shell.Page {
	return &shell.Page{Title: say.T("cameras.app"), Build: func() ([]menu.Row, []func(int)) {
		cfg := c.o.Settings.Read()
		scale := menu.Steps(schema.CameraScaleMost - schema.CameraScaleLeast + 1)
		refresh, rotate := menu.Steps(len(refreshSteps)), menu.Steps(len(rotateSteps))
		rows := []menu.Row{
			{Label: say.T("cameras.setting.tab"), Kind: menu.Toggle, On: cfg.Tab},
			{
				Label: say.T("cameras.pick"),
				Kind:  menu.Chevron,
				Value: homecontrol.Summary(cfg.Pick),
			},
			{
				Label: say.T("cameras.setting.scale"),
				Kind:  menu.Slider,
				Level: scale.Level(cfg.Scale - schema.CameraScaleLeast),
				Snap:  scale.Snap,
				Value: strconv.Itoa(cfg.Scale),
			},
			{
				Label: say.T("cameras.setting.refresh"),
				Kind:  menu.Slider,
				Level: refresh.Level(
					slices.Index(refreshSteps, nearest(refreshSteps, cfg.Refresh)),
				),
				Snap:  refresh.Snap,
				Value: seconds(cfg.Refresh),
			},
			{
				Label: say.T("cameras.setting.rotate"),
				Kind:  menu.Slider,
				Level: rotate.Level(slices.Index(rotateSteps, nearest(rotateSteps, cfg.Rotate))),
				Snap:  rotate.Snap,
				Value: seconds(cfg.Rotate),
			},
			{
				Label: say.T("cameras.setting.url"),
				Kind:  menu.Field,
				Value: cfg.URL,
				Hint:  c.o.Site.URL(),
				Save:  func(v string) { c.change("url", v) },
			},
		}
		taps := []func(int){
			func(int) { c.change("tab", setting.OnOff(!cfg.Tab)) },
			func(int) { c.o.Shell.Push(c.o.Picker()) },
			func(level int) {
				c.change("scale", strconv.Itoa(scale.Index(level)+schema.CameraScaleLeast))
			},
			func(level int) { c.change("refresh", strconv.Itoa(refreshSteps[refresh.Index(level)])) },
			func(level int) { c.change("rotate", strconv.Itoa(rotateSteps[rotate.Index(level)])) },
			nil,
		}
		return rows, taps
	}}
}

func (c *Cameras) change(name, value string) {
	if err := c.knobs.Change(name, value); err != nil {
		slog.Error("saving a setting failed", "setting", "cameras."+name, "err", err)
	}
}

func nearest(steps []int, v int) int {
	best := steps[0]
	for _, s := range steps {
		if abs(s-v) < abs(best-v) {
			best = s
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func seconds(n int) string {
	if n == 0 {
		return say.T("cameras.off")
	}
	return say.F("cameras.seconds", map[string]any{"N": n})
}
