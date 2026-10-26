package homealarmview

import "github.com/ygelfand/libcountertop/pkg/display/shell"

func (v *View) backdrop(owner shell.View, width, height int) bool {
	if v.o.Backdrop == nil {
		return false
	}
	v.mu.Lock()
	if v.backdrops == nil {
		v.backdrops = map[shell.View]Backdrop{}
	}
	b, ok := v.backdrops[owner]
	if !ok {
		b = v.o.Backdrop(owner)
		v.backdrops[owner] = b
	}
	v.mu.Unlock()
	b.Keep(width, height)
	return true
}

func (v *View) dropBackdrops() {
	v.mu.Lock()
	open := v.backdrops
	v.backdrops = nil
	v.mu.Unlock()
	for _, b := range open {
		b.Close()
	}
}
