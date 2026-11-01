package screens

import (
	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/lovelaceview"
	"github.com/ygelfand/libcountertop/pkg/tessera/firmwareview"
)

func standby(v *firmwareview.View) func() bool {
	if v == nil {
		return nil
	}
	return v.Rests
}

func lovelaceRows(f *lovelaceview.Feature) func(saver bool) ([]menu.Row, []func(int)) {
	if f == nil {
		return nil
	}
	return f.Rows
}

func lovelaceReload(f *lovelaceview.Feature) func() {
	if f == nil {
		return nil
	}
	return f.Refresh
}
