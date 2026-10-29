package todo

import (
	"github.com/ygelfand/libcountertop/pkg/runtime/product"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

func Build(p product.Context, settings storage.Section[schema.Todo]) *Todo {
	t := New(Options{
		Controller: p.Controller,
		States:     p.States,
		Settings:   settings,
		Redraw:     p.Redraw,
		DeviceID:   p.Screen,
	})
	p.Settings.Add(t.Settings())
	return t
}
