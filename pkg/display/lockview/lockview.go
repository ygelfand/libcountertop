package lockview

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	"github.com/ygelfand/libcountertop/pkg/display/navigation"
	"github.com/ygelfand/libcountertop/pkg/display/shell"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
	"github.com/ygelfand/libcountertop/pkg/say"
	setting "github.com/ygelfand/libcountertop/pkg/settings"
	"github.com/ygelfand/libcountertop/pkg/settings/schema"
)

const Group setting.Group = "Lock"

const (
	freeTries   = 5
	firstWait   = 30 * time.Second
	longestWait = time.Hour
)

var pinShape = regexp.MustCompile(`^[0-9]{4,8}$`)

var ErrShape = errors.New("lock: the pin takes 4 to 8 digits")

func NewTable() *setting.Table[schema.Lock] {
	return setting.NewTable("screen", []setting.Group{Group}, []setting.Setting[schema.Lock]{{
		Name:  "lock_pin",
		Group: Group,
		ID:    "lock_pin",
		Kind:  setting.Secret,
		Icon:  "mdi:lock",
		Read:  func(c *schema.Lock) string { return c.PIN },
		Write: func(c *schema.Lock, v string) error {
			v = strings.TrimSpace(v)
			if v != "" && !pinShape.MatchString(v) {
				return ErrShape
			}
			*c = schema.Lock{PIN: v}
			return nil
		},
	}}, setting.Messages{Text: say.T, Missing: say.Missing})
}

func Controls(
	device uint32,
	read func() schema.Lock,
	apply func(schema.Lock) error,
) *setting.Controls[schema.Lock] {
	return &setting.Controls[schema.Lock]{
		Table:  NewTable(),
		Device: device,
		Read:   read,
		Save: func(s setting.Setting[schema.Lock], v string) error {
			at := read()
			if err := s.Write(&at, v); err != nil {
				return err
			}
			return apply(at)
		},
	}
}

func Wait(failures int) time.Duration {
	over := failures - freeTries
	if over < 0 {
		return 0
	}
	if over >= 8 {
		return longestWait
	}
	return min(firstWait<<over, longestWait)
}

type Prompt struct{}

func (*Prompt) Covers() bool { return true }

type Setter struct{}

func (*Setter) Covers() bool { return true }

type Options struct {
	Shell  *shell.Shell
	Read   func() schema.Lock
	Save   func(schema.Lock) error
	SetPIN func(string) error
	Now    func() time.Time
}

type Gate struct {
	o      Options
	prompt Prompt
	setter Setter
	pad    *widgets.Keypad

	mu    sync.Mutex
	then  func()
	first string
	note  string
}

func New(o Options) *Gate {
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Gate{o: o, pad: &widgets.Keypad{ID: "lock", Redraw: o.Shell.Redraw}}
}

func (g *Gate) Open(then func()) {
	if g.o.Read().PIN == "" {
		then()
		return
	}
	g.mu.Lock()
	g.then = then
	g.mu.Unlock()
	g.pad.Reset()
	g.o.Shell.Push(&g.prompt)
}

func (g *Gate) Enter(code string) {
	at := g.o.Read()
	now := g.o.Now()
	if at.PIN == "" || now.Before(at.Until) {
		return
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(at.PIN)) == 1 {
		if at.Failures != 0 {
			at.Failures, at.Until = 0, time.Time{}
			g.save(at)
		}
		g.o.Shell.Remove(&g.prompt)
		g.mu.Lock()
		then := g.then
		g.then = nil
		g.mu.Unlock()
		if then != nil {
			then()
		}
		return
	}
	at.Failures++
	at.Until = now.Add(Wait(at.Failures))
	slog.Warn("wrong settings pin", "failures", at.Failures, "locked until", at.Until)
	g.save(at)
}

func (g *Gate) Locked() bool { return g.o.Read().PIN != "" }

func (g *Gate) Change() {
	g.mu.Lock()
	g.first, g.note = "", ""
	g.mu.Unlock()
	g.pad.Reset()
	g.o.Shell.Push(&g.setter)
}

func (g *Gate) Clear() {
	if err := g.o.SetPIN(""); err != nil {
		slog.Error("clearing the pin failed", "err", err)
	}
}

func (g *Gate) Choose(code string) {
	if !pinShape.MatchString(code) {
		return
	}
	g.mu.Lock()
	g.first, g.note = code, ""
	g.mu.Unlock()
	g.pad.Reset()
}

func (g *Gate) Confirm(code string) {
	g.mu.Lock()
	first := g.first
	if code != first {
		g.first, g.note = "", "lock.mismatch"
	}
	g.mu.Unlock()
	g.pad.Reset()
	if code != first {
		return
	}
	if err := g.o.SetPIN(code); err != nil {
		slog.Error("setting the pin failed", "err", err)
		return
	}
	g.o.Shell.Remove(&g.setter)
}

