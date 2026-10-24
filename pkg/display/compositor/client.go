package compositor

import (
	"image"
	"log/slog"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/ygelfand/libcountertop/pkg/display/compositor/internal/wayland"
	"github.com/ygelfand/libcountertop/pkg/display/compositor/internal/wire"
	"github.com/ygelfand/libcountertop/pkg/display/compositor/internal/xdg"
)

const (
	globalCompositor = iota + 1
	globalSubcompositor
	globalShm
	globalWmBase
	globalOutput
	globalSeat
)

type client struct {
	c      *Compositor
	conn   *wayland.Client
	serial atomic.Uint32

	surfaces map[*wayland.Surface]*surface
	buffers  map[*wayland.Buffer]*buffer
	pools    map[*pool]struct{}
	windows  []*Window

	mu    sync.Mutex
	touch *wayland.Touch
}

type surface struct {
	s       *wayland.Surface
	pending *wayland.Buffer
	damage  []image.Rectangle
	frames  []*wayland.Callback
	window  *Window
}

type pool struct {
	f    *os.File
	data []byte
	refs int
}

type buffer struct {
	pool                          *pool
	offset, width, height, stride int
}

func newClient(c *Compositor, conn *wayland.Client) *client {
	cl := &client{
		c:        c,
		conn:     conn,
		surfaces: map[*wayland.Surface]*surface{},
		buffers:  map[*wayland.Buffer]*buffer{},
		pools:    map[*pool]struct{}{},
	}
	conn.Display().Listener = (*display)(cl)
	return cl
}

func (cl *client) close() {
	for _, w := range cl.windows {
		cl.c.closed(w)
	}
	cl.windows = nil
	for p := range cl.pools {
		p.release()
	}
	clear(cl.pools)
}

func (cl *client) unref(p *pool) {
	p.refs--
	if p.refs > 0 {
		return
	}
	p.release()
	delete(cl.pools, p)
}

func (cl *client) next() uint32 { return cl.serial.Add(1) }

func (cl *client) gone(id uint32) {
	cl.conn.Delete(id)
	cl.conn.Display().DeleteId(id)
}

func (cl *client) touchFor() *wayland.Touch {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return cl.touch
}

type display client

func (d *display) Sync(cb *wayland.Callback) {
	cb.Done((*client)(d).next())
	(*client)(d).gone(cb.ID())
}

func (d *display) GetRegistry(r *wayland.Registry) {
	r.Listener = (*registry)(d)
	r.Global(globalCompositor, wayland.CompositorInterface, 5)
	r.Global(globalSubcompositor, wayland.SubcompositorInterface, 1)
	r.Global(globalShm, wayland.ShmInterface, 1)
	r.Global(globalWmBase, xdg.WmBaseInterface, 5)
	r.Global(globalOutput, wayland.OutputInterface, 4)
	r.Global(globalSeat, wayland.SeatInterface, 7)
}

type registry client

func (r *registry) Bind(name uint32, id wire.NewID) {
	cl := (*client)(r)
	switch name {
	case globalCompositor:
		wayland.BindCompositor(cl.conn, id).Listener = (*compositor)(cl)
	case globalSubcompositor:
		s := wayland.BindSubcompositor(cl.conn, id)
		s.Listener = &subcompositor{cl: cl, s: s}
	case globalShm:
		s := wayland.BindShm(cl.conn, id)
		s.Listener = (*shm)(cl)
		s.Format(wayland.ShmFormatArgb8888)
		s.Format(wayland.ShmFormatXrgb8888)
	case globalWmBase:
		xdg.BindWmBase(cl.conn, id).Listener = (*wmBase)(cl)
	case globalOutput:
		o := wayland.BindOutput(cl.conn, id)
		o.Listener = output{}
		o.Geometry(0, 0, 0, 0, wayland.OutputSubpixelUnknown, "", "", wayland.OutputTransformNormal)
		o.Mode(
			wayland.OutputModeCurrent|wayland.OutputModePreferred,
			int32(cl.c.o.Width),
			int32(cl.c.o.Height),
			60000,
		)
		o.Scale(1)
		o.Done()
	case globalSeat:
		s := wayland.BindSeat(cl.conn, id)
		s.Listener = (*seat)(cl)
		s.Capabilities(wayland.SeatCapabilityTouch)
	}
}

type compositor client

func (c *compositor) CreateSurface(s *wayland.Surface) {
	cl := (*client)(c)
	surf := &surface{s: s}
	cl.surfaces[s] = surf
	s.Listener = &surfaceRequests{cl: cl, s: surf}
}

