package settings

import (
	"testing"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
)

type service struct {
	On       bool
	User     string
	Password string
}

func serviceControls(at *service) *setting.Controls[service] {
	secret := setting.Words(
		setting.Setting[service]{Name: "password", Group: "Service"},
		64,
		func(c *service) *string { return &c.Password },
	)
	secret.Kind = setting.Secret
	table := setting.NewTable("service", []setting.Group{"Service"}, []setting.Setting[service]{
		setting.Flag(
			setting.Setting[service]{Name: "on", Group: "Service"},
			func(c *service) *bool { return &c.On },
		),
		setting.Words(
			setting.Setting[service]{Name: "user", Group: "Service"},
			64,
			func(c *service) *string { return &c.User },
		),
		secret,
	}, setting.Messages{Text: func(k string) string { return k }})
	return &setting.Controls[service]{
		Table: table,
		Read:  func() service { return *at },
		Save: func(s setting.Setting[service], v string) error {
			return s.Write(at, v)
		},
	}
}

func TestAServicePageEditsItsTable(t *testing.T) {
	at := service{User: "admin"}
	page := ServicePage("Service", serviceControls(&at))

	rows, taps := page.Build()
	if len(rows) != 3 {
		t.Fatalf("%d rows, want on, user and password", len(rows))
	}
	if rows[0].Kind != menu.Toggle || rows[0].On || taps[0] == nil {
		t.Errorf("on row is %+v", rows[0])
	}
	if rows[1].Kind != menu.Field || rows[1].Value != "admin" {
		t.Errorf("user row is %+v", rows[1])
	}
	if rows[2].Kind != menu.Password || rows[2].Value != "" || rows[2].Hint != "" {
		t.Errorf("an unset password row is %+v", rows[2])
	}

	taps[0](0)
	rows[1].Save("viewer")
	rows[2].Save("pw")
	if !at.On || at.User != "viewer" || at.Password != "pw" {
		t.Fatalf("saved %+v", at)
	}

	rows, _ = page.Build()
	if !rows[0].On || rows[2].Value != "pw" {
		t.Errorf("after saving: on %+v, password %+v", rows[0], rows[2])
	}
}

func TestAServicePageShowsAChoiceAsSegments(t *testing.T) {
	mode := "fast"
	table := setting.NewTable("service", []setting.Group{"Service"}, []setting.Setting[string]{{
		Name: "mode", Group: "Service", Kind: setting.Choice,
		Options: []setting.Option{{Value: "fast", Key: "fast"}, {Value: "exact", Key: "exact"}},
		Read:    func(c *string) string { return *c },
		Write: func(c *string, v string) error {
			*c = v
			return nil
		},
	}}, setting.Messages{Text: func(k string) string { return k }})
	page := ServicePage("Service", &setting.Controls[string]{
		Table: table,
		Read:  func() string { return mode },
		Save:  func(s setting.Setting[string], v string) error { return s.Write(&mode, v) },
	})

	rows, taps := page.Build()
	if len(rows) != 1 || rows[0].Kind != menu.Segments || rows[0].Level != 0 {
		t.Fatalf("rows are %+v", rows)
	}
	if want := "service.option.mode.exact"; rows[0].Options[1] != want {
		t.Errorf("second option is %q, want %q", rows[0].Options[1], want)
	}
	taps[0](1)
	if mode != "exact" {
		t.Errorf("saved %q, want exact", mode)
	}
}
