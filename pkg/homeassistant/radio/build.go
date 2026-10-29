package radio

import (
	"github.com/ygelfand/libcountertop/pkg/runtime/product"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

func Build(p product.Context, settings storage.Section[schema.Radio], mac func() string) *Radio {
	return New(Options{
		Controller: p.Controller,
		MAC:        mac,
		Settings:   settings,
		Redraw:     p.Redraw,
	})
}
