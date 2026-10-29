package nws

import (
	"github.com/ygelfand/libcountertop/pkg/runtime/product"
	"github.com/ygelfand/libcountertop/pkg/runtime/remember"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

func Build(
	p product.Context,
	settings storage.Section[schema.Weather],
	dismissed remember.Slot[[]string],
) *NWS {
	return New(Options{
		Controller: p.Controller,
		Agent:      p.Agent(),
		Settings:   settings,
		Dismissed:  dismissed,
		Redraw:     p.Redraw,
	})
}
