package calendar

import (
	"github.com/ygelfand/libcountertop/pkg/runtime/product"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

func Build(p product.Context, settings storage.Section[schema.Calendar]) *Calendar {
	c := New(Options{
		Controller: p.Controller,
		States:     p.States,
		Settings:   settings,
		Redraw:     p.Redraw,
		DeviceID:   p.Screen,
	})
	p.Settings.Add(c.Settings())
	return c
}
