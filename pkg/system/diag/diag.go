package diag

import (
	"context"
	"log/slog"
	"strings"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/inference/wake"
	netaddress "github.com/ygelfand/libcountertop/pkg/network/address"
	"github.com/ygelfand/libcountertop/pkg/runtime/collector"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
	"github.com/ygelfand/libcountertop/pkg/system/metrics"
)

const Group setting.Group = "Diagnostics"

type Zone struct {
	ID   string
	Name string
	Zone string
}

type Options struct {
	Settings storage.Section[schema.Diag]
	StateDir string
	Version  string
	Zones    []Zone
	Cache    *wake.CacheEntities
	Network  func() string
	Cores    int
	Hold     func(cores int) error
	Sample   func()
}

type Diag struct {
	o     Options
	knobs *setting.Controls[schema.Diag]

	zones       map[string]*esphome.Sensor
	cores       *esphome.Sensor
	coresOnline *esphome.Sensor
	usage       *esphome.Sensor
	load        *esphome.Sensor
	memory      *esphome.Sensor
	free        *esphome.Sensor
	uptime      *esphome.Sensor
	signal      *esphome.Sensor
	rxRate      *esphome.Sensor
	txRate      *esphome.Sensor
	address     *esphome.TextSensor
	network     *esphome.TextSensor
	version     *esphome.TextSensor

	wake chan struct{}

	last struct {
		at       time.Time
		rx, tx   float64
		recorded bool
	}
	cpu struct {
		busy, total float64
		recorded    bool
	}
}

func New(o Options) *Diag {
	d := &Diag{o: o, wake: make(chan struct{}, 1), zones: map[string]*esphome.Sensor{}}
	d.knobs = &setting.Controls[schema.Diag]{
		Table: NewTable(o),
		Read:  o.Settings.Read,
		Save:  d.store,
	}
	d.build()
	return d
}

func NewTable(o Options) *setting.Table[schema.Diag] {
	rows := []setting.Setting[schema.Diag]{
		setting.Count(
			setting.Setting[schema.Diag]{
				Name:  "interval",
				Group: Group,
				ID:    "metrics_interval",
				Icon:  "mdi:timer-sync",
				Min:   10,
				Max:   3600,
				Unit:  "s",
			},
			func(c *schema.Diag) *int { return &c.Interval },
		),
	}
	if o.Hold != nil {
		rows = append(rows, setting.Count(
			setting.Setting[schema.Diag]{
				Name:  "min_cores",
				Group: Group,
				ID:    "min_cores",
				Icon:  "mdi:cpu-64-bit",
				Min:   1,
				Max:   max(o.Cores, 1),
				Local: true,
			},
			func(c *schema.Diag) *int { return &c.MinCores },
		))
	}
	return setting.NewTable("diag", []setting.Group{Group}, rows,
		setting.Messages{Text: say.T, Missing: say.Missing})
}

func (d *Diag) Name() string { return "diagnostics" }

func (d *Diag) Controls() *setting.Controls[schema.Diag] { return d.knobs }

func (d *Diag) Entities() []esphome.Entity {
	out := d.knobs.Entities()
	if d.o.Cache != nil {
		out = append(out, d.o.Cache.Entities()...)
	}
	for _, z := range d.o.Zones {
		out = append(out, d.zones[z.Zone])
	}
	out = append(out,
		d.cores, d.coresOnline, d.usage, d.load, d.memory, d.free, d.uptime,
		d.signal, d.rxRate, d.txRate, d.address, d.version,
	)
	if d.o.Network != nil {
		out = append(out, d.network)
	}
	return out
}

func (d *Diag) Restore(c schema.Diag) {
	d.Measure()
	d.knobs.PublishFrom(c)
	if d.o.Hold == nil {
		return
	}
	if err := d.o.Hold(c.MinCores); err != nil {
		slog.Error("holding cores online failed", "cores", c.MinCores, "err", err)
	}
}

func (d *Diag) Measure() {
	if d.o.Cache != nil {
		d.o.Cache.Measure()
	}
	free, err := metrics.Free(d.o.StateDir)
	if err != nil {
		slog.Error("reading free space failed", "dir", d.o.StateDir, "err", err)
		return
	}
	d.free.Set(float32(free / 1024))
}

func (d *Diag) Soon() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *Diag) Run(ctx context.Context) error {
	return collector.Run(
		ctx,
		func() time.Duration { return time.Duration(d.o.Settings.Read().Interval) * time.Second },
		d.wake,
		d.Sample,
	)
}

func (d *Diag) Sample() {
	d.Measure()
	d.board()
	d.wireless()
	d.address.Set(strings.Join(addresses(), ", "))
	if d.o.Network != nil {
		d.network.Set(d.o.Network())
	}
	if d.o.Sample != nil {
		d.o.Sample()
	}
}

func (d *Diag) store(s setting.Setting[schema.Diag], v string) error {
	at := d.o.Settings.Read()
	if err := s.Write(&at, v); err != nil {
		return err
	}
	if s.Name == "min_cores" && d.o.Hold != nil {
		if err := d.o.Hold(at.MinCores); err != nil {
			return err
		}
	}
	err := setting.Store(d.o.Settings, s, v)
	d.Soon()
	return err
}

func (d *Diag) board() {
	r := metrics.Reader{}
	temps := r.Temperatures()
	for zone, s := range d.zones {
		if v, ok := temps[zone]; ok {
			s.Set(float32(v))
		}
	}
	present, online := r.Cores()
	set(d.cores, present)
	set(d.coresOnline, online)
	d.busy(r)
	one, _ := r.Load()
	set(d.load, one)
	available, _ := r.Memory()
	set(d.memory, available)
	set(d.uptime, r.Uptime())
}

func (d *Diag) busy(r metrics.Reader) {
	busy, total := r.CPU()
	if !busy.Known || !total.Known {
		return
	}
	was := d.cpu
	d.cpu.busy, d.cpu.total, d.cpu.recorded = busy.Value, total.Value, true
	if !was.recorded {
		return
	}
	elapsed := total.Value - was.total
	worked := busy.Value - was.busy
	if elapsed <= 0 || worked < 0 {
		return
	}
	d.usage.Set(float32(min(worked/elapsed, 1) * 100))
}

func (d *Diag) wireless() {
	signal, rx, tx := metrics.Reader{}.Wifi()
	set(d.signal, signal)
	now := time.Now()
	was := d.last
	d.last.at, d.last.rx, d.last.tx, d.last.recorded = now, rx.Value, tx.Value, true
	if !was.recorded {
		return
	}
	secs := now.Sub(was.at).Seconds()
	if secs <= 0 {
		return
	}
	if rx.Known {
		d.rxRate.Set(float32((rx.Value - was.rx) / secs / 1024))
	}
	if tx.Known {
		d.txRate.Set(float32((tx.Value - was.tx) / secs / 1024))
	}
}

func addresses() []string {
	ips := netaddress.Addresses()
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out
}

func set(s *esphome.Sensor, r metrics.Reading) {
	if r.Known {
		s.Set(float32(r.Value))
	}
}
