package firmware

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ygelfand/libcountertop/pkg/network/releases"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type manifest struct{ version string }

type source struct {
	offered   string
	fetched   error
	installed error
	restarted string

	hold     chan struct{}
	installs atomic.Int32
	restarts chan string
}

func (s *source) Fetch(context.Context, string) (manifest, error) {
	return manifest{version: s.offered}, s.fetched
}

func (s *source) Describe(m manifest) Release { return Release{Version: m.version} }

func (s *source) Install(_ context.Context, _ manifest, progress func(float32)) error {
	s.installs.Add(1)
	progress(0.5)
	if s.hold != nil {
		<-s.hold
	}
	return s.installed
}

func (s *source) Restart(why string) {
	s.restarted = why
	if s.restarts != nil {
		s.restarts <- why
	}
}

func restarted(t *testing.T, s *source) string {
	t.Helper()
	select {
	case why := <-s.restarts:
		return why
	case <-time.After(5 * time.Second):
		t.Fatal("the install never finished")
		return ""
	}
}

type trialSource struct {
	*source
	from string
}

func (t trialSource) RolledBack() string { return t.from }
func (t trialSource) Commit()            {}

type settings struct{ schema.Update }

func (s *settings) Read() schema.Update        { return s.Update }
func (s *settings) Channel(v string) error     { s.Update.Channel = v; return nil }
func (s *settings) LastVersion(v string) error { s.Update.LastVersion = v; return nil }

func build(src Source[manifest], saved *settings) *Firmware[manifest] {
	return New(Dependencies[manifest]{
		Source:   src,
		Settings: saved,
		Product:  "Thing",
		Version:  "1.0",
		Channels: []string{"stable", "dev"},
	})
}

func TestAnUnknownSavedChannelFallsBackToTheFirst(t *testing.T) {
	u := build(&source{}, &settings{schema.Update{Channel: "nightly"}})
	if got := u.Channel(); got != "stable" {
		t.Errorf("channel = %q, want stable", got)
	}
	if got := u.channel.Get(); got != "stable" {
		t.Errorf("select shows %q, want stable", got)
	}
}

func TestChoosingAnUnknownChannelChangesNothing(t *testing.T) {
	saved := &settings{schema.Update{Channel: "dev"}}
	u := build(&source{}, saved)
	u.Choose("nightly")
	if saved.Update.Channel != "dev" {
		t.Errorf("saved channel = %q, want dev", saved.Update.Channel)
	}
}

func TestAFailedInstallIsReported(t *testing.T) {
	src := &source{offered: "2.0", installed: errors.New("disk full")}
	failed := 0
	u := New(Dependencies[manifest]{
		Source:    src,
		Settings:  &settings{},
		Version:   "1.0",
		Channels:  []string{"stable"},
		OnFailure: func() { failed++ },
	})

	u.Install(context.Background())

	if failed != 1 {
		t.Errorf("failure feedback ran %d times, want 1", failed)
	}
	if u.Upgrade().Active() {
		t.Error("an upgrade is still shown after it failed")
	}
	if got := u.status.Get(); !strings.Contains(got, "disk full") {
		t.Errorf("status = %q", got)
	}
	if src.restarted != "" {
		t.Error("restarted after a failed install")
	}
}

func TestASuccessfulInstallRestarts(t *testing.T) {
	src := &source{offered: "2.0"}
	u := build(src, &settings{})
	u.Install(context.Background())
	if src.restarted != "update to 2.0" {
		t.Errorf("restart reason = %q", src.restarted)
	}
}

func TestNothingNewInstallsNothing(t *testing.T) {
	src := &source{offered: "1.0"}
	u := build(src, &settings{})
	u.Install(context.Background())
	if src.restarted != "" {
		t.Error("reinstalled the running version")
	}
}

func TestRollbackIsAnnouncedOnlyWithATrial(t *testing.T) {
	plain := build(&source{}, &settings{})
	if slices.Contains(plain.events.Types, EventRolledBack) {
		t.Error("a source without trials offers a rollback event")
	}

	saved := &settings{}
	u := build(trialSource{source: &source{}, from: "1.1"}, saved)
	if !slices.Contains(u.events.Types, EventRolledBack) {
		t.Error("a trial source offers no rollback event")
	}
	u.Announce()
	if got := u.status.Get(); got != "rolled back from 1.1" {
		t.Errorf("status = %q", got)
	}
	if saved.Update.LastVersion != "1.0" {
		t.Errorf("last version = %q, want 1.0", saved.Update.LastVersion)
	}
}

