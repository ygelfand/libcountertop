package screens

import "github.com/ygelfand/libcountertop/pkg/tessera/firmwareview"

func standby(v *firmwareview.View) func() bool {
	if v == nil {
		return nil
	}
	return v.Rests
}
