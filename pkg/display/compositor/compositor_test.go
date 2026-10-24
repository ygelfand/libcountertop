package compositor

import (
	"context"
	"image"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/display/compositor/internal/wire"
	wlc "github.com/ygelfand/libcountertop/pkg/display/compositor/internal/wlclient"
	"github.com/ygelfand/libcountertop/pkg/display/compositor/internal/xdgclient"
)

type globals map[string]uint32

func (g globals) Global(name uint32, iface string, _ uint32) { g[iface] = name }
func (g globals) GlobalRemove(uint32)                        {}

type xdgEvents struct{ configured chan uint32 }

func (x xdgEvents) Configure(serial uint32) { x.configured <- serial }

type toplevelEvents struct{}

func (toplevelEvents) Configure(_, _ int32, _ []byte) {}
func (toplevelEvents) Close()                         {}
func (toplevelEvents) ConfigureBounds(_, _ int32)     {}
func (toplevelEvents) WmCapabilities([]byte)          {}

type touchEvents struct{ down chan [2]float64 }

func (t touchEvents) Down(_, _ uint32, _ *wlc.Surface, _ int32, x, y wire.Fixed) {
	t.down <- [2]float64{x.Float(), y.Float()}
}
func (touchEvents) Up(_, _ uint32, _ int32)                   {}
func (touchEvents) Motion(_ uint32, _ int32, _, _ wire.Fixed) {}
func (touchEvents) Frame()                                    {}
func (touchEvents) Cancel()                                   {}
func (touchEvents) Shape(_ int32, _, _ wire.Fixed)            {}
func (touchEvents) Orientation(_ int32, _ wire.Fixed)         {}

func TestAWindowShowsItsFramesTitleAndTakesTouch(t *testing.T) {
	const width, height = 64, 40
	dir, err := os.MkdirTemp("", "wl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "run", "wl0")
	c := New(Options{Path: path, Width: width, Height: height})
	opened := make(chan *Window, 1)
	c.Opened.Listen(func(w *Window) { opened <- w })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ran := make(chan error, 1)
	go func() { ran <- c.Run(ctx) }()

	var conn *net.UnixConn
	for range 50 {
		var err error
		if conn, err = net.DialUnix(
			"unix",
			nil,
			&net.UnixAddr{Name: path, Net: "unix"},
		); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if conn == nil {
		select {
		case err := <-ran:
			t.Fatalf("the compositor stopped: %v", err)
		default:
			t.Fatal("the compositor never listened")
		}
	}
	cl := wlc.NewClient(wire.NewConn(conn))
	defer cl.Close()

	g := globals{}
	reg := cl.Display().GetRegistry()
	reg.Listener = g
	roundTrip(t, cl)
	comp := wlc.BindCompositor(cl, reg, g[wlc.CompositorInterface], 5)
	shm := wlc.BindShm(cl, reg, g[wlc.ShmInterface], 1)
	seat := wlc.BindSeat(cl, reg, g[wlc.SeatInterface], 7)
	wm := xdgclient.BindWmBase(cl, reg, g[xdgclient.WmBaseInterface], 5)

	touches := touchEvents{down: make(chan [2]float64, 1)}
	seat.GetTouch().Listener = touches

	surf := comp.CreateSurface()
	xs := wm.GetXdgSurface(surf)
	configured := make(chan uint32, 4)
	xs.Listener = xdgEvents{configured: configured}
	top := xs.GetToplevel()
	top.Listener = toplevelEvents{}
	top.SetTitle("dashboard")
	surf.Commit()
	roundTrip(t, cl)
	xs.AckConfigure(<-configured)

	w := <-opened
	if got := w.Title(); got != "dashboard" {
		t.Errorf("the window is titled %q, want %q", got, "dashboard")
	}

	frames := make(chan Frame, 1)
	w.Frames.Listen(func(f Frame) {
		f.Pixels = append([]byte(nil), f.Pixels...)
		frames <- f
	})

	stride := width * 4
	f, err := os.CreateTemp(t.TempDir(), "pool")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(int64(stride * height)); err != nil {
		t.Fatal(err)
	}
	pix, err := syscall.Mmap(
		int(f.Fd()),
		0,
		stride*height,
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_SHARED,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Munmap(pix)
	copy(pix, []byte{0, 0, 0xff, 0xff})
	pool := shm.CreatePool(f, int32(stride*height))
	buf := pool.CreateBuffer(0, width, height, int32(stride), wlc.ShmFormatArgb8888)
	surf.Attach(buf, 0, 0)
	surf.Damage(0, 0, 8, 8)
	surf.Commit()
	roundTrip(t, cl)

	select {
	case got := <-frames:
		if got.Width != width || got.Height != height || got.Stride != stride {
			t.Errorf("the frame is %dx%d stride %d, want %dx%d stride %d",
				got.Width, got.Height, got.Stride, width, height, stride)
		}
		if got.Pixels[2] != 0xff || got.Pixels[0] != 0 {
			t.Errorf("the first pixel is %v, want red", got.Pixels[:4])
		}
		if len(got.Damage) != 1 || got.Damage[0] != image.Rect(0, 0, 8, 8) {
			t.Errorf("the damage is %v, want one 8x8 square", got.Damage)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no frame reached the window")
	}

	w.TouchDown(0, 12.5, 30)
	roundTrip(t, cl)
	select {
	case got := <-touches.down:
		if got != [2]float64{12.5, 30} {
			t.Errorf("the client was touched at %v, want [12.5 30]", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the touch never reached the client")
	}

	w.Suspend()
	drawn := make(chan uint32, 1)
	surf.Frame().Listener = doneEvents{drawn}
	surf.Commit()
	roundTrip(t, cl)
	select {
	case <-drawn:
		t.Fatal("a suspended window answered a frame callback")
	default:
	}
	w.Resume()
	roundTrip(t, cl)
	select {
	case <-drawn:
	default:
		t.Fatal("resuming the window left its frame callback unanswered")
	}
}

type doneEvents struct{ done chan uint32 }

func (d doneEvents) Done(at uint32) { d.done <- at }

func roundTrip(t *testing.T, cl *wlc.Client) {
	t.Helper()
	if err := cl.RoundTrip(); err != nil {
		t.Fatal(err)
	}
}
