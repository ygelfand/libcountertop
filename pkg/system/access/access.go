package access

import (
	"log/slog"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const Group setting.Group = "Access"

type Options struct {
	Settings storage.Section[schema.Access]
	DeviceID uint32
	ADB      func(on bool) error
}

type Access struct {
	o     Options
	knobs *setting.Controls[schema.Access]
}

func New(o Options) *Access {
	a := &Access{o: o}
	a.knobs = &setting.Controls[schema.Access]{
		Table:  NewTable(),
		Device: o.DeviceID,
		Read:   o.Settings.Read,
		Save:   a.store,
	}
	return a
}

func NewTable() *setting.Table[schema.Access] {
	return setting.NewTable("access", []setting.Group{Group}, []setting.Setting[schema.Access]{
		setting.Flag(
			setting.Setting[schema.Access]{
				Name:  "adb",
				Group: Group,
				ID:    "remote_adb",
				Icon:  "mdi:console-network",
			},
			func(c *schema.Access) *bool { return &c.ADB },
		),
	}, setting.Messages{Text: say.T, Missing: say.Missing})
}

func (a *Access) Name() string { return "access" }

func (a *Access) Controls() *setting.Controls[schema.Access] { return a.knobs }

func (a *Access) Entities() []esphome.Entity { return a.knobs.Entities() }

func (a *Access) Restore(c schema.Access) {
	a.knobs.PublishFrom(c)
	if err := a.o.ADB(c.ADB); err != nil {
		slog.Error("adb over the network", "on", c.ADB, "err", err)
	}
}

func (a *Access) ADB() bool { return a.o.Settings.Read().ADB }

func (a *Access) SetADB(on bool) {
	if err := a.knobs.Change("adb", setting.OnOff(on)); err != nil {
		slog.Error("saving a setting failed", "setting", "access.adb", "err", err)
	}
}

func (a *Access) store(s setting.Setting[schema.Access], v string) error {
	at := a.o.Settings.Read()
	if err := s.Write(&at, v); err != nil {
		return err
	}
	if err := a.o.ADB(at.ADB); err != nil {
		return err
	}
	return setting.Store(a.o.Settings, s, v)
}
