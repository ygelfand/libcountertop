package castreceiver

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/ygelfand/libcountertop/pkg/media/cast"
)

type fakeVolume struct {
	mu     sync.Mutex
	level  int
	muted  bool
	listen []func(int)
	set    chan int
}

func (v *fakeVolume) Level() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.level
}

func (v *fakeVolume) SetLevel(level int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.level = level
}

func (v *fakeVolume) Mute(on bool) {
	v.mu.Lock()
	v.muted = on
	level := v.level
	v.mu.Unlock()
	v.set <- level
}

func (v *fakeVolume) Muted() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.muted
}

func (v *fakeVolume) Listen(f func(int)) func() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.listen = append(v.listen, f)
	return func() {}
}

func TestStoppingWithoutStartingIsSafe(t *testing.T) {
	dir := t.TempDir()
	s := New(Config{
		Name:            func() string { return "Kitchen" },
		CredentialsPath: filepath.Join(dir, "cast-credentials.json"),
		AuthorityPath:   filepath.Join(dir, "cast-authority.json"),
		Credentials:     func(*cast.Credentials) {},
		Volume:          &fakeVolume{set: make(chan int, 1)},
	})

	s.Down()
	s.Down()
	if s.Listening() {
		t.Error("a socket is open")
	}
	if s.Playing() != nil {
		t.Error("something is playing")
	}
}

func TestPreparingMakesCredentialsOnce(t *testing.T) {
	dir := t.TempDir()
	var seen []*cast.Credentials
	s := New(Config{
		Name:            func() string { return "Kitchen" },
		CredentialsPath: filepath.Join(dir, "cast-credentials.json"),
		AuthorityPath:   filepath.Join(dir, "cast-authority.json"),
		Credentials:     func(c *cast.Credentials) { seen = append(seen, c) },
		Volume:          &fakeVolume{set: make(chan int, 1)},
	})

	if err := s.Prepare(); err != nil {
		t.Fatal(err)
	}
	if err := s.Prepare(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] == nil || seen[0].Remote {
		t.Errorf("the credentials reported are %v", seen)
	}
}

func TestASenderVolumeReachesTheDevice(t *testing.T) {
	v := &fakeVolume{set: make(chan int, 1)}
	l := newLoudness(v)

	l.asked(0.42, false)
	if got := <-v.set; got != 42 {
		t.Errorf("the level is %d, want 42", got)
	}
	l.asked(0.42, true)
	if got := <-v.set; got != 42 || !v.Muted() {
		t.Errorf("muted at %d, want 42 kept and muted", got)
	}
	l.asked(0.42, false)
	<-v.set
	if v.Muted() {
		t.Error("unmuting left the device muted")
	}
}

func TestContentShapeHidesThePath(t *testing.T) {
	for id, want := range map[string]string{
		"https://example.com/a/b?token=x": "https://example.com",
		"abc123":                          "opaque, 6 characters",
	} {
		if got := contentShape(id); got != want {
			t.Errorf("contentShape(%q) = %q, want %q", id, got, want)
		}
	}
}
