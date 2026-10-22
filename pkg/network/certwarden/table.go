package certwarden

import (
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const Group setting.Group = "Certificate"

const textMost = 1024

type Setting = setting.Setting[schema.Certs]

func NewTable() *setting.Table[schema.Certs] {
	text := func(name, icon string, field func(*schema.Certs) *string) Setting {
		return setting.Words(
			Setting{Name: name, Group: Group, Icon: icon},
			textMost,
			field,
		)
	}
	secret := func(name string, field func(*schema.Certs) *string) Setting {
		s := text(name, "mdi:key", field)
		s.Kind = setting.Secret
		return s
	}
	return setting.NewTable("certs", []setting.Group{Group}, []Setting{
		text("server", "mdi:server-security", func(c *schema.Certs) *string { return &c.Server }),
		text("cert", "mdi:certificate", func(c *schema.Certs) *string { return &c.Cert }),
		secret("cert_api_key", func(c *schema.Certs) *string { return &c.CertAPIKey }),
		secret("key_api_key", func(c *schema.Certs) *string { return &c.KeyAPIKey }),
	}, setting.Messages{Text: say.T, Missing: say.Missing})
}