func (g *Gate) save(at schema.Lock) {
	if err := g.o.Save(at); err != nil {
		slog.Error("saving the lock failed", "err", err)
	}
}

func (g *Gate) Views(ui *widgets.UI) func(shell.View) *navigation.Screen {
	return func(sv shell.View) *navigation.Screen {
		switch sv {
		case &g.prompt:
			return &navigation.Screen{
				Title: say.T("lock.title"),
				View:  &g.prompt,
				Build: func(w *gogui.Window) gogui.View { return g.build(w, ui) },
			}
		case &g.setter:
			return &navigation.Screen{
				Title: say.T("screen.setting.lock_pin"),
				View:  &g.setter,
				Build: func(w *gogui.Window) gogui.View { return g.buildSetter(w, ui) },
			}
		}
		return nil
	}
}

func (g *Gate) buildSetter(w *gogui.Window, ui *widgets.UI) gogui.View {
	t := gogui.CurrentTheme().Cfg
	vw, vh := w.WindowSize()
	reach := style.Reach()
	wide := min(float32(vw)-2*reach, reach*12)

	g.mu.Lock()
	first, note := g.first, g.note
	g.mu.Unlock()
	typed := len(g.pad.Typed())

	status := t.TextStyleDef
	status.Size *= 1.4
	dots := t.TextStyleDef
	dots.Size = reach * 1.2
	dots.Color = t.ColorAccent

	content := []gogui.View{}
	if first == "" {
		says := say.T("lock.new")
		if note != "" {
			says = say.T(note)
		}
		state := style.Rest
		if typed >= 4 {
			state = style.Partial
		}
		ok := t.TextStyleDef
		ok.Size = reach * 0.5
		content = append(content,
			gogui.Label(says, status),
			gogui.Label(strings.Repeat("●", typed)+" ", dots),
			g.pad.View(&ui.Toolkit, wide, float32(vh), 8, g.Choose),
			gogui.Row(gogui.ContainerCfg{
				Width:   min(wide, min(reach*1.2, float32(vh)*0.07)*6),
				Sizing:  gogui.FixedFit,
				Padding: gogui.NoPadding,
				Content: []gogui.View{ui.Toolkit.WideKey(
					"lock-key-ok", say.T("lock.next"), ok, min(reach*1.2, float32(vh)*0.07), state,
					func(gogui.EventCtx) { g.Choose(g.pad.Typed()) },
				)},
			}),
		)
	} else {
		content = append(
			content,
			gogui.Label(say.T("lock.again"), status),
			gogui.Label(
				strings.Repeat("●", typed)+strings.Repeat("○", max(len(first)-typed, 0)),
				dots,
			),
			g.pad.View(&ui.Toolkit, wide, float32(vh), len(first), g.Confirm),
		)
	}
	return gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFill,
		Padding: gogui.NewPadding(reach*0.5, reach, reach*0.5, reach),
		Spacing: gogui.SpacingLarge,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Color:   t.ColorBackground,
		Content: content,
	})
}

func (g *Gate) build(w *gogui.Window, ui *widgets.UI) gogui.View {
	t := gogui.CurrentTheme().Cfg
	vw, vh := w.WindowSize()
	reach := style.Reach()
	wide := min(float32(vw)-2*reach, reach*12)

	at := g.o.Read()
	status := t.TextStyleDef
	status.Size *= 1.4
	says := say.T("lock.enter")
	if left := at.Until.Sub(g.o.Now()); left > 0 {
		says = say.F("lock.wait", map[string]any{"Seconds": int(left.Seconds()) + 1})
	} else if at.Failures > 0 {
		says = say.T("lock.wrong")
	}

	typed := len(g.pad.Typed())
	dots := t.TextStyleDef
	dots.Size = reach * 1.2
	dots.Color = t.ColorAccent
	shown := strings.Repeat("●", typed) + strings.Repeat("○", max(len(at.PIN)-typed, 0))

	return gogui.Column(gogui.ContainerCfg{
		Sizing:  gogui.FillFill,
		Padding: gogui.NewPadding(reach*0.5, reach, reach*0.5, reach),
		Spacing: gogui.SpacingLarge,
		HAlign:  gogui.HAlignCenter,
		VAlign:  gogui.VAlignMiddle,
		Color:   t.ColorBackground,
		Content: []gogui.View{
			gogui.Label(says, status),
			gogui.Label(shown, dots),
			g.pad.View(&ui.Toolkit, wide, float32(vh), len(at.PIN), g.Enter),
		},
	})
}