func TestAPendingRebootIsShownOnce(t *testing.T) {
	u := build(&source{}, &settings{})
	u.RebootPending(true)
	u.Announce()
	if got := u.status.Get(); got != "installed 1.0, reboot to finish" {
		t.Errorf("status = %q", got)
	}
}

type buildSource struct {
	*source
	builds []releases.Release
	listed error
}

func (b buildSource) Builds(
	context.Context,
) ([]releases.Release, error) {
	return b.builds, b.listed
}

func (b buildSource) FetchBuild(_ context.Context, version string) (manifest, error) {
	return manifest{version: version}, b.fetched
}

func TestOnlyASourceWithBuildsOffersAVersionPicker(t *testing.T) {
	plain := build(&source{}, &settings{})
	if plain.versions != nil || len(plain.Entities()) != 5 {
		t.Error("a source without builds offers a version picker")
	}
	_ = plain.List(context.Background())
	if got := plain.Catalog().Finding; got != Unchecked {
		t.Errorf("finding = %d, want unchecked", got)
	}

	u := build(buildSource{source: &source{}}, &settings{})
	if u.versions == nil || len(u.Entities()) != 7 {
		t.Fatal("a source with builds offers no version picker")
	}
	if got := u.versions.Get(); got != unpicked {
		t.Errorf("picker starts at %q, want %q", got, unpicked)
	}
}

func TestPickingAVersionInstallsNothingUntilTheButton(t *testing.T) {
	src := &source{restarts: make(chan string, 1)}
	builds := []releases.Release{{Tag: "1.0"}, {Tag: "0.9"}}
	u := build(buildSource{source: src, builds: builds}, &settings{})
	_ = u.List(context.Background())

	u.pick("0.8")
	if got := u.versions.Get(); got != unpicked {
		t.Errorf("picked %q, a version that is not offered", got)
	}
	u.pick("0.9")
	if got := u.versions.Get(); got != "0.9" {
		t.Fatalf("picker shows %q, want 0.9", got)
	}
	if src.installs.Load() != 0 {
		t.Fatal("picking a version installed it")
	}

	u.installPicked()
	if got := u.versions.Get(); got != unpicked {
		t.Errorf("picker shows %q after the button, want %q", got, unpicked)
	}
	if why := restarted(t, src); why != "update to 0.9" {
		t.Errorf("restart reason = %q", why)
	}
}

func TestASecondInstallIsRefusedWhileOneRuns(t *testing.T) {
	src := &source{hold: make(chan struct{}), restarts: make(chan string, 1)}
	u := build(
		buildSource{source: src, builds: []releases.Release{{Tag: "0.9"}, {Tag: "0.8"}}},
		&settings{},
	)

	u.InstallBuildLater("0.9")
	for deadline := time.Now().Add(5 * time.Second); u.Upgrade().Version == ""; {
		if time.Now().After(deadline) {
			t.Fatal("the first install never started")
		}
		time.Sleep(time.Millisecond)
	}

	u.InstallBuild(context.Background(), "0.8")
	if n := src.installs.Load(); n != 1 {
		t.Errorf("%d installs ran, want only the first", n)
	}
	if got := u.Upgrade().Version; got != "0.9" {
		t.Errorf("installing %q, want the first install's 0.9", got)
	}

	close(src.hold)
	if why := restarted(t, src); why != "update to 0.9" {
		t.Errorf("restart reason = %q", why)
	}
}

func TestTheButtonWithNothingPickedInstallsNothing(t *testing.T) {
	src := &source{}
	u := build(buildSource{source: src}, &settings{})
	u.installPicked()
	u.pick("1.0")
	u.installPicked()
	if src.restarted != "" {
		t.Error("the button installed with nothing to install")
	}
}

func TestTheListOffersEveryVersionButTheRunningOne(t *testing.T) {
	builds := []releases.Release{{Tag: "1.1-dev.2", Prerelease: true}, {Tag: "1.0"}, {Tag: "0.9"}}
	u := build(buildSource{source: &source{}, builds: builds}, &settings{})
	var seen []Finding
	u.Listed.Listen(func(c Catalog) { seen = append(seen, c.Finding) })

	if err := u.List(context.Background()); err != nil {
		t.Fatal(err)
	}

	if want := []Finding{Checking, Current}; !slices.Equal(seen, want) {
		t.Errorf("listed %v, want %v", seen, want)
	}
	if got := u.Catalog().Builds; !slices.Equal(got, builds) {
		t.Errorf("builds = %v, want %v", got, builds)
	}
	if want := []string{unpicked, "1.1-dev.2", "0.9"}; !slices.Equal(u.versions.Options, want) {
		t.Errorf("options = %v, want %v", u.versions.Options, want)
	}
}

