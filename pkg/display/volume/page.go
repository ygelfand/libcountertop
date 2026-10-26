package volume

import (
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/audio/tone"

	"github.com/ygelfand/libcountertop/pkg/display/menu"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const settle = 400 * time.Millisecond

type Levels interface {
	Level(schema.Stream) int
	Set(schema.Stream, int)
}
type Chimes interface {
	Chime() schema.Chime
	SetChime(schema.Chime)
	Preview(schema.Stream)
}

type PageOptions struct {
	Levels    Levels
	Streams   []schema.Stream
	MainSteps int
	Chimes    Chimes
	Feedback  *tone.Feedback
	Media     *setting.Controls[schema.Media]
	Outputs   Rows
	Push      func(shell.View)
}

type Rows func() ([]menu.Row, []func(int))

func Segments[T any](c *setting.Controls[T]) Rows {
	return func() ([]menu.Row, []func(int)) {
		var rows []menu.Row
		var actions []func(int)
		for _, field := range c.Table.Rows() {
			row, act := segments(c, field)
			rows, actions = append(rows, row), append(actions, act)
		}
		return rows, actions
	}
}

func NewPage(o PageOptions) *shell.Page {
	streams := slices.Clone(o.Streams)
	hear := &preview{chimes: o.Chimes}
	return &shell.Page{Title: say.T("settings.volume"), Build: func() ([]menu.Row, []func(int)) {
		var rows []menu.Row
		var actions []func(int)
		for _, s := range streams {
			row := menu.Row{
				Label: say.T("sound." + string(s)),
				Kind:  menu.Slider,
				Level: o.Levels.Level(s),
			}
			if s == schema.StreamMain && o.MainSteps > 0 {
				row.Snap = menu.Steps(o.MainSteps + 1).Snap
			}
			rows = append(rows, row)
			actions = append(actions, func(level int) {
				o.Levels.Set(s, level)
				hear.after(s)
			})
		}
		if o.Media != nil {
			if field, ok := o.Media.Table.Find("buttons"); ok {
				row, act := segments(o.Media, field)
				rows, actions = append(rows, row), append(actions, act)
			}
		}
		if o.Outputs != nil {
			more, act := o.Outputs()
			rows, actions = append(rows, more...), append(actions, act...)
		}
		if o.Feedback != nil && o.Push != nil {
			at := o.Feedback.Read()
			for _, field := range o.Feedback.Table.Rows() {
				rows = append(
					rows,
					menu.Row{Label: field.Named(), Kind: menu.Chevron, Value: field.Visible(&at)},
				)
				actions = append(actions, func(int) {
					o.Push(feedbackChoice(o.Feedback, field))
				})
			}
		} else if o.Chimes != nil {
			chimes := schema.Chimes()
			labels := make([]string, len(chimes))
			for i, c := range chimes {
				labels[i] = c.Label()
			}
			rows = append(rows, menu.Row{
				Label:   say.T("volume.chime"),
				Kind:    menu.Segments,
				Options: labels,
				Level:   max(slices.Index(chimes, o.Chimes.Chime()), 0),
			})
			actions = append(actions, func(i int) {
				o.Chimes.SetChime(chimes[i])
				o.Chimes.Preview(schema.StreamFeedback)
			})
		}
		return rows, actions
	}}
}

func segments[T any](c *setting.Controls[T], field setting.Setting[T]) (menu.Row, func(int)) {
	at := c.Read()
	values := field.Values()
	labels := make([]string, len(values))
	for i, v := range values {
		labels[i] = field.LabelOf(v)
	}
	row := menu.Row{
		Label:   field.Title(),
		Kind:    menu.Segments,
		Options: labels,
		Level:   max(slices.Index(values, field.Read(&at)), 0),
	}
	return row, func(i int) {
		if i < 0 || i >= len(values) {
			return
		}
		if err := c.Change(field.Name, values[i]); err != nil {
			slog.Error("saving a setting failed", "setting", c.Domain()+"."+field.Name, "err", err)
		}
	}
}

func feedbackChoice(f *tone.Feedback, field setting.Setting[schema.Feedback]) *shell.Page {
	return &shell.Page{Title: field.Named(), Build: func() ([]menu.Row, []func(int)) {
		now := f.Read()
		values := field.Values()
		rows := make([]menu.Row, 0, len(values))
		actions := make([]func(int), 0, len(values))
		for _, value := range values {
			rows = append(
				rows,
				menu.Row{Label: field.LabelOf(value), Chosen: field.Read(&now) == value},
			)
			actions = append(actions, func(int) {
				if err := f.Change(field.Name, value); err != nil {
					slog.Error("saving feedback sound failed", "setting", field.Name, "err", err)
					return
				}
				f.PreviewSetting(field.Name)
			})
		}
		return rows, actions
	}}
}

type preview struct {
	chimes Chimes

	mu    sync.Mutex
	timer *time.Timer
}

func (p *preview) after(s schema.Stream) {
	if p.chimes == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(settle, func() { p.chimes.Preview(s) })
}
