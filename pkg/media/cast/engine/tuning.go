package engine

import (
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"

	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
	"github.com/ygelfand/libcountertop/pkg/system/diag"
)

const (
	lowMemory  = "low_memory"
	pageMemory = "page_memory"
)

type Tuning struct {
	settings storage.Section[schema.Diag]
	knobs    *setting.Controls[schema.Diag]

	mu      sync.Mutex
	restart func()
}

func NewTuning(settings storage.Section[schema.Diag]) *Tuning {
	t := &Tuning{settings: settings}
	t.knobs = &setting.Controls[schema.Diag]{
		Table: NewTable(),
		Read:  settings.Read,
		Save:  t.save,
	}
	return t
}

func NewTable() *setting.Table[schema.Diag] {
	return setting.NewTable("diag", []setting.Group{diag.Group}, []setting.Setting[schema.Diag]{
		setting.Flag(
			setting.Setting[schema.Diag]{Name: lowMemory, Group: diag.Group, Icon: "mdi:memory"},
			func(c *schema.Diag) *bool { return &c.LowMemory },
		),
		pageMemoryRow(),
	}, setting.Messages{Text: say.T, Missing: say.Missing})
}

func (t *Tuning) Controls() *setting.Controls[schema.Diag] { return t.knobs }

func (t *Tuning) Restarts(restart func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.restart = restart
}

func (t *Tuning) Args() []string {
	c := t.settings.Read()
	var out []string
	if c.LowMemory {
		out = append(out, "--enable-low-end-device-mode")
	}
	if c.PageMemory > 0 {
		out = append(out, "--js-flags=--max-old-space-size="+strconv.Itoa(c.PageMemory))
	}
	return out
}

func (t *Tuning) LowMemory() bool { return t.settings.Read().LowMemory }

func (t *Tuning) PageMemory() int { return t.settings.Read().PageMemory }

func (t *Tuning) SetLowMemory(on bool) { t.change(lowMemory, setting.OnOff(on)) }

func (t *Tuning) SetPageMemory(mb int) { t.change(pageMemory, strconv.Itoa(mb)) }

func (t *Tuning) change(name, value string) {
	if err := t.knobs.Change(name, value); err != nil {
		slog.Error("the setting could not be saved", "setting", name, "err", err)
	}
}

func (t *Tuning) save(s setting.Setting[schema.Diag], v string) error {
	if err := setting.Store(t.settings, s, v); err != nil {
		return err
	}
	t.mu.Lock()
	restart := t.restart
	t.mu.Unlock()
	if restart != nil {
		restart()
	}
	return nil
}

func pageMemoryRow() setting.Setting[schema.Diag] {
	s := setting.Setting[schema.Diag]{
		Name: pageMemory, Group: diag.Group, Icon: "mdi:memory-arrow-down", Kind: setting.Choice,
	}
	for _, n := range schema.PageMemorySteps {
		v := strconv.Itoa(n)
		if n == 0 {
			s.Options = append(s.Options, setting.Option{Value: v, Key: "none"})
			continue
		}
		s.Options = append(s.Options, setting.Option{Value: v, Label: v + " MB"})
	}
	s.Read = func(c *schema.Diag) string { return strconv.Itoa(c.PageMemory) }
	s.Write = func(c *schema.Diag, v string) error {
		n, err := strconv.Atoi(v)
		if err != nil || !slices.Contains(schema.PageMemorySteps, n) {
			return fmt.Errorf("%s takes one of %v, not %q", pageMemory, schema.PageMemorySteps, v)
		}
		c.PageMemory = n
		return nil
	}
	return s
}
