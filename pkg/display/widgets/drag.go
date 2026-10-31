package widgets

import (
	"sync"

	gogui "github.com/go-gui-org/go-gui/gui"
)

type Drag struct {
	mu  sync.Mutex
	end func(w *gogui.Window, velocity float32)
}

func (d *Drag) Begin(w *gogui.Window, end func(w *gogui.Window, velocity float32)) {
	d.Track(w, nil, end)
}

func (d *Drag) Track(
	w *gogui.Window,
	move func(gogui.EventCtx),
	end func(w *gogui.Window, velocity float32),
) {
	d.mu.Lock()
	d.end = end
	d.mu.Unlock()
	w.MouseLock(gogui.MouseLockCfg{
		MouseMove: move,
		MouseUp: func(e gogui.EventCtx) {
			e.Window.MouseUnlock()
			d.End(e.Window, 0)
		},
		Cancel: func(w *gogui.Window) { d.End(w, 0) },
	})
}

func (d *Drag) End(w *gogui.Window, velocity float32) {
	d.mu.Lock()
	end := d.end
	d.end = nil
	d.mu.Unlock()
	if end != nil {
		end(w, velocity)
	}
}

func (d *Drag) Active() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.end != nil
}
