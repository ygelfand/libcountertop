package detect

import (
	"github.com/ygelfand/libcountertop/pkg/inference/wake"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const PryonGroup setting.Group = "Pryon"

type PryonOptions struct {
	Read    func() schema.Wake
	Save    func(schema.Wake) error
	Changed func()
}

type PryonSwitch struct {
	engine  *Engine
	options PryonOptions
}

func NewPryonSwitch(engine *Engine, options PryonOptions) *PryonSwitch {
	return &PryonSwitch{engine: engine, options: options}
}

func (p *PryonSwitch) Controls() *setting.Controls[schema.Wake] {
	return &setting.Controls[schema.Wake]{
		Table: PryonTable(),
		Read:  p.options.Read,
		Save:  p.save,
	}
}

func PryonTable() *setting.Table[schema.Wake] {
	return setting.NewTable(
		stopDomain,
		[]setting.Group{PryonGroup},
		[]setting.Setting[schema.Wake]{
			setting.Flag(
				setting.Setting[schema.Wake]{
					Name:  "pryon",
					Group: PryonGroup,
					Icon:  "mdi:account-voice",
				},
				func(w *schema.Wake) *bool { return &w.Pryon },
			),
			setting.Flag(
				setting.Setting[schema.Wake]{
					Name:  "pryon_stop",
					Group: PryonGroup,
					Icon:  "mdi:hand-back-left",
					Idle:  func(w *schema.Wake) bool { return !w.Pryon },
				},
				func(w *schema.Wake) *bool { return &w.Stop.Pryon },
			),
		},
		setting.Messages{Text: say.T, Missing: say.Missing},
	)
}

func (p *PryonSwitch) On() bool { return p.options.Read().Pryon }

func (p *PryonSwitch) Set(on bool) error {
	w := p.options.Read()
	w.Pryon = on
	return p.write(w)
}

func (p *PryonSwitch) Stop() *PryonStop { return &PryonStop{p} }

func (p *PryonSwitch) write(w schema.Wake) error {
	was := p.options.Read()
	if w.Pryon == was.Pryon && w.Stop.Pryon == was.Stop.Pryon {
		return nil
	}
	if err := p.options.Save(w); err != nil {
		return err
	}
	if !w.Pryon {
		p.engine.ClearKind(wake.KindPryon)
	}
	p.options.Changed()
	return nil
}

func (p *PryonSwitch) save(row setting.Setting[schema.Wake], v string) error {
	at := p.options.Read()
	if err := row.Write(&at, v); err != nil {
		return err
	}
	return p.write(at)
}

type PryonStop struct{ p *PryonSwitch }

func (s *PryonStop) On() bool {
	w := s.p.options.Read()
	return w.Pryon && w.Stop.Pryon
}

func (s *PryonStop) Available() bool { return s.p.On() }

func (s *PryonStop) Set(on bool) error {
	w := s.p.options.Read()
	w.Stop.Pryon = on
	return s.p.write(w)
}
