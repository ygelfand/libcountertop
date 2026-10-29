package radar

import (
	"github.com/ygelfand/libcountertop/pkg/runtime/product"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

func Build(p product.Context, settings storage.Section[schema.Weather]) *Radar {
	return New(Options{
		Home:     p.Controller.Home,
		Agent:    p.Agent(),
		Settings: settings,
		Redraw:   p.Redraw,
		CacheDir: p.Path("tiles"),
	})
}
