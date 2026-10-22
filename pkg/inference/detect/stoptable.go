package detect

import (
	"math"
	"strconv"

	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const StopGroup setting.Group = "Stop word"

const stopDomain = "assistants"

func (s *StopWord) Controls() *setting.Controls[schema.Stop] {
	return &setting.Controls[schema.Stop]{
		Table: StopTable(),
		Read:  func() schema.Stop { return schema.Stop{Threshold: s.options.Read()} },
		Save:  s.save,
	}
}

func StopTable() *setting.Table[schema.Stop] {
	row := setting.Setting[schema.Stop]{
		Name:     "stop_sensitivity",
		Group:    StopGroup,
		Kind:     setting.Number,
		Min:      50,
		Max:      100,
		Unit:     "%",
		NoEntity: true,
	}
	row.Read = func(c *schema.Stop) string {
		return strconv.Itoa(int(math.Round(c.Threshold * 100)))
	}
	row.Write = func(c *schema.Stop, v string) error {
		p, err := row.Number(v)
		if err != nil {
			return err
		}
		c.Threshold = float64(p) / 100
		return nil
	}
	return setting.NewTable(
		stopDomain,
		[]setting.Group{StopGroup},
		[]setting.Setting[schema.Stop]{row},
		setting.Messages{Text: say.T, Missing: say.Missing},
	)
}

func (s *StopWord) save(row setting.Setting[schema.Stop], v string) error {
	at := schema.Stop{Threshold: s.options.Read()}
	if err := row.Write(&at, v); err != nil {
		return err
	}
	err := s.options.Save(at.Threshold)
	s.Restore(s.options.Read())
	s.Load()
	return err
}