func (c *compositor) CreateRegion(r *wayland.Region) {
	r.Listener = &region{cl: (*client)(c), r: r}
}

type region struct {
	cl *client
	r  *wayland.Region
}

func (r *region) Destroy()                  { r.cl.gone(r.r.ID()) }
func (r *region) Add(_, _, _, _ int32)      {}
func (r *region) Subtract(_, _, _, _ int32) {}

type surfaceRequests struct {
	cl *client
	s  *surface
}

func (r *surfaceRequests) Destroy() {
	delete(r.cl.surfaces, r.s.s)
	r.cl.gone(r.s.s.ID())
}

func (r *surfaceRequests) Attach(b *wayland.Buffer, _, _ int32) { r.s.pending = b }

func (r *surfaceRequests) Damage(x, y, width, height int32) {
	r.s.damage = append(r.s.damage, image.Rect(int(x), int(y), int(x+width), int(y+height)))
}

func (r *surfaceRequests) DamageBuffer(x, y, width, height int32) { r.Damage(x, y, width, height) }

func (r *surfaceRequests) Frame(cb *wayland.Callback) { r.s.frames = append(r.s.frames, cb) }

func (r *surfaceRequests) SetOpaqueRegion(*wayland.Region)            {}
func (r *surfaceRequests) SetInputRegion(*wayland.Region)             {}
func (r *surfaceRequests) SetBufferTransform(wayland.OutputTransform) {}
func (r *surfaceRequests) SetBufferScale(int32)                       {}
func (r *surfaceRequests) Offset(_, _ int32)                          {}

func (r *surfaceRequests) Commit() {
	if b := r.s.pending; b != nil {
		r.cl.present(r.s, b)
		b.Release()
		r.s.pending = nil
	}
	r.s.damage = r.s.damage[:0]
	frames := slices.Clone(r.s.frames)
	r.s.frames = r.s.frames[:0]
	if w := r.s.window; w != nil && w.hold(frames) {
		return
	}
	r.cl.answer(frames)
}

func (cl *client) answer(frames []*wayland.Callback) {
	now := stamp()
	for _, cb := range frames {
		cb.Done(now)
		cl.gone(cb.ID())
	}
}

func (cl *client) present(s *surface, b *wayland.Buffer) {
	buf, ok := cl.buffers[b]
	if !ok || s.window == nil {
		return
	}
	end := buf.offset + buf.stride*buf.height
	if end > len(buf.pool.data) {
		return
	}
	damage := s.damage
	full := image.Rect(0, 0, buf.width, buf.height)
	if len(damage) == 0 {
		damage = []image.Rectangle{full}
	}
	for i := range damage {
		damage[i] = damage[i].Intersect(full)
	}
	s.window.Frames.Emit(Frame{
		Pixels: buf.pool.data[buf.offset:end],
		Stride: buf.stride,
		Width:  buf.width,
		Height: buf.height,
		Damage: damage,
	})
}

type subcompositor struct {
	cl *client
	s  *wayland.Subcompositor
}

func (s *subcompositor) Destroy() { s.cl.gone(s.s.ID()) }

func (s *subcompositor) GetSubsurface(sub *wayland.Subsurface, _, _ *wayland.Surface) {
	sub.Listener = &subsurface{cl: s.cl, s: sub}
}

type subsurface struct {
	cl *client
	s  *wayland.Subsurface
}

func (s *subsurface) Destroy()                    { s.cl.gone(s.s.ID()) }
func (s *subsurface) SetPosition(_, _ int32)      {}
func (s *subsurface) PlaceAbove(*wayland.Surface) {}
func (s *subsurface) PlaceBelow(*wayland.Surface) {}
func (s *subsurface) SetSync()                    {}
func (s *subsurface) SetDesync()                  {}

type shm client

func (s *shm) Release() {}

func (s *shm) CreatePool(p *wayland.ShmPool, f *os.File, size int32) {
	cl := (*client)(s)
	pl := &pool{f: f, refs: 1}
	pl.remap(size)
	cl.pools[pl] = struct{}{}
	p.Listener = &poolRequests{cl: cl, p: p, pool: pl}
}

