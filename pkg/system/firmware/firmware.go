package firmware

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"slices"
	"sync"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/hook"
	"github.com/ygelfand/libcountertop/pkg/network/releases"
	"github.com/ygelfand/libcountertop/pkg/runtime/component"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const (
	EventInstalled  = "installed"
	EventRolledBack = "rolled_back"
	EventFailed     = "failed"
)

type Release struct {
	Version    string
	Notes      string
	ReleaseURL string
}

type Source[M any] interface {
	Fetch(ctx context.Context, channel string) (M, error)
	Describe(found M) Release
	Install(ctx context.Context, found M, progress func(float32)) error
	Restart(why string)
}

type Trial interface {
	RolledBack() string
	Commit()
}

const unpicked = "Select for rollback"

type Catalog struct {
	Finding Finding
	Builds  []releases.Release
}

type Builds[M any] interface {
	Builds(ctx context.Context) ([]releases.Release, error)
	FetchBuild(ctx context.Context, version string) (M, error)
}

type Settings interface {
	Read() schema.Update
	Channel(label string) error
	LastVersion(version string) error
}

type Activity interface {
	Refresh()
	Done()
}

type Indicator interface{ Begin() Activity }

type Dependencies[M any] struct {
	Source    Source[M]
	Settings  Settings
	Product   string
	Version   string
	Channels  []string
	Busy      Indicator
	OnFailure func()
	Reconnect func()
	ListPath  string
}

type Upgrade struct {
	Version string
	At      float32
}

func (up Upgrade) Active() bool { return up.Version != "" }

type Finding int

const (
	Unchecked Finding = iota
	Checking
	Current
	Available
	Unreachable
)

type Offer struct {
	Finding Finding
	Release Release
}

type Firmware[M any] struct {
	Upgrading hook.Hook[Upgrade]
	Offered   hook.Hook[Offer]
	Listed    hook.Hook[Catalog]

	deps Dependencies[M]

	entity   *esphome.Update
	channel  *esphome.Select
	look     *esphome.Button
	status   *esphome.TextSensor
	events   *esphome.Event
	versions *esphome.Select
	apply    *esphome.Button

	mu      sync.Mutex
	found   M
	offer   Offer
	catalog Catalog
	upgrade Upgrade

	announced     sync.Once
	rolledBack    string
	rebootPending bool
}

func New[M any](deps Dependencies[M]) *Firmware[M] {
	u := &Firmware[M]{deps: deps}

	u.entity = &esphome.Update{
		Base: esphome.Base{
			ObjectID: "firmware",
			Name:     "Firmware",
			Icon:     "mdi:package-up",
		},
		DeviceClass: "firmware",
		OnCommand:   u.command,
	}
	u.publish(Release{Version: deps.Version})

	u.channel = &esphome.Select{
		Base: esphome.Base{
			ObjectID: "update_channel",
			Name:     "Update channel",
			Icon:     "mdi:source-branch",
			Category: esphome.CategoryDiagnostic,
		},
		Options:   slices.Clone(deps.Channels),
		OnCommand: u.Choose,
	}
	u.channel.Set(u.Channel())

	// Home Assistant asks for an update check only through homeassistant.update_entity.
	u.look = &esphome.Button{
		Base: esphome.Base{
			ObjectID: "check_for_updates",
			Name:     "Check for updates",
			Icon:     "mdi:cloud-search",
			Category: esphome.CategoryDiagnostic,
		},
		OnPress: u.CheckLater,
	}

	u.status = &esphome.TextSensor{
		Base: esphome.Base{
			ObjectID: "update_status",
			Name:     "Update status",
			Icon:     "mdi:package-variant",
			Category: esphome.CategoryDiagnostic,
		},
	}

	types := []string{EventInstalled, EventFailed}
	if t, ok := deps.Source.(Trial); ok {
		u.rolledBack = t.RolledBack()
		types = []string{EventInstalled, EventRolledBack, EventFailed}
	}
	u.events = &esphome.Event{
		Base: esphome.Base{
			ObjectID: "update_outcome",
			Name:     "Update outcome",
			Icon:     "mdi:package-up",
		},
		Types: types,
	}

	if _, ok := deps.Source.(Builds[M]); ok {
		u.versions = &esphome.Select{
			Base: esphome.Base{
				ObjectID: "install_version",
				Name:     "Install version",
				Icon:     "mdi:history",
				Category: esphome.CategoryConfig,
			},
			Options:   []string{unpicked},
			OnCommand: u.pick,
		}
		if builds := u.loadList(); len(builds) > 0 {
			u.versions.Options = u.options(builds)
			u.catalog = Catalog{Builds: builds}
		}
		u.versions.Set(unpicked)
		u.apply = &esphome.Button{
			Base: esphome.Base{
				ObjectID: "install_selected_version",
				Name:     "Install selected version",
				Icon:     "mdi:package-down",
				Category: esphome.CategoryConfig,
			},
			OnPress: u.installPicked,
		}
	}
	return u
}

