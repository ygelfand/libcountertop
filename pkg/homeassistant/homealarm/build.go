package homealarm

import (
	"github.com/ygelfand/libcountertop/pkg/runtime/product"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

func Build(
	p product.Context,
	settings storage.Section[schema.HomeAlarm],
	chimes func() []setting.Option,
) *Alarm {
	a := New(Options{
		Controller: p.Controller,
		States:     p.States,
		Settings:   settings,
		DeviceID:   p.Screen,
		Chimes:     chimes,
	})
	p.Settings.Add(a.Settings())
	return a
}