func TestHomeAssistantReconnectsOnlyWhenTheOptionsChange(t *testing.T) {
	reconnects := 0
	src := buildSource{source: &source{}, builds: []releases.Release{{Tag: "0.9"}}}
	u := New(Dependencies[manifest]{
		Source:    src,
		Settings:  &settings{},
		Version:   "1.0",
		Channels:  []string{"stable"},
		Reconnect: func() { reconnects++ },
	})
	_ = u.List(context.Background())
	_ = u.List(context.Background())
	if reconnects != 1 {
		t.Errorf("reconnected %d times, want 1", reconnects)
	}
}

func TestAListSavedLastBootNeedsNoReconnect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "releases.json")
	builds := []releases.Release{{Tag: "0.9"}, {Tag: "0.8"}}
	deps := func(reconnect func()) Dependencies[manifest] {
		return Dependencies[manifest]{
			Source:    buildSource{source: &source{}, builds: builds},
			Settings:  &settings{},
			Version:   "1.0",
			Channels:  []string{"stable"},
			Reconnect: reconnect,
			ListPath:  path,
		}
	}
	_ = New(deps(func() {})).List(context.Background())

	reconnects := 0
	u := New(deps(func() { reconnects++ }))
	if want := []string{unpicked, "0.9", "0.8"}; !slices.Equal(u.versions.Options, want) {
		t.Errorf("a restart offered %v, want %v", u.versions.Options, want)
	}
	_ = u.List(context.Background())
	if reconnects != 0 {
		t.Errorf("the same list reconnected %d times", reconnects)
	}
}

func TestAFailedListKeepsWhatWasListed(t *testing.T) {
	u := build(buildSource{source: &source{}, listed: errors.New("rate limited")}, &settings{})
	u.listed(Catalog{Finding: Current, Builds: []releases.Release{{Tag: "0.9"}}})

	if err := u.List(context.Background()); err == nil {
		t.Error("a failed list returned no error")
	}

	got := u.Catalog()
	if got.Finding != Unreachable || len(got.Builds) != 1 {
		t.Errorf("catalog = %+v, want unreachable with the earlier build", got)
	}
}

func TestInstallingABuildInstallsThatVersionEvenWhenOlder(t *testing.T) {
	src := &source{}
	u := build(buildSource{source: src}, &settings{})
	u.InstallBuild(context.Background(), "0.9")
	if src.restarted != "update to 0.9" {
		t.Errorf("restart reason = %q", src.restarted)
	}
}

func TestInstallingTheRunningBuildDoesNothing(t *testing.T) {
	src := &source{}
	u := build(buildSource{source: src}, &settings{})
	u.InstallBuild(context.Background(), "1.0")
	if src.restarted != "" {
		t.Error("reinstalled the running version")
	}
}

func TestABuildThatCannotBeFetchedIsReported(t *testing.T) {
	src := &source{fetched: errors.New("404 Not Found")}
	u := build(buildSource{source: src}, &settings{})
	u.InstallBuild(context.Background(), "0.9")
	if src.restarted != "" {
		t.Error("restarted without a build")
	}
	if got := u.status.Get(); !strings.Contains(got, "404") {
		t.Errorf("status = %q", got)
	}
}

func TestACheckReportsWhatItFound(t *testing.T) {
	for _, c := range []struct {
		name string
		src  *source
		want Finding
	}{
		{"newer", &source{offered: "2.0"}, Available},
		{"older", &source{offered: "0.9"}, Current},
		{"same", &source{offered: "1.0"}, Current},
		{"empty", &source{}, Current},
		{"offline", &source{offered: "2.0", fetched: errors.New("no route")}, Unreachable},
	} {
		t.Run(c.name, func(t *testing.T) {
			u := build(c.src, &settings{})
			var seen []Finding
			u.Offered.Listen(func(o Offer) { seen = append(seen, o.Finding) })
			u.Check(context.Background())
			if got := u.Offer().Finding; got != c.want {
				t.Errorf("finding = %d, want %d", got, c.want)
			}
			if want := []Finding{Checking, c.want}; !slices.Equal(seen, want) {
				t.Errorf("offered %v, want %v", seen, want)
			}
		})
	}
}
