package photos

import (
	"github.com/ygelfand/libcountertop/pkg/runtime/product"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

func Build(p product.Context, settings storage.Section[schema.Photos], most func() int) *Library {
	l := New(Options{
		Settings: settings,
		Dir:      p.Path("photos"),
		DeviceID: p.Screen,
		Most:     most,
	})
	p.Settings.Add(l.Settings())
	return l
}
