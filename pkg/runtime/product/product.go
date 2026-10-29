package product

import (
	"path/filepath"

	"github.com/ygelfand/libcountertop/pkg/homeassistant"
	"github.com/ygelfand/libcountertop/pkg/homeassistant/states"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
)

type Context struct {
	Name       string
	Version    string
	Site       string
	StateDir   string
	Controller *homeassistant.Controller
	States     *states.States
	Settings   *setting.Registry
	Redraw     func()
	Screen     uint32
	Camera     uint32
}

func (c Context) Agent() string { return "(" + c.Name + " " + c.Version + ", " + c.Site + ")" }

func (c Context) Path(name string) string { return filepath.Join(c.StateDir, name) }
