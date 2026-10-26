package clockview

import (
	"sync"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"
)

const turnTime = 900 * time.Millisecond

type turnKey struct {
	w  *gogui.Window
	id string
}

type turn struct {
	from, to string
	at       float32
}

var (
	turnsMu sync.Mutex
	turns   = map[turnKey]*turn{}
)

func turning(w *gogui.Window, id, to string) (from string, at float32) {
	key := turnKey{w, id}
	turnsMu.Lock()
	defer turnsMu.Unlock()
	t, ok := turns[key]
	if !ok {
		turns[key] = &turn{from: to, to: to, at: 1}
		return to, 1
	}
	if t.to != to {
		t.from, t.to, t.at = t.to, to, 0
		if w != nil {
			tw := gogui.NewTweenAnimation("clock-turn-"+id, 0, 1, func(v float32, _ *gogui.Window) {
				turnsMu.Lock()
				t.at = v
				turnsMu.Unlock()
			})
			tw.Duration, tw.Easing = turnTime, gogui.EaseInOutCubic
			w.QueueCommand(func(w *gogui.Window) { w.AnimationAdd(tw) })
		} else {
			t.at = 1
		}
	}
	return t.from, t.at
}