func (u *Firmware[M]) pick(version string) {
	if !slices.Contains(u.options(u.Catalog().Builds), version) {
		slog.Warn("unknown option", "setting", u.versions.ObjectID, "value", version)
		return
	}
	u.versions.Set(version)
}

func (u *Firmware[M]) installPicked() {
	version := u.versions.Get()
	u.versions.Set(unpicked)
	if version == unpicked || version == u.deps.Version {
		return
	}
	u.InstallBuildLater(version)
}

func (u *Firmware[M]) Name() string { return "firmware" }

func (u *Firmware[M]) Entities() []esphome.Entity {
	entities := []esphome.Entity{u.entity, u.channel, u.look, u.status, u.events}
	if u.versions != nil {
		entities = append(entities, u.versions, u.apply)
	}
	return entities
}

func (u *Firmware[M]) Restore() { u.channel.Set(u.Channel()) }

func (u *Firmware[M]) RebootPending(pending bool) {
	u.mu.Lock()
	u.rebootPending = pending
	u.mu.Unlock()
}

func (u *Firmware[M]) Announce() {
	u.announced.Do(func() {
		u.mu.Lock()
		pending := u.rebootPending
		u.mu.Unlock()

		version := u.deps.Version
		finish := "installed " + version + ", reboot to finish"
		if u.deps.Settings.Read().LastVersion == version {
			if pending {
				u.status.Set(component.Fit(finish))
			}
			return
		}

		event, detail := EventInstalled, "installed "+version
		if u.rolledBack != "" {
			event, detail = EventRolledBack, "rolled back from "+u.rolledBack
		} else if pending {
			detail = finish
		}
		u.Settled(event, detail)

		if err := u.deps.Settings.LastVersion(version); err != nil {
			slog.Error("saving the reported version failed", "err", err)
		}
	})
}

func (u *Firmware[M]) Settled(event, status string) {
	u.status.Set(component.Fit(status))
	u.events.Trigger(event)
}

func (u *Firmware[M]) Channel() string {
	saved := u.deps.Settings.Read().Channel
	if len(u.deps.Channels) == 0 || slices.Contains(u.deps.Channels, saved) {
		return saved
	}
	return u.deps.Channels[0]
}

func (u *Firmware[M]) Channels() []string { return slices.Clone(u.deps.Channels) }

func (u *Firmware[M]) Version() string { return u.deps.Version }

func (u *Firmware[M]) Choose(label string) {
	if !slices.Contains(u.deps.Channels, label) {
		slog.Warn("unknown option", "setting", u.channel.ObjectID, "value", label)
		return
	}
	u.channel.Set(label)
	if err := u.deps.Settings.Channel(label); err != nil {
		slog.Error("saving a setting failed", "setting", u.channel.ObjectID, "err", err)
	}
	slog.Info("setting changed", "setting", u.channel.ObjectID, "using", label)
	u.CheckLater()
}

func (u *Firmware[M]) Check(ctx context.Context) {
	channel := u.Channel()
	u.offered(Offer{Finding: Checking})

	found, err := u.deps.Source.Fetch(ctx, channel)
	if err != nil {
		slog.Error("checking for an update failed", "channel", channel, "err", err)
		u.offered(Offer{Finding: Unreachable})
		return
	}

	u.mu.Lock()
	u.found = found
	u.mu.Unlock()

	release := u.deps.Source.Describe(found)
	finding := Current
	if Newer(release.Version, u.deps.Version) {
		finding = Available
	}
	u.offered(Offer{Finding: finding, Release: release})
	slog.Info(
		"update check",
		"channel",
		channel,
		"running",
		u.deps.Version,
		"offered",
		release.Version,
	)
	u.publish(release)
}

func (u *Firmware[M]) CheckLater() {
	safe.Go("update check", func() { u.Check(context.Background()) })
}

func (u *Firmware[M]) InstallLater() {
	safe.Go("update install", func() { u.Install(context.Background()) })
}

func (u *Firmware[M]) Offer() Offer {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.offer
}

func (u *Firmware[M]) offered(o Offer) {
	u.mu.Lock()
	u.offer = o
	u.mu.Unlock()
	u.Offered.Emit(o)
}

func (u *Firmware[M]) command(cmd esphome.UpdateCommand) {
	switch cmd {
	case esphome.UpdateCheck:
		u.CheckLater()

	case esphome.UpdateInstall:
		u.InstallLater()
	}
}

func (u *Firmware[M]) Install(ctx context.Context) {
	found, err := u.deps.Source.Fetch(ctx, u.Channel())
	u.mu.Lock()
	if err != nil {
		slog.Warn("re-reading the channel failed, using the last check", "err", err)
		found = u.found
	} else {
		u.found = found
	}
	u.mu.Unlock()
	u.install(ctx, found)
}

func (u *Firmware[M]) Catalog() Catalog {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.catalog
}

func (u *Firmware[M]) ListLater() {
	safe.Go("update list", func() { _ = u.List(context.Background()) })
}

