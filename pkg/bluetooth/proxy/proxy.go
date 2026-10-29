package proxy

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"
	"github.com/ygelfand/go-esphome-device/api"
	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/libcountertop/pkg/bluetooth/hci"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

const Features = esphome.BluetoothPassiveScan | esphome.BluetoothRawAdvertisements | esphome.BluetoothStateAndMode

const Connections = esphome.BluetoothActiveConnections | esphome.BluetoothRemoteCaching |
	esphome.BluetoothPairing | esphome.BluetoothCacheClearing

const queued = 512
const batch = 16
const hold = 250 * time.Millisecond

const Group setting.Group = "Bluetooth"

type Scanner interface {
	Up() bool
	Scan(context.Context, hci.Scanning, func(hci.Advertisement)) error
	Advertise([]byte) error
	StopAdvertising() error
}

type Dependencies struct {
	Radio     Scanner
	Settings  storage.Section[schema.Bluetooth]
	Reconnect func()
	Beacon    func() []byte
	Links     Connector
	Bonds     string
}

type Proxy struct {
	options        Dependencies
	proxy          *esphome.BluetoothProxy
	links          *connections
	knobs          *setting.Controls[schema.Bluetooth]
	wanted         chan struct{}
	reports        chan hci.Advertisement
	heard, dropped atomic.Uint64
	lifecycle      sync.Mutex
	stop           context.CancelFunc
	done           chan struct{}
}

func New(options Dependencies) *Proxy {
	b := &Proxy{
		options: options,
		proxy:   &esphome.BluetoothProxy{},
		wanted:  make(chan struct{}, 1),
		reports: make(chan hci.Advertisement, queued),
	}
	b.knobs = &setting.Controls[schema.Bluetooth]{
		Table: NewTable(),
		Read:  options.Settings.Read,
		Save:  b.store,
	}
	if options.Links != nil {
		b.links = newConnections(options.Links, options.Bonds)
	}
	b.proxy.OnSubscribed = func(subscribed bool) {
		if !subscribed && b.links != nil {
			go b.links.closeAll()
		}
		b.apply()
	}
	b.proxy.OnMode = func(bool) { b.apply() }
	return b
}

func NewTable() *setting.Table[schema.Bluetooth] {
	return setting.NewTable(
		"bluetooth",
		[]setting.Group{Group},
		[]setting.Setting[schema.Bluetooth]{
			setting.Flag(
				setting.Setting[schema.Bluetooth]{
					Name:  "proxy",
					Group: Group,
					Icon:  "mdi:bluetooth",
				},
				func(c *schema.Bluetooth) *bool { return &c.Proxy },
			),
			setting.Pick(
				setting.Setting[schema.Bluetooth]{
					Name:  "scan",
					Group: Group,
					Icon:  "mdi:radar",
				},
				schema.ScanShares(),
				func(c *schema.Bluetooth) *schema.ScanShare { return &c.Scan },
			),
		},
		setting.Messages{Text: say.T, Missing: say.Missing},
	)
}

func (b *Proxy) Name() string                                  { return "bluetooth proxy" }
func (b *Proxy) Entities() []esphome.Entity                    { return b.knobs.Entities() }
func (b *Proxy) Controls() *setting.Controls[schema.Bluetooth] { return b.knobs }
func (b *Proxy) Handle(ctx context.Context, conn *esphome.Conn, msg proto.Message) error {
	if b.links != nil && b.Enabled() {
		if handled, err := b.links.handle(conn, msg); handled {
			return err
		}
	}
	return b.proxy.Handle(ctx, conn, msg)
}
func (b *Proxy) Enabled() bool { return b.options.Settings.Read().Proxy }
func (b *Proxy) scanning() hci.Scanning {
	return hci.Scanning{Active: b.proxy.Active(), Percent: b.options.Settings.Read().Scan.Percent()}
}
func (b *Proxy) Restore() { b.knobs.Publish(); b.apply() }
func (b *Proxy) SetProxy(on bool) {
	if err := b.knobs.Change("proxy", setting.OnOff(on)); err != nil {
		slog.Error("saving bluetooth proxy setting failed", "err", err)
		b.Restore()
	}
}
func (b *Proxy) store(s setting.Setting[schema.Bluetooth], v string) error {
	err := setting.Store(b.options.Settings, s, v)
	b.apply()
	if err != nil {
		return err
	}
	if b.options.Reconnect != nil {
		b.options.Reconnect()
	}
	return nil
}
func (b *Proxy) Reports() uint64 { return b.heard.Load() }
func (b *Proxy) Dropped() uint64 { return b.dropped.Load() }
func (b *Proxy) Features() esphome.BluetoothFeature {
	switch {
	case !b.Enabled():
		return 0
	case b.links != nil:
		return Features | Connections
	}
	return Features
}

