package tessera

import (
	"strings"
	"time"
	"unicode/utf8"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/ygelfand/libcountertop/pkg/runtime/component"
	"github.com/ygelfand/libcountertop/pkg/say"
)

const (
	alertTitleMost    = 64
	alertSubtitleMost = 240
	alertButtonMost   = 16
	alertLongest      = 86400
)

const (
	AlertOK       = "ok"
	AlertSecond   = "button2"
	AlertTimeout  = "timeout"
	AlertReplaced = "replaced"
	AlertRemote   = "remote"
)

type Alert struct {
	Title    string
	Subtitle string
	Icon     string
	Color    string
	Buttons  []AlertButton
	Timeout  time.Duration
	Flash    bool
	Ended    func(action string)
}

type AlertButton struct {
	Text  string
	Color string
}

type PageAsk struct {
	Index int
	Ask   uint64
}

func shorten(text string, most int) string {
	text = strings.TrimSpace(text)
	if len(text) <= most {
		return text
	}
	end := most - 3
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + "..."
}

func (s *Screen) alert(c esphome.Call, choice bool) {
	if s.options.Alert == nil {
		return
	}
	title := shorten(c.String("title"), alertTitleMost)
	if title == "" {
		title = say.T("tessera.alert.notification")
	}
	first := shorten(c.String("button_text"), alertButtonMost)
	if first == "" {
		first = "OK"
	}
	a := Alert{
		Title:    title,
		Subtitle: shorten(c.String("subtitle"), alertSubtitleMost),
		Icon:     c.String("icon"),
		Color:    strings.ToLower(strings.TrimSpace(c.String("color"))),
		Buttons:  []AlertButton{{Text: first}},
		Timeout:  time.Duration(min(max(c.Int("timeout"), 0), alertLongest)) * time.Second,
		Flash:    c.Bool("flash"),
	}
	if choice {
		a.Buttons[0].Color = strings.ToLower(strings.TrimSpace(c.String("button_color")))
		if second := shorten(c.String("button2_text"), alertButtonMost); second != "" {
			a.Buttons = append(a.Buttons, AlertButton{
				Text:  second,
				Color: strings.ToLower(strings.TrimSpace(c.String("button2_color"))),
			})
		}
	}
	a.Ended = func(action string) {
		s.Told.Emit(component.Event{Name: "esphome.screen_alert", Data: map[string]string{
			"action": action,
			"title":  title,
			"screen": s.node(),
		}})
	}
	s.options.Alert(a)
}

func (s *Screen) showPage(page int) {
	s.mu.Lock()
	pages := len(s.layout.Pages)
	if pages == 0 {
		s.mu.Unlock()
		return
	}
	s.wanted = PageAsk{Index: min(max(page, 1), pages) - 1, Ask: s.wanted.Ask + 1}
	s.mu.Unlock()
	if s.options.Front != nil {
		s.options.Front()
	}
	s.Changed.Emit(struct{}{})
}

func (s *Screen) Asked() PageAsk {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wanted
}

func (s *Screen) screenActions() []*esphome.Action {
	w := s.options.Facing.word()
	alert := []esphome.Arg{
		{Name: "title", Type: esphome.ArgString, Example: "Front door"},
		{Name: "subtitle", Type: esphome.ArgString, Example: "Someone is at the door"},
		{Name: "icon", Type: esphome.ArgString, Example: "mdi:doorbell"},
		{Name: "color", Type: esphome.ArgString, Example: "red"},
		{
			Name:        "button_text",
			Type:        esphome.ArgString,
			Description: "Empty shows OK.",
		},
		{
			Name:        "timeout",
			Type:        esphome.ArgInt,
			Description: "Seconds before it closes itself; 0 keeps it until dismissed.",
			Example:     "30",
		},
		{Name: "flash", Type: esphome.ArgBool},
	}
	choice := append(append([]esphome.Arg(nil), alert...),
		esphome.Arg{Name: "button_color", Type: esphome.ArgString},
		esphome.Arg{
			Name:        "button2_text",
			Type:        esphome.ArgString,
			Description: "The second button; empty shows only one.",
		},
		esphome.Arg{Name: "button2_color", Type: esphome.ArgString},
	)
	return []*esphome.Action{
		{
			Name:        w + "_show_page",
			Description: "Switch the dashboard to a page.",
			Args: []esphome.Arg{
				{Name: "page", Type: esphome.ArgInt, Description: "Counting from 1.", Example: "1"},
			},
			Run: func(c esphome.Call) (any, error) { s.showPage(c.Int("page")); return nil, nil },
		},
		{
			Name:        w + "_show_alert",
			Description: "Show an alert; esphome.screen_alert fires with how it ended.",
			Args:        alert,
			Run:         func(c esphome.Call) (any, error) { s.alert(c, false); return nil, nil },
		},
		{
			Name:        w + "_show_alert_choice",
			Description: "Show an alert with up to two buttons; esphome.screen_alert says which was tapped.",
			Args:        choice,
			Run:         func(c esphome.Call) (any, error) { s.alert(c, true); return nil, nil },
		},
		{
			Name:        w + "_dismiss_alert",
			Description: "Close the alert on the screen.",
			Run: func(esphome.Call) (any, error) {
				if s.options.Unalert != nil {
					s.options.Unalert()
				}
				return nil, nil
			},
		},
	}
}