func (u *Firmware[M]) List(ctx context.Context) error {
	b, ok := u.deps.Source.(Builds[M])
	if !ok {
		return nil
	}
	before := u.Catalog().Builds
	u.listed(Catalog{Finding: Checking, Builds: before})

	builds, err := b.Builds(ctx)
	if err != nil {
		slog.Error("listing releases failed", "err", err)
		u.listed(Catalog{Finding: Unreachable, Builds: before})
		return err
	}
	options := u.options(builds)
	changed := !slices.Equal(options, u.options(before))
	u.versions.SetOptions(options)
	u.listed(Catalog{Finding: Current, Builds: builds})
	if changed {
		u.saveList(builds)
		if u.deps.Reconnect != nil {
			u.deps.Reconnect()
		}
	}
	return nil
}

func (u *Firmware[M]) loadList() []releases.Release {
	if u.deps.ListPath == "" {
		return nil
	}
	raw, err := os.ReadFile(u.deps.ListPath)
	if err != nil {
		return nil
	}
	var builds []releases.Release
	if err := json.Unmarshal(raw, &builds); err != nil {
		slog.Warn("the saved release list is unreadable", "path", u.deps.ListPath, "err", err)
		return nil
	}
	return builds
}

func (u *Firmware[M]) saveList(builds []releases.Release) {
	if u.deps.ListPath == "" {
		return
	}
	raw, err := json.Marshal(builds)
	if err == nil {
		err = safe.WriteFile(u.deps.ListPath, raw, 0o600)
	}
	if err != nil {
		slog.Warn("saving the release list failed", "path", u.deps.ListPath, "err", err)
	}
}

func (u *Firmware[M]) options(builds []releases.Release) []string {
	options := []string{unpicked}
	for _, r := range builds {
		if r.Tag != u.deps.Version {
			options = append(options, r.Tag)
		}
	}
	return options
}

func (u *Firmware[M]) listed(c Catalog) {
	u.mu.Lock()
	u.catalog = c
	u.mu.Unlock()
	u.Listed.Emit(c)
}

func (u *Firmware[M]) InstallBuildLater(version string) {
	safe.Go("update install build", func() { u.InstallBuild(context.Background(), version) })
}

func (u *Firmware[M]) InstallBuild(ctx context.Context, version string) {
	b, ok := u.deps.Source.(Builds[M])
	if !ok {
		return
	}
	if version == u.deps.Version {
		return
	}
	found, err := b.FetchBuild(ctx, version)
	if err != nil {
		slog.Error("fetching a release failed", "version", version, "err", err)
		u.Settled(EventFailed, "installing "+version+" failed: "+err.Error())
		return
	}
	u.install(ctx, found)
}

func (u *Firmware[M]) install(ctx context.Context, found M) {
	release := u.deps.Source.Describe(found)
	if release.Version == "" || release.Version == u.deps.Version {
		slog.Warn("an install was asked for with nothing to install", "running", u.deps.Version)
		return
	}
	if busy := u.claim(release); busy != "" {
		slog.Warn("an install is already under way", "installing", busy, "asked", release.Version)
		return
	}

	refresh := func() {}
	if u.deps.Busy != nil {
		working := u.deps.Busy.Begin()
		defer working.Done()
		refresh = working.Refresh
	}

	u.progress(release, 0)
	err := u.deps.Source.Install(ctx, found, func(at float32) {
		refresh()
		u.progress(release, at)
	})

	// The update entity has no way to report a failed install.
	if err != nil {
		slog.Error("installing an update failed", "version", release.Version, "err", err)
		u.upgrading(Upgrade{})
		u.publish(release)
		u.Settled(EventFailed, "installing "+release.Version+" failed: "+err.Error())
		if u.deps.OnFailure != nil {
			u.deps.OnFailure()
		}
		return
	}
	u.deps.Source.Restart("update to " + release.Version)
}

func (u *Firmware[M]) claim(release Release) (busy string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.upgrade.Version != "" {
		return u.upgrade.Version
	}
	u.upgrade = Upgrade{Version: release.Version}
	return ""
}

func (u *Firmware[M]) progress(release Release, at float32) {
	state := u.state(release)
	state.InProgress, state.Progress = true, at*100
	u.entity.Set(state)
	u.upgrading(Upgrade{Version: release.Version, At: at})
}

func (u *Firmware[M]) upgrading(up Upgrade) {
	u.mu.Lock()
	u.upgrade = up
	u.mu.Unlock()
	u.Upgrading.Emit(up)
}

func (u *Firmware[M]) Upgrade() Upgrade {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.upgrade
}

func (u *Firmware[M]) publish(release Release) { u.entity.Set(u.state(release)) }

func (u *Firmware[M]) state(release Release) esphome.UpdateState {
	return esphome.UpdateState{
		CurrentVersion: u.deps.Version,
		LatestVersion:  release.Version,
		Title:          u.deps.Product + " (" + u.Channel() + " channel)",
		ReleaseSummary: release.Notes,
		ReleaseURL:     release.ReleaseURL,
	}
}