func (b *Proxy) Start(parent context.Context) error {
	b.lifecycle.Lock()
	defer b.lifecycle.Unlock()
	if b.stop != nil {
		return errors.New("bluetooth proxy already running")
	}
	if b.options.Radio == nil {
		return errors.New("bluetooth proxy has no radio")
	}
	ctx, stop := context.WithCancel(parent)
	for len(b.reports) > 0 {
		<-b.reports
	}
	b.stop, b.done = stop, make(chan struct{})
	done := b.done
	safe.Go("bluetooth proxy", func() { defer close(done); b.run(ctx) })
	b.Restore()
	return nil
}
func (b *Proxy) Run(ctx context.Context) error {
	b.lifecycle.Lock()
	done := b.done
	b.lifecycle.Unlock()
	if done == nil {
		return errors.New("bluetooth proxy not started")
	}
	select {
	case <-ctx.Done():
		return nil
	case <-done:
		if ctx.Err() != nil {
			return nil
		}
		return errors.New("bluetooth proxy worker stopped")
	}
}
func (b *Proxy) Close() error {
	b.lifecycle.Lock()
	defer b.lifecycle.Unlock()
	if b.stop != nil {
		b.stop()
		<-b.done
		b.stop, b.done = nil, nil
	}
	return nil
}
func (b *Proxy) apply() {
	select {
	case b.wanted <- struct{}{}:
	default:
	}
}

type ended struct {
	generation uint64
	err        error
}

func (b *Proxy) run(ctx context.Context) {
	ctx, stop := context.WithCancel(ctx)
	var delivery sync.WaitGroup
	delivery.Go(func() { b.deliver(ctx) })
	defer func() { stop(); delivery.Wait() }()
	var cancel context.CancelFunc
	var scanDone chan struct{}
	var running hci.Scanning
	var generation uint64
	finished := make(chan ended, 1)
	retry := time.NewTimer(time.Hour)
	retry.Stop()
	defer retry.Stop()
	delay := time.Second
	halt := func() {
		generation++
		if cancel != nil {
			cancel()
			<-scanDone
			cancel = nil
			scanDone = nil
		}
		if b.options.Radio.Up() {
			_ = b.options.Radio.StopAdvertising()
		}
	}
	defer halt()
	tune := func() bool {
		want, mode := b.Enabled(), b.scanning()
		if cancel != nil && want && running == mode {
			return true
		}
		halt()
		if !want {
			if b.links != nil {
				b.links.closeAll()
			}
			_ = b.proxy.Report(esphome.ScannerStopped)
			return true
		}
		if !b.options.Radio.Up() {
			_ = b.proxy.Report(esphome.ScannerFailed)
			return false
		}
		scanCtx, stop := context.WithCancel(ctx)
		cancel, scanDone, running = stop, make(chan struct{}), mode
		done, g := scanDone, generation
		safe.Go("bluetooth scanning", func() {
			defer close(done)
			err := b.options.Radio.Scan(scanCtx, mode, b.found)
			if scanCtx.Err() == nil {
				select {
				case finished <- ended{generation: g, err: err}:
				case <-scanCtx.Done():
				}
			}
		})
		if b.options.Beacon != nil {
			if beacon := b.options.Beacon(); len(beacon) > 0 {
				if err := b.options.Radio.Advertise(beacon); err != nil {
					slog.Warn("BLE beacon advertising failed", "err", err)
				}
			}
		}
		_ = b.proxy.Report(esphome.ScannerRunning)
		return true
	}
	apply := func() {
		if tune() {
			retry.Stop()
			delay = time.Second
		} else {
			retry.Reset(delay)
			delay = min(delay*2, 30*time.Second)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.wanted:
			delay = time.Second
			apply()
		case <-retry.C:
			apply()
		case end := <-finished:
			if end.generation == generation {
				slog.Warn("bluetooth scanner stopped", "err", end.err)
				halt()
				_ = b.proxy.Report(esphome.ScannerFailed)
				retry.Reset(delay)
			}
		}
	}
}

func (b *Proxy) found(a hci.Advertisement) {
	b.heard.Add(1)
	if b.proxy.Subscribed() {
		b.queue(a)
	}
}
func (b *Proxy) queue(a hci.Advertisement) {
	a.Data = slices.Clone(a.Data)
	select {
	case b.reports <- a:
	default:
		b.dropped.Add(1)
	}
}
func (b *Proxy) deliver(ctx context.Context) {
	var pending []*api.BluetoothLERawAdvertisement
	flush := time.NewTimer(hold)
	flush.Stop()
	defer flush.Stop()
	send := func() {
		if len(pending) == 0 {
			return
		}
		ads := pending
		pending = nil
		if err := b.proxy.Advertise(ads); err != nil {
			slog.Debug("BLE advertisements not delivered", "count", len(ads), "err", err)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case a := <-b.reports:
			pending = append(
				pending,
				&api.BluetoothLERawAdvertisement{
					Address:     a.Addr(),
					Rssi:        int32(a.RSSI),
					AddressType: uint32(a.AddressType),
					Data:        a.Data,
				},
			)
			if len(pending) == 1 {
				flush.Reset(hold)
			}
			if len(pending) >= batch {
				flush.Stop()
				send()
			}
		case <-flush.C:
			send()
		}
	}
}
