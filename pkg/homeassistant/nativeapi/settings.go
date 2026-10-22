package nativeapi

import (
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const Group setting.Group = "API"

func NewTable() *setting.Table[schema.API] {
	return setting.NewTable("access", []setting.Group{Group}, []setting.Setting[schema.API]{
		setting.Flag(
			setting.Setting[schema.API]{Name: "adopted", Group: Group, ID: "api_adopted"},
			func(c *schema.API) *bool { return &c.Adopted },
		),
	}, setting.Messages{Text: say.T, Missing: say.Missing}).Local()
}

func (a *Server) Controls() *setting.Controls[schema.API] { return a.knobs }

func (a *Server) controls() *setting.Controls[schema.API] {
	read := func() schema.API { return schema.API{Adopted: a.deps.Adoption.Adopted()} }
	return &setting.Controls[schema.API]{
		Table: NewTable(),
		Want:  func(setting.Group) bool { return false },
		Read:  read,
		Save: func(s setting.Setting[schema.API], v string) error {
			at := read()
			if err := s.Write(&at, v); err != nil {
				return err
			}
			return a.deps.Adoption.SetAdopted(at.Adopted)
		},
	}
}
