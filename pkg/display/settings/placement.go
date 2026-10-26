package settings

import (
	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/say"
	config "github.com/ygelfand/libcountertop/pkg/settings/schema"
)

type Placement interface {
	SetPosition(config.Position)
	SetAlign(config.Align)
	SetSize(config.Size)
}

type Placing struct {
	Positions []config.Position
	Aligns    []config.Align
	Read      func() config.Placement
	Set       Placement
	Preview   func(config.Placement) menu.Preview
}

func (o Placing) Rows(sh *shell.Shell) ([]menu.Row, []func(int)) {
	now := o.Read()
	push := func(page *shell.Page) func(int) { return func(int) { sh.Push(page) } }
	return []menu.Row{
			SegmentRow(say.T("place.vertical"), o.Positions, now.Position),
			SegmentRow(say.T("place.horizontal"), o.Aligns, now.Align),
			{Label: say.T("place.size"), Kind: menu.Chevron, Value: now.Size.Label()},
		}, []func(int){
			SegmentPick(o.Positions, o.Set.SetPosition),
			SegmentPick(o.Aligns, o.Set.SetAlign),
			push(Choose(
				say.T("place.size"),
				config.Sizes(),
				func() config.Size { return o.Read().Size },
				o.Set.SetSize,
				previewing(o, func(p *config.Placement, v config.Size) { p.Size = v }),
			)),
		}
}

func previewing[T any](
	o Placing,
	put func(*config.Placement, T),
) func(T) menu.Preview {
	if o.Preview == nil {
		return nil
	}
	return func(v T) menu.Preview {
		p := o.Read()
		put(&p, v)
		return o.Preview(p)
	}
}
