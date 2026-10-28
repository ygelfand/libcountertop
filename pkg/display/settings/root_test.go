package settings

import (
	"testing"

	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

func TestRootCreatesPagesOnlyWhenOpened(t *testing.T) {
	created := 0
	page := func() shell.View { created++; return &shell.Page{} }
	o := RootDependencies{
		Network:      rootNetwork{},
		Screen:       rootScreen{},
		Volume:       rootVolume{},
		Shell:        shell.New(),
		Basic:        NewBasicPages(BasicDependencies{}),
		Assistants:   &AssistantPages{options: AssistantDependencies{Slots: 0}},
		NetworkPage:  page,
		FeaturesPage: page,
		VolumePage:   page,
		AboutPage:    page,
	}

	rows, actions := RootPage(o).Build()
	if created != 0 || len(actions) != 0 {
		t.Fatalf("created=%d rows=%d actions=%d", created, len(rows), len(actions))
	}
	o.Shell.Push(rows[0].Open())
	if created != 1 {
		t.Fatalf("created=%d after opening one page", created)
	}
}

type rootNetwork struct{}

func (rootNetwork) Network() string { return "" }

type rootScreen struct{}

func (rootScreen) Read() schema.Screen               { return schema.Screen{} }
func (rootScreen) Update(func(*schema.Screen)) error { return nil }

type rootVolume struct{}

func (rootVolume) Level() int             { return 0 }
func (rootVolume) Set(int)                {}
func (rootVolume) Watch(func(int)) func() { return func() {} }