func (p *pool) remap(size int32) {
	if p.data != nil {
		_ = syscall.Munmap(p.data)
		p.data = nil
	}
	data, err := syscall.Mmap(int(p.f.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		slog.Warn("a compositor client's buffer pool could not be mapped", "err", err)
		return
	}
	p.data = data
}

func (p *pool) release() {
	if p.data != nil {
		_ = syscall.Munmap(p.data)
		p.data = nil
	}
	_ = p.f.Close()
}

type poolRequests struct {
	cl   *client
	p    *wayland.ShmPool
	pool *pool
}

func (r *poolRequests) CreateBuffer(
	b *wayland.Buffer,
	offset, width, height, stride int32,
	_ wayland.ShmFormat,
) {
	r.pool.refs++
	r.cl.buffers[b] = &buffer{
		pool:   r.pool,
		offset: int(offset),
		width:  int(width),
		height: int(height),
		stride: int(stride),
	}
	b.Listener = &bufferRequests{cl: r.cl, b: b}
}

func (r *poolRequests) Destroy() {
	r.cl.unref(r.pool)
	r.cl.gone(r.p.ID())
}

func (r *poolRequests) Resize(size int32) { r.pool.remap(size) }

type bufferRequests struct {
	cl *client
	b  *wayland.Buffer
}

func (r *bufferRequests) Destroy() {
	if buf, ok := r.cl.buffers[r.b]; ok {
		r.cl.unref(buf.pool)
		delete(r.cl.buffers, r.b)
	}
	r.cl.gone(r.b.ID())
}

type wmBase client

func (w *wmBase) Destroy()                         {}
func (w *wmBase) CreatePositioner(*xdg.Positioner) {}
func (w *wmBase) Pong(uint32)                      {}

func (w *wmBase) GetXdgSurface(xs *xdg.Surface, s *wayland.Surface) {
	cl := (*client)(w)
	xs.Listener = &xdgSurface{cl: cl, xs: xs, s: cl.surfaces[s]}
}

type xdgSurface struct {
	cl *client
	xs *xdg.Surface
	s  *surface
}

func (x *xdgSurface) Destroy() { x.cl.gone(x.xs.ID()) }

func (x *xdgSurface) GetToplevel(t *xdg.Toplevel) {
	if x.s == nil {
		return
	}
	w := &Window{
		cl:      x.cl,
		surface: x.s,
		top:     t,
		xs:      x.xs,
		width:   x.cl.c.o.Width,
		height:  x.cl.c.o.Height,
	}
	x.s.window = w
	x.cl.windows = append(x.cl.windows, w)
	t.Listener = &toplevel{cl: x.cl, w: w}
	w.configure()
	x.cl.c.opened(w)
}

func (x *xdgSurface) GetPopup(*xdg.Popup, *xdg.Surface, *xdg.Positioner) {}
func (x *xdgSurface) SetWindowGeometry(_, _, _, _ int32)                 {}
func (x *xdgSurface) AckConfigure(uint32)                                {}

type toplevel struct {
	cl *client
	w  *Window
}

func (t *toplevel) Destroy() {
	t.w.surface.window = nil
	for i, w := range t.cl.windows {
		if w == t.w {
			t.cl.windows = append(t.cl.windows[:i], t.cl.windows[i+1:]...)
			break
		}
	}
	t.cl.c.closed(t.w)
	t.cl.gone(t.w.top.ID())
}

func (t *toplevel) SetTitle(title string)                                { t.w.retitle(title) }
func (t *toplevel) SetParent(*xdg.Toplevel)                              {}
func (t *toplevel) SetAppId(string)                                      {}
func (t *toplevel) ShowWindowMenu(*wayland.Seat, uint32, int32, int32)   {}
func (t *toplevel) Move(*wayland.Seat, uint32)                           {}
func (t *toplevel) Resize(*wayland.Seat, uint32, xdg.ToplevelResizeEdge) {}
func (t *toplevel) SetMaxSize(_, _ int32)                                {}
func (t *toplevel) SetMinSize(_, _ int32)                                {}
func (t *toplevel) SetMaximized()                                        {}
func (t *toplevel) UnsetMaximized()                                      {}
func (t *toplevel) SetFullscreen(*wayland.Output)                        {}
func (t *toplevel) UnsetFullscreen()                                     {}
func (t *toplevel) SetMinimized()                                        {}

type output struct{}

func (output) Release() {}

type seat client

func (s *seat) GetPointer(*wayland.Pointer)   {}
func (s *seat) GetKeyboard(*wayland.Keyboard) {}
func (s *seat) Release()                      {}

func (s *seat) GetTouch(t *wayland.Touch) {
	cl := (*client)(s)
	cl.mu.Lock()
	cl.touch = t
	cl.mu.Unlock()
	t.Listener = touch{}
}

type touch struct{}

func (touch) Release() {}
